//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"example.com/marketplace/internal/platform/auth"
	"example.com/marketplace/internal/platform/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func localEnv(t *testing.T) map[string]string {
	t.Helper()
	f, e := os.Open("../../.local/.env")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	m := map[string]string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		k, v, ok := strings.Cut(s.Text(), "=")
		if ok {
			m[k] = v
		}
	}
	return m
}
func cfg(t *testing.T) config.Config {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	e := localEnv(t)
	return config.Config{JWTPublic: base64.StdEncoding.EncodeToString(pub), JWTPrivate: base64.StdEncoding.EncodeToString(priv), PublicURL: "http://localhost:8080", ServiceToken: "test-service-token", KafkaBrokers: []string{"localhost:19092"}, S3Endpoint: "http://localhost:9000", S3PublicEndpoint: "http://localhost:9000", S3Access: e["S3_ACCESS_KEY"], S3Secret: e["S3_SECRET_KEY"], S3Bucket: "test-" + uuid.NewString(), SMTPAddr: "localhost:1025", SMTPFrom: "marketplace@localhost"}
}
func db(t *testing.T, name string, migrations fs.FS) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	env := localEnv(t)
	dsn := "postgres://" + name + ":" + env["APP_DB_PASSWORD"] + "@localhost:15432/" + name + "?sslmode=disable"
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal("db config", e)
	}
	schema := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal("create test schema", e)
	}
	pconf, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	pconf.ConnConfig.RuntimeParams["search_path"] = schema
	p, e := pgxpool.NewWithConfig(ctx, pconf)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { p.Close(); admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })
	files, e := fs.Glob(migrations, "migrations/*.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range files {
		b, _ := fs.ReadFile(migrations, path)
		if _, e = p.Exec(ctx, string(b)); e != nil {
			t.Fatal(filepath.Base(path), e)
		}
	}
	return p
}
func token(t *testing.T, c config.Config, id string) string {
	t.Helper()
	b, _ := base64.StdEncoding.DecodeString(c.JWTPrivate)
	raw, e := auth.Sign(ed25519.PrivateKey(b), id, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func request(t *testing.T, h http.Handler, method, path, bearer string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, reader)
	r.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var data map[string]any
	json.Unmarshal(w.Body.Bytes(), &data)
	return w, data
}
