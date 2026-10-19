//go:build integration

package integration

import (
	"context"
	"example.com/marketplace/services/listings"
	"example.com/marketplace/services/search"
	"github.com/google/uuid"
	"net/http/httptest"
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
