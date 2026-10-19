//go:build integration

package integration

import (
	"context"
	"example.com/marketplace/internal/platform/kafka"
	"example.com/marketplace/services/search"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestSearchApplyNewerOnly(t *testing.T) {
	c := cfg(t)
	pool := db(t, "search", search.Migrations)
	p := search.Projection{DB: pool, ID: "default"}
	id := uuid.NewString()
	snapshot := map[string]any{"id": id, "owner_id": uuid.NewString(), "title": "Велосипед", "description": "Горный велосипед", "category_id": "sport", "price_minor": 1500000, "status": "published", "version": 3, "created_at": time.Now(), "photos": []any{}}
	event := kafka.New("listing.changed", id, 3, snapshot)
	if e := p.Apply(context.Background(), event); e != nil {
		t.Fatal(e)
	}
	h := search.New(pool, c)
	w, d := request(t, h, "GET", "/api/v1/search?q=велосипед&min_price=1000000&max_price=2000000", "", nil)
	if w.Code != 200 || len(d["items"].([]any)) != 1 {
		t.Fatal("search", w.Code, w.Body.String())
	}
	snapshot["status"] = "withdrawn"
	snapshot["version"] = 4
	if e := p.Apply(context.Background(), kafka.New("listing.changed", id, 4, snapshot)); e != nil {
		t.Fatal(e)
	}
	if e := p.Apply(context.Background(), event); e != nil {
		t.Fatal(e)
	}
	w, d = request(t, h, "GET", "/api/v1/search", "", nil)
	if w.Code != 200 || len(d["items"].([]any)) != 0 {
		t.Fatal("stale event restored listing")
	}
	w, _ = request(t, h, "GET", "/api/v1/search?min_price=20&max_price=10", "", nil)
	if w.Code != 400 {
		t.Fatal("bad range accepted")
	}
}
