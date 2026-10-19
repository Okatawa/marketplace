//go:build integration

package integration

import (
	"example.com/marketplace/services/accounts"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestAccountsRegisterLoginAndProfile(t *testing.T) {
	c := cfg(t)
	h := accounts.New(db(t, "accounts", accounts.Migrations), c)
	w, d := request(t, h, "POST", "/api/v1/auth/register", "", map[string]any{"name": "Анна", "email": "anna@example.com", "password": "strong-password"})
	if w.Code != 201 {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	access := d["access_token"].(string)
	w, me := request(t, h, "GET", "/api/v1/me", access, nil)
	if w.Code != 200 || me["email"] != "anna@example.com" {
		t.Fatal("profile", w.Code)
	}
	w, public := request(t, h, "GET", "/api/v1/users/"+me["id"].(string), "", nil)
	if w.Code != 200 || public["email"] != nil {
		t.Fatal("private email exposed")
	}
	w, _ = request(t, h, "POST", "/api/v1/auth/login", "", map[string]string{"email": "anna@example.com", "password": "wrong-password"})
	if w.Code != 401 {
		t.Fatal("bad login accepted", w.Code)
	}
}
func TestAccountsConcurrentRefreshAndLogout(t *testing.T) {
	c := cfg(t)
	h := accounts.New(db(t, "accounts", accounts.Migrations), c)
	w, _ := request(t, h, "POST", "/api/v1/auth/register", "", map[string]any{"name": "Анна", "email": "anna@example.com", "password": "strong-password"})
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no cookie")
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
			r.AddCookie(cookies[0])
			r.Header.Set("Origin", c.PublicURL)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)
	ok := 0
	for n := range codes {
		if n == 200 {
			ok++
		} else if n != 401 {
			t.Fatal("refresh", n)
		}
	}
	if ok != 1 {
		t.Fatal("expected one rotation", ok)
	}
	r := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
	r.AddCookie(cookies[0])
	r.Header.Set("Origin", "https://evil.test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("foreign origin accepted")
	}
	w, _ = request(t, h, "POST", "/api/v1/auth/login", "", map[string]string{"email": "anna@example.com", "password": "strong-password"})
	cookie := w.Result().Cookies()[0]
	r = httptest.NewRequest("POST", "/api/v1/auth/logout", strings.NewReader(""))
	r.AddCookie(cookie)
	r.Header.Set("Origin", c.PublicURL)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatal("logout", w.Code)
	}
	r = httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
	r.AddCookie(cookie)
	r.Header.Set("Origin", c.PublicURL)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("revoked session accepted")
	}
}
