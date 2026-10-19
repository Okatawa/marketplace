package search

import (
	"context"
	"encoding/json"
	"example.com/marketplace/internal/platform/kafka"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Projection struct {
	DB *pgxpool.Pool
	ID string
}

func (p Projection) Apply(ctx context.Context, event kafka.Envelope) error {
	if e := event.Validate(); e != nil {
		return e
	}
	var value struct {
		ID, Status string
		Version    int64
		PriceMinor int64 `json:"price_minor"`
	}
	if e := json.Unmarshal(event.Payload, &value); e != nil {
		return kafka.PermanentError{Err: e}
	}
	if event.EventType != "listing.changed" || value.ID != event.AggregateID || value.Version != event.AggregateVersion || (value.Status != "published" && value.Status != "withdrawn") || value.PriceMinor < 0 {
		return kafka.PermanentError{Err: fmt.Errorf("invalid listing snapshot")}
	}
	_, e := p.DB.Exec(ctx, `INSERT INTO items(projection_id,id,version,status,data) VALUES($1,$2,$3,$4,$5) ON CONFLICT(projection_id,id) DO UPDATE SET version=excluded.version,status=excluded.status,data=excluded.data WHERE items.version<excluded.version`, p.ID, value.ID, value.Version, value.Status, event.Payload)
	return e
}
