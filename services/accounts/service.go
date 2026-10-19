// Package accounts owns identities and rotating refresh sessions.
package accounts

import (
	"example.com/marketplace/internal/platform/httpx"
	"golang.org/x/crypto/bcrypt"
	"net/mail"
	"strings"
	"unicode/utf8"
)

func ValidateRegistration(name, email, password string) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(name)); n < 1 || n > 80 {
		return httpx.Bad("Имя: от 1 до 80 символов")
	}
	a, e := mail.ParseAddress(email)
	if e != nil || a.Address != email || len(email) > 254 {
		return httpx.Bad("Укажите корректный email")
	}
	if len(password) < 12 || len(password) > 72 {
		return httpx.Bad("Пароль: от 12 до 72 байт")
	}
	return nil
}
func HashPassword(s string) (string, error) {
	b, e := bcrypt.GenerateFromPassword([]byte(s), 12)
	return string(b), e
}
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
