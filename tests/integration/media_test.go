//go:build integration

package integration

import (
	"bytes"
	"context"
	"example.com/marketplace/services/listings"
	"github.com/google/uuid"
	"image"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

func TestMediaUploadAndPublication(t *testing.T) {
	c := cfg(t)
	pool := db(t, "listings", listings.Migrations)
	if e := listings.NewMedia(c).Ensure(context.Background()); e != nil {
		t.Fatal(e)
	}
	h := listings.New(pool, c)
	owner := token(t, c, uuid.NewString())
	w, d := request(t, h, "POST", "/api/v1/listings", owner, map[string]any{"title": "Велосипед", "description": "Хорошее состояние", "category_id": "sport", "price_minor": 1500000})
	if w.Code != 201 {
		t.Fatal(w.Code)
	}
	id := d["id"].(string)
	var content bytes.Buffer
	mp := multipart.NewWriter(&content)
	part, _ := mp.CreateFormFile("file", "photo.png")
	png.Encode(part, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	mp.Close()
	r := httptest.NewRequest("POST", "/api/v1/listings/"+id+"/photos", &content)
	r.Header.Set("Content-Type", mp.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+owner)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatal("upload", w.Code, w.Body.String())
	}
	w, _ = request(t, h, "POST", "/api/v1/listings/"+id+"/publish", owner, map[string]int{"expected_version": 2})
	if w.Code != 200 {
		t.Fatal("publish", w.Code, w.Body.String())
	}
	var n int
	if e := pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox").Scan(&n); e != nil || n != 1 {
		t.Fatal("atomic outbox", n, e)
	}
	w, d = request(t, h, "GET", "/api/v1/listings/"+id, "", nil)
	if w.Code != 200 || len(d["photos"].([]any)) != 1 {
		t.Fatal("public photos", w.Code)
	}
	photo := d["photos"].([]any)[0].(map[string]any)
	w, _ = request(t, h, "DELETE", "/api/v1/listings/"+id+"/photos/"+photo["id"].(string), owner, nil)
	if w.Code != 400 {
		t.Fatal("removed last published photo")
	}
}
