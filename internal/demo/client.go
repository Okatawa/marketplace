package demo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

type Client struct {
	Base, Token string
	HTTP        *http.Client
}

func New(base string) *Client {
	return &Client{Base: base, HTTP: &http.Client{Timeout: 15 * time.Second}}
}
func (c *Client) Do(method, path string, body any) (map[string]any, int, error) {
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return nil, 0, e
		}
		reader = bytes.NewReader(b)
	}
	r, e := http.NewRequest(method, c.Base+"/api/v1"+path, reader)
	if e != nil {
		return nil, 0, e
	}
	r.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		r.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if method == "POST" {
		r.Header.Set("Idempotency-Key", fmt.Sprintf("demo-%d", time.Now().UnixNano()))
	}
	resp, e := c.HTTP.Do(r)
	if e != nil {
		return nil, 0, e
	}
	defer resp.Body.Close()
	var d map[string]any
	if resp.StatusCode != 204 {
		if e = json.NewDecoder(resp.Body).Decode(&d); e != nil {
			return nil, resp.StatusCode, e
		}
	}
	if token, ok := d["access_token"].(string); ok {
		c.Token = token
	}
	if resp.StatusCode >= 400 {
		return d, resp.StatusCode, fmt.Errorf("%s %s returned %d", method, path, resp.StatusCode)
	}
	return d, resp.StatusCode, nil
}
func (c *Client) Account(name, email, password string) error {
	_, status, e := c.Do("POST", "/auth/register", map[string]string{"name": name, "email": email, "password": password})
	if status == 409 {
		_, _, e = c.Do("POST", "/auth/login", map[string]string{"email": email, "password": password})
	}
	return e
}
func (c *Client) Upload(id string, png []byte) error {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, e := w.CreateFormFile("file", "photo.png")
	if e != nil {
		return e
	}
	part.Write(png)
	w.Close()
	r, e := http.NewRequest("POST", c.Base+"/api/v1/listings/"+id+"/photos", &b)
	if e != nil {
		return e
	}
	r.Header.Set("Authorization", "Bearer "+c.Token)
	r.Header.Set("Content-Type", w.FormDataContentType())
	resp, e := c.HTTP.Do(r)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		return fmt.Errorf("upload returned %d", resp.StatusCode)
	}
	return nil
}
