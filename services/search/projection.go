package search

import (
	"context"
	"encoding/json"
	"example.com/marketplace/internal/platform/kafka"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
	"unicode/utf8"
)

type Projection struct {
	DB *pgxpool.Pool
	ID string
}

func (p Projection) Apply(ctx context.Context, event kafka.Envelope) error {
	if e := event.Validate(); e != nil {
		return e
	}
	// Decode canonical field names individually: JSON's case-insensitive struct
	// matching must not accept a key the SQL projection cannot subsequently read.
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(event.Payload, &fields); e != nil {
		return kafka.PermanentError{Err: e}
	}
	var value struct {
		ID, OwnerID, Title, Description, CategoryID, Status string
		Version, PriceMinor                                 int64
		CreatedAt                                           time.Time
		Photos                                              []map[string]json.RawMessage
	}
	for _, field := range []struct {
		name   string
		target any
	}{
		{"id", &value.ID}, {"owner_id", &value.OwnerID},
		{"title", &value.Title}, {"description", &value.Description},
		{"category_id", &value.CategoryID}, {"status", &value.Status},
		{"version", &value.Version}, {"price_minor", &value.PriceMinor},
		{"created_at", &value.CreatedAt}, {"photos", &value.Photos},
	} {
		raw, ok := fields[field.name]
		if !ok || string(raw) == "null" {
			return kafka.PermanentError{Err: fmt.Errorf("invalid listing snapshot: required %s", field.name)}
		}
		if e := json.Unmarshal(raw, field.target); e != nil {
			return kafka.PermanentError{Err: e}
		}
	}
	if event.EventType != "listing.changed" || value.ID != event.AggregateID || value.Version != event.AggregateVersion || (value.Status != "published" && value.Status != "withdrawn") || value.PriceMinor < 0 || value.PriceMinor > 100000000000 || utf8.RuneCountInString(value.Title) > 120 || utf8.RuneCountInString(value.Description) > 10000 {
		return kafka.PermanentError{Err: fmt.Errorf("invalid listing snapshot")}
	}
	if _, e := uuid.Parse(value.OwnerID); e != nil {
		return kafka.PermanentError{Err: e}
	}
	for _, photo := range value.Photos {
		var id string
		if e := json.Unmarshal(photo["id"], &id); e != nil {
			return kafka.PermanentError{Err: e}
		}
		if _, e := uuid.Parse(id); e != nil {
			return kafka.PermanentError{Err: e}
		}
	}
	_, e := p.DB.Exec(ctx, `INSERT INTO items(projection_id,id,version,status,data) VALUES($1,$2,$3,$4,$5) ON CONFLICT(projection_id,id) DO UPDATE SET version=excluded.version,status=excluded.status,data=excluded.data WHERE items.version<excluded.version`, p.ID, value.ID, value.Version, value.Status, event.Payload)
	return e
}
