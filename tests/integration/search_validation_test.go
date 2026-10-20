//go:build integration

package integration

import (
	"context"
	"errors"
	"example.com/marketplace/internal/platform/kafka"
	"example.com/marketplace/services/search"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestSearchRejectsMalformedSnapshotsWithoutReplacingVisibleListing(t *testing.T) {
	pool := db(t, "search", search.Migrations)
	p := search.Projection{DB: pool, ID: "default"}
	ctx := context.Background()
	type invalidSnapshot struct {
		name   string
		change func(map[string]any)
	}
	cases := []invalidSnapshot{
		{"price out of range", func(s map[string]any) { s["price_minor"] = int64(100000000001) }},
		{"price wrong type", func(s map[string]any) { s["price_minor"] = "0" }},
		{"invalid owner", func(s map[string]any) { s["owner_id"] = "not-a-uuid" }},
		{"invalid date", func(s map[string]any) { s["created_at"] = "yesterday" }},
		{"invalid photo", func(s map[string]any) { s["photos"] = []any{map[string]any{"id": "bad"}} }},
		{"null photo", func(s map[string]any) { s["photos"] = []any{nil} }},
		{"long title", func(s map[string]any) { s["title"] = strings.Repeat("я", 121) }},
		{"wrong title type", func(s map[string]any) { s["title"] = 1 }},
		{"long description", func(s map[string]any) { s["description"] = strings.Repeat("я", 10001) }},
		{"wrong field casing", func(s map[string]any) { s["PRICE_MINOR"] = s["price_minor"]; delete(s, "price_minor") }},
	}
	for _, field := range []string{"id", "owner_id", "title", "description", "category_id", "price_minor", "status", "version", "created_at", "photos"} {
		cases = append(cases, invalidSnapshot{"missing " + field, func(s map[string]any) { delete(s, field) }})
		cases = append(cases, invalidSnapshot{"null " + field, func(s map[string]any) { s[field] = nil }})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.NewString()
			snapshot := map[string]any{"id": id, "owner_id": uuid.NewString(), "title": "Бесплатный велосипед", "description": "Рабочий", "category_id": "sport", "price_minor": 0, "status": "published", "version": 1, "created_at": "2026-10-20T10:00:00Z", "photos": []any{}}
			if err := p.Apply(ctx, kafka.New("listing.changed", id, 1, snapshot)); err != nil {
				t.Fatal(err)
			}
			snapshot["version"] = 2
			tc.change(snapshot)
			err := p.Apply(ctx, kafka.New("listing.changed", id, 2, snapshot))
			var permanent kafka.PermanentError
			if !errors.As(err, &permanent) {
				t.Errorf("malformed snapshot must be rejected permanently, got %v", err)
			}
			var version int64
			var price int64
			if err := pool.QueryRow(ctx, "SELECT version,(data->>'price_minor')::bigint FROM items WHERE id=$1 AND projection_id='default'", id).Scan(&version, &price); err != nil {
				t.Fatal(err)
			}
			if version != 1 || price != 0 {
				t.Fatalf("invalid snapshot replaced visible listing: version=%d price=%d", version, price)
			}
		})
	}
}
