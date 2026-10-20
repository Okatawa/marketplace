//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"example.com/marketplace/internal/platform/kafka"
	"example.com/marketplace/services/listings"
	"example.com/marketplace/services/search"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReindexSnapshotAndActivation(t *testing.T) {
	c := cfg(t)
	lp := db(t, "listings", listings.Migrations)
	sp := db(t, "search", search.Migrations)
	id := uuid.NewString()
	_, e := lp.Exec(context.Background(), "INSERT INTO listings(id,owner_id,title,status,version) VALUES($1,$2,'Снимок','published',7)", id, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(listings.New(lp, c))
	defer server.Close()
	c.ListingsURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	projection := uuid.NewString()
	if e = search.Reindex(ctx, sp, c, projection); e != nil {
		t.Fatal(e)
	}
	var active string
	var version int64
	if e = sp.QueryRow(ctx, "SELECT active_projection FROM settings").Scan(&active); e != nil || active != projection {
		t.Fatal("activation", active, e)
	}
	if e = sp.QueryRow(ctx, "SELECT version FROM items WHERE projection_id=$1 AND id=$2", projection, id).Scan(&version); e != nil || version != 7 {
		t.Fatal("snapshot missing", version, e)
	}
}

func TestReindexConcurrentWithdrawal(t *testing.T) {
	c := cfg(t)
	sp := db(t, "search", search.Migrations)
	id := uuid.NewString()
	p, err := kafka.NewProducer(c.KafkaBrokers)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := listings.Listing{ID: id, OwnerID: uuid.NewString(), Title: "Конкурентное снятие", Status: "withdrawn", Version: 2, Photos: []listings.Photo{}}
		if err := p.Publish(r.Context(), "marketplace.listings.v1", id, kafka.New("listing.changed", id, 2, value)); err != nil {
			t.Error(err)
		}
		value.Status, value.Version = "published", 1
		json.NewEncoder(w).Encode(map[string]any{"items": []listings.Listing{value}, "next_cursor": ""})
	}))
	defer server.Close()
	c.ListingsURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	projection := uuid.NewString()
	if err := search.Reindex(ctx, sp, c, projection); err != nil {
		t.Fatal(err)
	}
	var status string
	var version int64
	if err := sp.QueryRow(ctx, "SELECT status,version FROM items WHERE projection_id=$1 AND id=$2", projection, id).Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != "withdrawn" || version != 2 {
		t.Fatal("stale snapshot resurrected withdrawn listing", status, version)
	}
}

func TestReindexAbortKeepsOldProjection(t *testing.T) {
	c := cfg(t)
	sp := db(t, "search", search.Migrations)
	var before, after string
	sp.QueryRow(context.Background(), "SELECT active_projection FROM settings").Scan(&before)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	c.ListingsURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := search.Reindex(ctx, sp, c, uuid.NewString()); err == nil {
		t.Fatal("expected export failure")
	}
	sp.QueryRow(ctx, "SELECT active_projection FROM settings").Scan(&after)
	if before != after {
		t.Fatal("failed rebuild replaced active projection")
	}
}

func TestReindexRejectsInvalidLiveEvent(t *testing.T) {
	c := cfg(t)
	sp := db(t, "search", search.Migrations)
	p, err := kafka.NewProducer(c.KafkaBrokers)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	id := uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := p.Publish(r.Context(), "marketplace.listings.v1", id, kafka.New("listing.changed", id, 1, map[string]any{"id": id, "status": "draft", "version": 1})); err != nil {
			t.Error(err)
		}
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "next_cursor": ""})
	}))
	defer server.Close()
	c.ListingsURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	err = search.Reindex(ctx, sp, c, uuid.NewString())
	if err == nil || !strings.Contains(err.Error(), "invalid listing snapshot") {
		t.Fatal("reindex must surface the permanent error without activation", err)
	}
}
