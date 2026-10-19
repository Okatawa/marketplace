package accounts

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"example.com/marketplace/internal/platform/auth"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/internal/platform/httpx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type Service struct {
	db      *pgxpool.Pool
	cfg     config.Config
	private ed25519.PrivateKey
}
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

func New(db *pgxpool.Pool, c config.Config) http.Handler {
	key, _ := base64.StdEncoding.DecodeString(c.JWTPrivate)
	s := &Service{db, c, ed25519.PrivateKey(key)}
	m := http.NewServeMux()
	v, e := auth.FromBase64(c.JWTPublic)
	if e != nil {
		panic(e)
	}
	m.HandleFunc("POST /api/v1/auth/register", httpx.Wrap(s.register))
	m.HandleFunc("POST /api/v1/auth/login", httpx.Wrap(s.login))
	m.HandleFunc("POST /api/v1/auth/refresh", httpx.Wrap(s.refresh))
	m.HandleFunc("POST /api/v1/auth/logout", httpx.Wrap(s.logout))
	m.Handle("GET /api/v1/me", v.Optional(httpx.Wrap(s.me)))
	m.Handle("PATCH /api/v1/me", v.Optional(httpx.Wrap(s.update)))
	m.HandleFunc("GET /api/v1/users/{id}", httpx.Wrap(s.profile))
	m.HandleFunc("GET /internal/v1/users/{id}/notification-address", httpx.Wrap(s.address))
	return m
}
func (s *Service) register(w http.ResponseWriter, r *http.Request) error {
	var in struct{ Name, Email, Password string }
	if e := httpx.Decode(w, r, &in); e != nil {
		return e
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if e := ValidateRegistration(in.Name, in.Email, in.Password); e != nil {
		return e
	}
	hash, e := HashPassword(in.Password)
	if e != nil {
		return e
	}
	id := uuid.NewString()
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	if _, e = tx.Exec(r.Context(), "INSERT INTO users(id,name,email,password_hash) VALUES($1,$2,$3,$4)", id, in.Name, in.Email, hash); e != nil {
		return e
	}
	raw, digest := newRefresh()
	if _, e = tx.Exec(r.Context(), "INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)", digest, id, time.Now().Add(30*24*time.Hour)); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	return s.respondSession(w, id, raw, 201)
}
func (s *Service) login(w http.ResponseWriter, r *http.Request) error {
	var in struct{ Email, Password string }
	if e := httpx.Decode(w, r, &in); e != nil {
		return e
	}
	var id, hash string
	e := s.db.QueryRow(r.Context(), "SELECT id::text,password_hash FROM users WHERE email=$1", strings.ToLower(strings.TrimSpace(in.Email))).Scan(&id, &hash)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if e != nil || !CheckPassword(hash, in.Password) {
		return httpx.Fail(401, "credentials", "Неверный email или пароль")
	}
	raw, digest := newRefresh()
	_, e = s.db.Exec(r.Context(), "INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)", digest, id, time.Now().Add(30*24*time.Hour))
	if e != nil {
		return e
	}
	return s.respondSession(w, id, raw, 200)
}
func newRefresh() (string, string) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	return raw, refreshHash(raw)
}
func refreshHash(raw string) string { b := sha256.Sum256([]byte(raw)); return hex.EncodeToString(b[:]) }
func (s *Service) origin(r *http.Request) error {
	if r.Header.Get("Origin") != s.cfg.PublicURL {
		return httpx.Fail(403, "origin", "Недопустимый источник запроса")
	}
	return nil
}
func (s *Service) refresh(w http.ResponseWriter, r *http.Request) error {
	if e := s.origin(r); e != nil {
		return e
	}
	cookie, e := r.Cookie("refresh")
	if e != nil {
		return httpx.Fail(401, "session", "Сессия истекла")
	}
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	var id string
	e = tx.QueryRow(r.Context(), "DELETE FROM sessions WHERE token_hash=$1 AND expires_at>now() RETURNING user_id::text", refreshHash(cookie.Value)).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return httpx.Fail(401, "session", "Сессия истекла")
	}
	if e != nil {
		return e
	}
	raw, digest := newRefresh()
	if _, e = tx.Exec(r.Context(), "INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)", digest, id, time.Now().Add(30*24*time.Hour)); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	return s.respondSession(w, id, raw, 200)
}
func (s *Service) respondSession(w http.ResponseWriter, id, raw string, status int) error {
	access, e := auth.Sign(s.private, id, time.Now().Add(15*time.Minute))
	if e != nil {
		return e
	}
	http.SetCookie(w, &http.Cookie{Name: "refresh", Value: raw, Path: "/api/v1/auth", HttpOnly: true, Secure: s.cfg.Production, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600})
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, status, map[string]string{"access_token": access})
	return nil
}
func (s *Service) logout(w http.ResponseWriter, r *http.Request) error {
	if e := s.origin(r); e != nil {
		return e
	}
	if c, e := r.Cookie("refresh"); e == nil {
		if _, e = s.db.Exec(r.Context(), "DELETE FROM sessions WHERE token_hash=$1", refreshHash(c.Value)); e != nil {
			return e
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "refresh", Path: "/api/v1/auth", HttpOnly: true, Secure: s.cfg.Production, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.WriteHeader(204)
	return nil
}
func (s *Service) me(w http.ResponseWriter, r *http.Request) error {
	id, e := auth.Require(r)
	if e != nil {
		return e
	}
	var u User
	e = s.db.QueryRow(r.Context(), "SELECT id::text,name,email FROM users WHERE id=$1", id).Scan(&u.ID, &u.Name, &u.Email)
	if e != nil {
		return e
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, 200, u)
	return nil
}
func (s *Service) update(w http.ResponseWriter, r *http.Request) error {
	id, e := auth.Require(r)
	if e != nil {
		return e
	}
	var in struct{ Name string }
	if e = httpx.Decode(w, r, &in); e != nil {
		return e
	}
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 80 {
		return httpx.Bad("Имя: от 1 до 80 символов")
	}
	if _, e = s.db.Exec(r.Context(), "UPDATE users SET name=$1 WHERE id=$2", in.Name, id); e != nil {
		return e
	}
	return s.me(w, r)
}
func (s *Service) profile(w http.ResponseWriter, r *http.Request) error {
	var u User
	if e := s.db.QueryRow(r.Context(), "SELECT id::text,name FROM users WHERE id=$1", r.PathValue("id")).Scan(&u.ID, &u.Name); e != nil {
		return e
	}
	httpx.JSON(w, 200, u)
	return nil
}
func (s *Service) address(w http.ResponseWriter, r *http.Request) error {
	if e := auth.Service(r, s.cfg.ServiceToken); e != nil {
		return e
	}
	var email string
	if e := s.db.QueryRow(r.Context(), "SELECT email FROM users WHERE id=$1", r.PathValue("id")).Scan(&email); e != nil {
		return e
	}
	httpx.JSON(w, 200, map[string]string{"email": email})
	return nil
}
