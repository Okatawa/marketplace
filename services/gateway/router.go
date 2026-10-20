// Package gateway exposes an explicit allowlist of public routes.
package gateway

import (
	"context"
	"errors"
	"example.com/marketplace/internal/platform/auth"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/internal/platform/httpx"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	count int
	start time.Time
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]bucket
}

func (l *limiter) allow(key string, max int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b := l.entries[key]
	if now.Sub(b.start) >= time.Minute {
		b = bucket{start: now}
	}
	b.count++
	l.entries[key] = b
	if len(l.entries) > 10000 {
		for k, v := range l.entries {
			if now.Sub(v.start) >= time.Minute {
				delete(l.entries, k)
			}
		}
		if len(l.entries) > 10000 {
			delete(l.entries, key)
			return false
		}
	}
	return b.count <= max
}
func New(c config.Config) http.Handler {
	mux := http.NewServeMux()
	routes := []struct {
		target   string
		patterns []string
	}{{c.AccountsURL, []string{"POST /api/v1/auth/register", "POST /api/v1/auth/login", "POST /api/v1/auth/refresh", "POST /api/v1/auth/logout", "GET /api/v1/me", "PATCH /api/v1/me", "GET /api/v1/users/{id}"}}, {c.ListingsURL, []string{"GET /api/v1/categories", "POST /api/v1/listings", "GET /api/v1/listings/{id}", "PATCH /api/v1/listings/{id}", "POST /api/v1/listings/{id}/publish", "POST /api/v1/listings/{id}/withdraw", "GET /api/v1/me/listings", "POST /api/v1/listings/{id}/photos", "DELETE /api/v1/listings/{id}/photos/{photo_id}", "GET /api/v1/listings/{id}/photos/{photo_id}"}}, {config.Env("SEARCH_URL", "http://search:8080"), []string{"GET /api/v1/search"}}, {config.Env("MESSAGING_URL", "http://messaging:8080"), []string{"POST /api/v1/conversations", "GET /api/v1/conversations", "GET /api/v1/conversations/{id}/messages", "POST /api/v1/conversations/{id}/messages"}}}
	for _, route := range routes {
		target, patterns := route.target, route.patterns
		u, e := url.Parse(target)
		if e != nil {
			panic(e)
		}
		p := httputil.NewSingleHostReverseProxy(u)
		p.Transport = otelhttp.NewTransport(http.DefaultTransport)
		p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
			status := 502
			if errors.Is(e, context.DeadlineExceeded) {
				status = 504
			}
			httpx.WriteError(w, r, httpx.Fail(status, "upstream", "Сервис временно недоступен"))
		}
		for _, pattern := range patterns {
			mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httpx.ObserveRoute(r, pattern)
				p.ServeHTTP(w, r)
			}))
		}
	}
	v, e := auth.FromBase64(c.JWTPublic)
	if e != nil {
		panic(e)
	}
	api := v.Optional(httpx.JSONMux(mux))
	l := &limiter{entries: map[string]bucket{}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			serveWeb(w, r, c.S3PublicEndpoint)
			return
		}
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		max := 120
		key := ip
		if strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
			max = 10
			key += "/auth"
		}
		if !l.allow(key, max) {
			w.Header().Set("Retry-After", "60")
			httpx.WriteError(w, r, httpx.Fail(429, "rate_limit", "Слишком много запросов. Подождите минуту"))
			return
		}
		for _, h := range []string{"X-User-ID", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded", "X-Service-Token"} {
			r.Header.Del(h)
		}
		w.Header().Set("X-Request-ID", httpx.RequestID(r))
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		api.ServeHTTP(w, r.WithContext(ctx))
	})
}
