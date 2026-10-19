package telemetry

import (
	"bytes"
	"github.com/prometheus/client_golang/prometheus"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPLogsExcludeSecrets(t *testing.T) {
	var log bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&log, nil))
	m := http.NewServeMux()
	m.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	h := WrapHTTP(m, logger, prometheus.NewRegistry())
	r := httptest.NewRequest("POST", "/login?token=private-query", strings.NewReader(`{"password":"private-password"}`))
	r.Header.Set("Authorization", "Bearer private-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	for _, secret := range []string{"private-query", "private-password", "private-token"} {
		if strings.Contains(log.String(), secret) {
			t.Fatal("secret in request log")
		}
	}
	if !strings.Contains(log.String(), "http_request") {
		t.Fatal("missing request log")
	}
}
