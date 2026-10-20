package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/internal/platform/httpx"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGatewayErrorContract(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	backend := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic(http.ErrAbortHandler) }))
	backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	backend.Start()
	defer backend.Close()
	for _, tc := range []struct {
		name, method, path, token string
		status, attempts          int
	}{
		{"invalid token", "GET", "/api/v1/me", "Bearer invalid", 401, 1},
		{"malformed token", "GET", "/api/v1/me", "Basic invalid", 401, 1},
		{"rate limit", "POST", "/api/v1/auth/login", "", 429, 11},
		{"upstream", "GET", "/api/v1/users/123", "", 502, 1},
		{"not found", "GET", "/api/v1/unknown", "", 404, 1},
		{"method", "DELETE", "/api/v1/categories", "", 405, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := New(config.Config{JWTPublic: base64.StdEncoding.EncodeToString(pub), AccountsURL: backend.URL, ListingsURL: backend.URL})
			var w *httptest.ResponseRecorder
			for i := 0; i < tc.attempts; i++ {
				r := httptest.NewRequest(tc.method, tc.path, nil)
				r.Header.Set("Authorization", tc.token)
				r.Header.Set("X-Request-ID", "untrusted-invalid-id")
				w = httptest.NewRecorder()
				h.ServeHTTP(w, r)
			}
			var body struct {
				RequestID string `json:"request_id"`
				Error     struct{ Code, Message string }
			}
			if w.Code != tc.status || json.Unmarshal(w.Body.Bytes(), &body) != nil || !httpx.ValidID(body.RequestID) || body.RequestID != w.Header().Get("X-Request-ID") || body.Error.Code == "" || body.Error.Message == "" {
				t.Fatalf("error response violates contract: status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.status == 405 && !strings.Contains(w.Header().Get("Allow"), "GET") {
				t.Fatal("missing Allow header")
			}
			if tc.status == 429 && w.Header().Get("Retry-After") == "" {
				t.Fatal("missing Retry-After")
			}
		})
	}
}

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

func TestWebAllowsConfiguredPhotoOrigin(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	h := New(config.Config{JWTPublic: base64.StdEncoding.EncodeToString(pub), AccountsURL: "http://accounts:8080", ListingsURL: "http://listings:8080", S3PublicEndpoint: "http://127.0.0.1:19000"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "http://127.0.0.1:19000") {
		t.Fatal("configured object store blocked by CSP")
	}
}
