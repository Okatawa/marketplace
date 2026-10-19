//go:build integration

package integration

import (
	"example.com/marketplace/services/listings"
	"github.com/google/uuid"
	"testing"
)

func TestListingsOwnershipAndVersion(t *testing.T) {
	c := cfg(t)
	h := listings.New(db(t, "listings", listings.Migrations), c)
	owner := token(t, c, uuid.NewString())
	other := token(t, c, uuid.NewString())
	w, d := request(t, h, "POST", "/api/v1/listings", owner, map[string]any{"title": "Велосипед", "description": "Хороший", "category_id": "sport", "price_minor": 1500000})
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	id := d["id"].(string)
	w, _ = request(t, h, "GET", "/api/v1/listings/"+id, other, nil)
	if w.Code != 404 {
		t.Fatal("draft leaked", w.Code)
	}
	in := map[string]any{"title": "Новый", "description": "Хороший", "category_id": "sport", "price_minor": 1500000, "expected_version": 1}
	w, _ = request(t, h, "PATCH", "/api/v1/listings/"+id, other, in)
	if w.Code != 403 {
		t.Fatal("foreign mutation", w.Code)
	}
	w, _ = request(t, h, "PATCH", "/api/v1/listings/"+id, owner, in)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w, _ = request(t, h, "PATCH", "/api/v1/listings/"+id, owner, in)
	if w.Code != 409 {
		t.Fatal("lost update accepted", w.Code)
	}
	w, _ = request(t, h, "POST", "/api/v1/listings/"+id+"/publish", owner, map[string]int{"expected_version": 2})
	if w.Code != 400 {
		t.Fatal("publication without photo", w.Code)
	}
}
