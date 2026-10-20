//go:build smoke

package smoke

import (
	"encoding/json"
	"example.com/marketplace/internal/demo"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMarketplaceJourney(t *testing.T) {
	base := os.Getenv("BASE_URL")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	seller, buyer := demo.New(base), demo.New(base)
	suffix := fmt.Sprint(time.Now().UnixNano())
	if e := seller.Account("Анна", "seller-"+suffix+"@example.com", "smoke-test-password"); e != nil {
		t.Fatal(e)
	}
	if e := buyer.Account("Борис", "buyer-"+suffix+"@example.com", "smoke-test-password"); e != nil {
		t.Fatal(e)
	}
	title := "Велосипед " + suffix
	l, _, e := seller.Do("POST", "/listings", map[string]any{"title": title, "description": "Проверка сквозного сценария", "category_id": "sport", "price_minor": 1500000})
	if e != nil {
		t.Fatal(e)
	}
	id := l["id"].(string)
	if e = seller.Upload(id, demo.Photo(0)); e != nil {
		t.Fatal(e)
	}
	l, _, e = seller.Do("GET", "/listings/"+id, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = seller.Do("POST", "/listings/"+id+"/publish", map[string]any{"expected_version": l["version"]}); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(40 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		d, _, e := buyer.Do("GET", "/search?q="+suffix, nil)
		if e == nil && len(d["items"].([]any)) == 1 {
			found = true
			break
		}
		time.Sleep(time.Second)
	}
	if !found {
		t.Fatal("listing not projected through Kafka")
	}
	c, _, e := buyer.Do("POST", "/conversations", map[string]string{"listing_id": id})
	if e != nil {
		t.Fatal(e)
	}
	cid := c["id"].(string)
	if _, _, e = buyer.Do("POST", "/conversations/"+cid+"/messages", map[string]string{"body": "Здравствуйте! Ещё продаётся?"}); e != nil {
		t.Fatal(e)
	}
	d, _, e := seller.Do("GET", "/conversations/"+cid+"/messages", nil)
	if e != nil || len(d["items"].([]any)) != 1 {
		t.Fatal("message not delivered", e)
	}
	mailpit := os.Getenv("MAILPIT_URL")
	if mailpit == "" {
		mailpit = "http://127.0.0.1:8025"
	}
	client := &http.Client{Timeout: 5 * time.Second}
	delivered := false
	deadline = time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(mailpit + "/api/v1/search?query=" + url.QueryEscape("to:seller-"+suffix+"@example.com"))
		if err == nil {
			var inbox struct {
				Messages []struct{ ID string }
			}
			json.NewDecoder(resp.Body).Decode(&inbox)
			resp.Body.Close()
			for _, message := range inbox.Messages {
				detail, err := client.Get(mailpit + "/api/v1/message/" + message.ID)
				if err != nil {
					continue
				}
				var mail struct{ Text string }
				json.NewDecoder(detail.Body).Decode(&mail)
				detail.Body.Close()
				if strings.Contains(mail.Text, "/#chat/"+cid) && !strings.Contains(mail.Text, "Ещё продаётся?") {
					delivered = true
				}
			}
		}
		if delivered {
			break
		}
		time.Sleep(time.Second)
	}
	if !delivered {
		t.Fatal("notification email with private conversation link not received")
	}
	t.Log("Published listing and private conversation", id, cid)
}
