package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"example.com/marketplace/internal/platform/httpx"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"net/http"
	"strings"
	"time"
)

type Claims struct {
	UserID string
	jwt.RegisteredClaims
}
type Verifier struct {
	key              ed25519.PublicKey
	issuer, audience string
}

func NewVerifier(key ed25519.PublicKey, issuer, audience string) *Verifier {
	return &Verifier{key, issuer, audience}
}
func FromBase64(raw string) (*Verifier, error) {
	b, e := base64.StdEncoding.DecodeString(raw)
	if e != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key")
	}
	return NewVerifier(ed25519.PublicKey(b), "marketplace", "marketplace"), nil
}
func Sign(key ed25519.PrivateKey, id string, expires time.Time) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.RegisteredClaims{Subject: id, Issuer: "marketplace", Audience: jwt.ClaimStrings{"marketplace"}, ExpiresAt: jwt.NewNumericDate(expires), IssuedAt: jwt.NewNumericDate(time.Now())}).SignedString(key)
}
func (v *Verifier) Verify(raw string) (Claims, error) {
	c := Claims{}
	t, e := jwt.ParseWithClaims(raw, &c.RegisteredClaims, func(t *jwt.Token) (any, error) { return v.key, nil }, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience), jwt.WithExpirationRequired())
	if e != nil || !t.Valid || c.Subject == "" {
		return c, fmt.Errorf("invalid token")
	}
	c.UserID = c.Subject
	return c, nil
}

type userKey struct{}

func User(r *http.Request) string { s, _ := r.Context().Value(userKey{}).(string); return s }
func (v *Verifier) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if raw := r.Header.Get("Authorization"); raw != "" {
			if !strings.HasPrefix(raw, "Bearer ") {
				httpx.JSON(w, 401, map[string]any{"error": map[string]string{"code": "unauthorized", "message": "Войдите в аккаунт"}})
				return
			}
			c, e := v.Verify(strings.TrimPrefix(raw, "Bearer "))
			if e != nil {
				httpx.JSON(w, 401, map[string]any{"error": map[string]string{"code": "unauthorized", "message": "Сессия истекла"}})
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), userKey{}, c.UserID))
		}
		next.ServeHTTP(w, r)
	})
}
func Require(r *http.Request) (string, error) {
	id := User(r)
	if id == "" {
		return "", httpx.Fail(401, "unauthorized", "Войдите в аккаунт")
	}
	return id, nil
}
func Service(r *http.Request, expected string) error {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if expected == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
		return httpx.Fail(403, "forbidden", "Доступ запрещён")
	}
	return nil
}
