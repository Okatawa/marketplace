package search

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/internal/platform/httpx"
	"example.com/marketplace/internal/platform/kafka"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kadm"
	"net/url"
	"time"
)

// Reindex starts a new consumer at a captured boundary, loads the current source
// snapshot, catches up to a second boundary and switches the active projection.
// Failure leaves the old projection active; incomplete data is retained for inspection.
func Reindex(ctx context.Context, db *pgxpool.Pool, cfg config.Config, id string) error {
	if _, e := uuid.Parse(id); e != nil {
		return e
	}
	group := "search-reindex-" + id
	topic := "marketplace.listings.v1"
	p, e := kafka.NewProducer(cfg.KafkaBrokers)
	if e != nil {
		return e
	}
	defer p.Close()
	admin := kadm.NewClient(p.Client)
	topics, e := admin.ListTopics(ctx, topic)
	if e != nil {
		return e
	}
	source, ok := topics[topic]
	if !ok || source.Err != nil || len(source.Partitions) == 0 {
		return fmt.Errorf("source topic unavailable")
	}
	replicas := int16(1)
	for _, part := range source.Partitions {
		replicas = int16(len(part.Replicas))
		break
	}
	retention := "604800000"
	created, e := admin.CreateTopics(ctx, int32(len(source.Partitions)), replicas, map[string]*string{"retention.ms": &retention}, topic+"."+group+".dlq")
	if e != nil {
		return e
	}
	for _, result := range created {
		if result.Err != nil {
			return result.Err
		}
	}
	start, e := admin.ListEndOffsets(ctx, topic)
	if e != nil {
		return e
	}
	if e = start.Error(); e != nil {
		return e
	}
	if _, e = db.Exec(ctx, "INSERT INTO projections(id,group_id) VALUES($1,$2)", id, group); e != nil {
		return e
	}
	committed, e := admin.CommitOffsets(ctx, group, start.Offsets())
	if e != nil {
		return e
	}
	if e = committed.Error(); e != nil {
		return e
	}
	consumer, e := kafka.NewConsumer(cfg.KafkaBrokers, topic, group)
	if e != nil {
		return e
	}
	defer consumer.Close()
	// A rebuild must be complete. Normal consumers route poison messages to DLQ;
	// the temporary rebuild consumer aborts instead of activating partial data.
	consumer.StopOnPermanent = true
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	var consumerErr error
	projection := Projection{DB: db, ID: id}
	go func() { consumerErr = consumer.Run(work, projection.Apply); close(done) }()
	defer func() { cancel(); <-done }()
	after := ""
	for {
		var page struct {
			Items []json.RawMessage `json:"items"`
			Next  string            `json:"next_cursor"`
		}
		if e = httpx.GetJSON(ctx, cfg.ListingsURL+"/internal/v1/listings/export?limit=100&after_id="+url.QueryEscape(after), cfg.ServiceToken, &page); e != nil {
			return e
		}
		for _, raw := range page.Items {
			var meta struct {
				ID      string `json:"id"`
				Version int64  `json:"version"`
			}
			if e = json.Unmarshal(raw, &meta); e != nil {
				return e
			}
			event := kafka.Envelope{EventID: uuid.NewString(), EventType: "listing.changed", SchemaVersion: 1, OccurredAt: time.Now(), AggregateID: meta.ID, AggregateVersion: meta.Version, Payload: raw}
			if e = projection.Apply(ctx, event); e != nil {
				return e
			}
		}
		if page.Next == "" {
			break
		}
		if page.Next == after {
			return fmt.Errorf("snapshot cursor did not advance")
		}
		after = page.Next
	}
	barrier, e := admin.ListEndOffsets(ctx, topic)
	if e != nil {
		return e
	}
	if e = barrier.Error(); e != nil {
		return e
	}
	for {
		select {
		case <-done:
			return fmt.Errorf("reindex consumer stopped: %w", consumerErr)
		default:
		}
		lowest, e := admin.ListStartOffsets(ctx, topic)
		if e != nil {
			return e
		}
		if e = lowest.Error(); e != nil {
			return e
		}
		for part, o := range start[topic] {
			if lowest[topic][part].Offset > o.Offset {
				return fmt.Errorf("retained history changed during rebuild")
			}
		}
		offsets, e := admin.FetchOffsets(ctx, group)
		if e != nil {
			return e
		}
		if e = offsets.Error(); e != nil {
			return e
		}
		ready := true
		for part, target := range barrier[topic] {
			current, ok := offsets[topic][part]
			if !ok || current.At < target.Offset {
				ready = false
			}
		}
		if ready {
			break
		}
		if !kafka.Pause(ctx, 250*time.Millisecond) {
			return ctx.Err()
		}
	}
	cancel()
	<-done
	if consumerErr != nil && !errors.Is(consumerErr, context.Canceled) {
		return consumerErr
	}
	tx, e := db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "UPDATE projections SET ready=true WHERE id=$1", id); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE settings SET active_projection=$1", id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
