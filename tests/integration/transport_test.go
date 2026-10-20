//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"example.com/marketplace/internal/platform/kafka"
	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type kafkaTransportFixture struct {
	topic, group, dlq string
	producer          *kafka.Producer
	admin             *kadm.Client
}

func transportFixture(t *testing.T, ctx context.Context) kafkaTransportFixture {
	t.Helper()
	p, err := kafka.NewProducer([]string{"127.0.0.1:19092"})
	if err != nil {
		t.Fatal(err)
	}
	f := kafkaTransportFixture{topic: "test-transport-" + uuid.NewString(), group: "test-" + uuid.NewString(), producer: p, admin: kadm.NewClient(p.Client)}
	f.dlq = f.topic + "." + f.group + ".dlq"
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		f.admin.DeleteGroups(cleanup, f.group)
		f.admin.DeleteTopics(cleanup, f.topic, f.dlq)
		p.Close()
	})
	results, err := f.admin.CreateTopics(ctx, 1, 1, nil, f.topic, f.dlq)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Err != nil {
			t.Fatal(result.Err)
		}
	}
	return f
}

func (f kafkaTransportFixture) consumer(t *testing.T) *kafka.Consumer {
	t.Helper()
	c, err := kafka.NewConsumer([]string{"127.0.0.1:19092"}, f.topic, f.group)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f kafkaTransportFixture) awaitOffset(t *testing.T, ctx context.Context, want int64) {
	t.Helper()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		offsets, err := f.admin.FetchOffsets(ctx, f.group)
		if err != nil {
			t.Fatal(err)
		}
		if offset, ok := offsets.Lookup(f.topic, 0); ok {
			if offset.Err != nil {
				t.Fatal(offset.Err)
			}
			if offset.At == want {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("offset did not reach %d: %v", want, ctx.Err())
		case <-ticker.C:
		}
	}
}

// Removing propagation injection or extraction breaks the trace across Kafka.
func TestTraceContextThroughKafka(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := transportFixture(t, ctx)
	oldProvider, oldPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer func() {
		otel.SetTracerProvider(oldProvider)
		otel.SetTextMapPropagator(oldPropagator)
		provider.Shutdown(context.Background())
	}()
	publishCtx, span := provider.Tracer("integration").Start(ctx, "HTTP request")
	defer span.End()
	event := kafka.New("listing.changed", uuid.NewString(), 1, map[string]string{"status": "published"})
	if err := f.producer.Publish(publishCtx, f.topic, event.AggregateID, event); err != nil {
		t.Fatal(err)
	}
	c := f.consumer(t)
	work, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	received := make(chan trace.SpanContext, 1)
	go func() {
		done <- c.Run(work, func(ctx context.Context, got kafka.Envelope) error {
			if got.EventID != event.EventID {
				return errors.New("unexpected event")
			}
			received <- trace.SpanContextFromContext(ctx)
			return nil
		})
	}()
	defer func() { stop(); <-done; c.Close() }()
	select {
	case sc := <-received:
		if !sc.IsValid() || sc.TraceID() != span.SpanContext().TraceID() || sc.SpanID() == span.SpanContext().SpanID() {
			t.Fatalf("consumer is not a child in the request trace: %v", sc)
		}
	case <-ctx.Done():
		t.Fatal("event was not consumed", ctx.Err())
	}
	f.awaitOffset(t, ctx, 1)
}

// Poison records must be preserved in the DLQ before their source offset advances.
func TestMalformedRecordIsPreservedInDLQ(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := transportFixture(t, ctx)
	invalid := `{"event_id":`
	if err := f.producer.Client.ProduceSync(ctx, &kgo.Record{Topic: f.topic, Key: []byte("original-key"), Value: []byte(invalid)}).FirstErr(); err != nil {
		t.Fatal(err)
	}
	c := f.consumer(t)
	work, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	handled := make(chan struct{}, 1)
	go func() {
		done <- c.Run(work, func(context.Context, kafka.Envelope) error {
			handled <- struct{}{}
			return nil
		})
	}()
	defer func() { stop(); <-done; c.Close() }()
	reader, err := kgo.NewClient(kgo.SeedBrokers("127.0.0.1:19092"), kgo.ConsumeTopics(f.dlq), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	fetch := reader.PollRecords(ctx, 1)
	if errs := fetch.Errors(); len(errs) != 0 {
		t.Fatal(errs)
	}
	records := fetch.Records()
	if len(records) != 1 {
		t.Fatal("missing DLQ record", ctx.Err())
	}
	var dead struct {
		Topic, Key, Value string
		Partition         int32
		Offset            int64
	}
	if err := json.Unmarshal(records[0].Value, &dead); err != nil {
		t.Fatal(err)
	}
	if dead.Topic != f.topic || dead.Partition != 0 || dead.Offset != 0 || dead.Key != "original-key" || dead.Value != invalid {
		t.Fatalf("DLQ lost source coordinates or original bytes: %+v", dead)
	}
	f.awaitOffset(t, ctx, 1)
	select {
	case <-handled:
		t.Fatal("malformed record reached the domain handler")
	default:
	}
}

// Committing an unsuccessful handler would lose this event after restart.
func TestTransientHandlerCancellationDoesNotCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	f := transportFixture(t, ctx)
	event := kafka.New("listing.changed", uuid.NewString(), 1, map[string]string{"status": "published"})
	if err := f.producer.Publish(ctx, f.topic, event.AggregateID, event); err != nil {
		t.Fatal(err)
	}
	first := f.consumer(t)
	work, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- first.Run(work, func(context.Context, kafka.Envelope) error {
			stop()
			return errors.New("temporary database outage")
		})
	}()
	err := <-done
	stop()
	first.Close()
	if !errors.Is(err, context.Canceled) {
		t.Fatal("expected canceled unsuccessful handler", err)
	}
	offsets, err := f.admin.FetchOffsets(ctx, f.group)
	if err != nil {
		t.Fatal(err)
	}
	if offset, ok := offsets.Lookup(f.topic, 0); ok && offset.At > 0 {
		t.Fatalf("failed event was committed at offset %d", offset.At)
	}
	second := f.consumer(t)
	retry, stopRetry := context.WithCancel(ctx)
	retried := make(chan string, 1)
	doneRetry := make(chan error, 1)
	go func() {
		doneRetry <- second.Run(retry, func(_ context.Context, got kafka.Envelope) error {
			retried <- got.EventID
			return nil
		})
	}()
	defer func() { stopRetry(); <-doneRetry; second.Close() }()
	select {
	case id := <-retried:
		if id != event.EventID {
			t.Fatalf("redelivered different event: %s", id)
		}
	case <-ctx.Done():
		t.Fatal("unsuccessful event lost on restart", ctx.Err())
	}
	f.awaitOffset(t, ctx, 1)
}
