//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"example.com/marketplace/internal/platform/kafka"
	"example.com/marketplace/internal/platform/outbox"
	"example.com/marketplace/services/listings"
	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"strings"
	"testing"
	"time"
)

func TestOutboxPublishAcceptedThenDBFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	pool := db(t, "listings", listings.Migrations)
	producer, e := kafka.NewProducer([]string{"127.0.0.1:19092"})
	if e != nil {
		t.Fatal(e)
	}
	defer producer.Close()
	admin := kadm.NewClient(producer.Client)
	topic := "test-outbox-" + uuid.NewString()
	results, e := admin.CreateTopics(ctx, 1, 1, nil, topic)
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range results {
		if r.Err != nil {
			t.Fatal(r.Err)
		}
	}
	defer admin.DeleteTopics(context.Background(), topic)
	event := kafka.New("listing.changed", uuid.NewString(), 1, map[string]string{"status": "published"})
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = outbox.Insert(ctx, tx, event); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	_, e = pool.Exec(ctx, `CREATE FUNCTION fail_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated lost DB acknowledgement'; END $$;CREATE TRIGGER fail_mark BEFORE UPDATE ON outbox FOR EACH ROW EXECUTE FUNCTION fail_update();`)
	if e != nil {
		t.Fatal(e)
	}
	if e = outbox.Run(ctx, pool, producer, topic); e == nil || !strings.Contains(e.Error(), "simulated lost DB") {
		t.Fatal("expected simulated update failure, got", e)
	}
	client, e := kgo.NewClient(kgo.SeedBrokers("127.0.0.1:19092"), kgo.ConsumeTopics(topic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	first := client.PollRecords(ctx, 1).Records()
	if len(first) != 1 {
		t.Fatal("first publication missing")
	}
	if _, e = pool.Exec(ctx, "DROP TRIGGER fail_mark ON outbox"); e != nil {
		t.Fatal(e)
	}
	work, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- outbox.Run(work, pool, producer, topic) }()
	second := client.PollRecords(ctx, 1).Records()
	stop()
	<-done
	if len(second) != 1 {
		t.Fatal("retry publication missing")
	}
	var a, b kafka.Envelope
	json.Unmarshal(first[0].Value, &a)
	json.Unmarshal(second[0].Value, &b)
	if a.EventID != event.EventID || b.EventID != a.EventID {
		t.Fatal("retry changed identity")
	}
}
