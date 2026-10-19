package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"example.com/marketplace/internal/platform/config"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayRoutesAndLimits(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-User-ID") != "" {
			t.Error("trusted forged header")
		}
		w.WriteHeader(200)
	}))
	defer backend.Close()
	h := New(config.Config{JWTPublic: base64.StdEncoding.EncodeToString(pub), AccountsURL: backend.URL, ListingsURL: backend.URL})
	for _, path := range []string{"/internal/v1/users/123/notification-address", "/api/v1/unknown"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatal("internal or unknown exposed", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/users/123", nil)
	r.Header.Set("X-User-ID", "admin")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for i := 0; i < 11; i++ {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/auth/login", nil))
	}
	if w.Code != 429 {
		t.Fatal("login unlimited", w.Code)
	}
}
