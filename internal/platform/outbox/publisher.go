package outbox

import (
	"context"
	"encoding/json"
	"example.com/marketplace/internal/platform/kafka"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"time"
)

func Insert(ctx context.Context, tx pgx.Tx, event kafka.Envelope) error {
	raw, e := json.Marshal(event)
	if e != nil {
		return e
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	headers, _ := json.Marshal(carrier)
	_, e = tx.Exec(ctx, "INSERT INTO outbox(event,headers) VALUES($1,$2)", raw, headers)
	return e
}
func Run(ctx context.Context, db *pgxpool.Pool, p *kafka.Producer, topic string) error {
	conn, e := db.Acquire(ctx)
	if e != nil {
		return e
	}
	defer conn.Release()
	var locked bool
	if e = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(77191)").Scan(&locked); e != nil {
		return e
	}
	if !locked {
		return fmt.Errorf("publisher already active")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, "SELECT pg_advisory_unlock(77191)"); err != nil {
			conn.Conn().Close(cleanup)
		}
	}()
	for ctx.Err() == nil {
		var id int64
		var raw, headers []byte
		e = conn.QueryRow(ctx, "SELECT id,event,headers FROM outbox WHERE sent_at IS NULL ORDER BY id LIMIT 1").Scan(&id, &raw, &headers)
		if e == pgx.ErrNoRows {
			if !kafka.Pause(ctx, 250*time.Millisecond) {
				return ctx.Err()
			}
			continue
		}
		if e != nil {
			return e
		}
		var event kafka.Envelope
		if e = json.Unmarshal(raw, &event); e != nil {
			return e
		}
		carrier := propagation.MapCarrier{}
		if e = json.Unmarshal(headers, &carrier); e != nil {
			return e
		}
		work := otel.GetTextMapPropagator().Extract(ctx, carrier)
		if e = p.Publish(work, topic, event.AggregateID, event); e != nil {
			return e
		}
		if _, e = conn.Exec(ctx, "UPDATE outbox SET sent_at=now() WHERE id=$1", id); e != nil {
			return e
		}
	}
	return ctx.Err()
}
