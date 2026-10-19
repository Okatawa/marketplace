package kafka

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"log/slog"
	"os"
	"time"
)

func Options(brokers []string) []kgo.Opt {
	o := []kgo.Opt{kgo.SeedBrokers(brokers...), kgo.DialTimeout(5 * time.Second)}
	if os.Getenv("KAFKA_TLS") == "true" {
		o = append(o, kgo.DialTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
	}
	if u := os.Getenv("KAFKA_USERNAME"); u != "" {
		o = append(o, kgo.SASL(plain.Auth{User: u, Pass: os.Getenv("KAFKA_PASSWORD")}.AsMechanism()))
	}
	return o
}

type Producer struct{ Client *kgo.Client }

func NewProducer(brokers []string) (*Producer, error) {
	o := append(Options(brokers), kgo.RequiredAcks(kgo.AllISRAcks()), kgo.RecordDeliveryTimeout(20*time.Second))
	c, e := kgo.NewClient(o...)
	return &Producer{c}, e
}
func (p *Producer) Close() { p.Client.Close() }
func (p *Producer) Publish(ctx context.Context, topic, key string, e Envelope) error {
	ctx, span := otel.Tracer("marketplace/kafka").Start(ctx, "publish "+topic, trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	headers := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, headers)
	r := &kgo.Record{Topic: topic, Key: []byte(key), Value: raw}
	for k, v := range headers {
		r.Headers = append(r.Headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
	}
	return p.Client.ProduceSync(ctx, r).FirstErr()
}

type Consumer struct {
	Client       *kgo.Client
	Producer     *Producer
	Topic, Group string
}

func NewConsumer(brokers []string, topic, group string) (*Consumer, error) {
	o := append(Options(brokers), kgo.ConsumerGroup(group), kgo.ConsumeTopics(topic), kgo.DisableAutoCommit(), kgo.BlockRebalanceOnPoll(), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	c, e := kgo.NewClient(o...)
	if e != nil {
		return nil, e
	}
	p, e := NewProducer(brokers)
	if e != nil {
		c.Close()
		return nil, e
	}
	return &Consumer{c, p, topic, group}, nil
}
func (c *Consumer) Close() { c.Client.AllowRebalance(); c.Client.Close(); c.Producer.Close() }
func (c *Consumer) Run(ctx context.Context, handle func(context.Context, Envelope) error) error {
	for ctx.Err() == nil {
		fetches := c.Client.PollRecords(ctx, 1)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			c.Client.AllowRebalance()
			return fmt.Errorf("poll: %w", errs[0].Err)
		}
		for _, record := range fetches.Records() {
			carrier := propagation.MapCarrier{}
			for _, h := range record.Headers {
				carrier[h.Key] = string(h.Value)
			}
			work := otel.GetTextMapPropagator().Extract(ctx, carrier)
			work, span := otel.Tracer("marketplace/kafka").Start(work, "consume "+c.Topic, trace.WithSpanKind(trace.SpanKindConsumer))
			var event Envelope
			decodeErr := json.Unmarshal(record.Value, &event)
			if decodeErr != nil {
				decodeErr = PermanentError{decodeErr}
			} else {
				decodeErr = event.Validate()
			}
			delay := time.Second
			for {
				err := decodeErr
				if err == nil {
					err = handle(work, event)
				}
				var permanent PermanentError
				if errors.As(err, &permanent) {
					payload, _ := json.Marshal(map[string]any{"topic": record.Topic, "partition": record.Partition, "offset": record.Offset, "key": string(record.Key), "value": string(record.Value)})
					dlq := &kgo.Record{Topic: c.Topic + "." + c.Group + ".dlq", Key: []byte(fmt.Sprintf("%s:%d:%d", record.Topic, record.Partition, record.Offset)), Value: payload}
					err = c.Producer.Client.ProduceSync(work, dlq).FirstErr()
				}
				if err == nil {
					if err = c.Client.CommitRecords(work, record); err == nil {
						break
					}
				}
				slog.WarnContext(work, "event retry", "topic", c.Topic, "partition", record.Partition, "offset", record.Offset)
				if !Pause(ctx, delay) {
					span.End()
					return ctx.Err()
				}
				if delay < 30*time.Second {
					delay *= 2
					if delay > 30*time.Second {
						delay = 30 * time.Second
					}
				}
			}
			span.End()
		}
		c.Client.AllowRebalance()
	}
	return ctx.Err()
}
func Pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
