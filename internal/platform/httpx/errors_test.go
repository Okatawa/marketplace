package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestErrorHasRequestID(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Request-ID", "untrusted-client-text")
	w := httptest.NewRecorder()
	Wrap(func(http.ResponseWriter, *http.Request) error { return Bad("Ошибка") })(w, r)
	var body struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !ValidID(body.RequestID) || body.RequestID != w.Header().Get("X-Request-ID") {
		t.Fatal("missing safe request ID")
	}
}
