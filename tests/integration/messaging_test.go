//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"example.com/marketplace/services/listings"
	"example.com/marketplace/services/messaging"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMessagingPrivacyAndIdempotency(t *testing.T) {
	c := cfg(t)
	ld := db(t, "listings", listings.Migrations)
	seller, buyer, outsider := uuid.NewString(), uuid.NewString(), uuid.NewString()
	listingID := uuid.NewString()
	_, e := ld.Exec(context.Background(), "INSERT INTO listings(id,owner_id,title,status) VALUES($1,$2,'Велосипед','published')", listingID, seller)
	if e != nil {
		t.Fatal(e)
	}
	ls := httptest.NewServer(listings.New(ld, c))
	defer ls.Close()
	c.ListingsURL = ls.URL
	md := db(t, "messaging", messaging.Migrations)
	h := messaging.New(md, c)
	buyerToken := token(t, c, buyer)
	w, d := request(t, h, "POST", "/api/v1/conversations", buyerToken, map[string]string{"listing_id": listingID})
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	id := d["id"].(string)
	w, d = request(t, h, "POST", "/api/v1/conversations", buyerToken, map[string]string{"listing_id": listingID})
	if w.Code != 200 || d["id"] != id {
		t.Fatal("duplicate conversation")
	}
	w, _ = request(t, h, "GET", "/api/v1/conversations/"+id+"/messages", token(t, c, outsider), nil)
	if w.Code != 403 {
		t.Fatal("conversation leaked", w.Code)
	}
	send := func(body string) (int, map[string]any) {
		r := httptest.NewRequest("POST", "/api/v1/conversations/"+id+"/messages", strings.NewReader(`{"body":"`+body+`"}`))
		r.Header.Set("Authorization", "Bearer "+buyerToken)
		r.Header.Set("Idempotency-Key", "message-one")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var d map[string]any
		json.Unmarshal(w.Body.Bytes(), &d)
		return w.Code, d
	}
	code, first := send("Здравствуйте")
	if code != 201 {
		t.Fatal(code, first)
	}
	code, second := send("Здравствуйте")
	if code != 200 || second["id"] != first["id"] {
		t.Fatal("duplicate message")
	}
	code, _ = send("Другой текст")
	if code != 409 {
		t.Fatal("idempotency conflict ignored")
	}
	var count int
	var event string
	if e = md.QueryRow(context.Background(), "SELECT count(*),min(event::text) FROM outbox").Scan(&count, &event); e != nil || count != 1 || strings.Contains(event, "Здравствуйте") {
		t.Fatal("event privacy/atomicity", count, e)
	}
	_, e = ld.Exec(context.Background(), "UPDATE listings SET status='withdrawn' WHERE id=$1", listingID)
	if e != nil {
		t.Fatal(e)
	}
	w, d = request(t, h, "POST", "/api/v1/conversations", buyerToken, map[string]string{"listing_id": listingID})
	if w.Code != 200 || d["id"] != id {
		t.Fatal("existing conversation lost")
	}
}
