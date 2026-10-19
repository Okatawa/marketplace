package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"github.com/golang-jwt/jwt/v5"
	"testing"
	"time"
)

func TestTokenVerification(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	v := NewVerifier(pub, "marketplace", "marketplace")
	token, err := Sign(priv, "user-1", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	c, err := v.Verify(token)
	if err != nil || c.UserID != "user-1" {
		t.Fatalf("verify: %v %v", c, err)
	}
	expired, _ := Sign(priv, "user-1", time.Now().Add(-time.Minute))
	if _, err = v.Verify(expired); err == nil {
		t.Fatal("expired accepted")
	}
}
func TestWrongJWTAlgorithm(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	raw, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "admin", "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(pub))
	if _, err := NewVerifier(pub, "marketplace", "marketplace").Verify(raw); err == nil {
		t.Fatal("algorithm confusion")
	}
}
