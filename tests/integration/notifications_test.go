//go:build integration

package integration

import (
	"context"
	"errors"
	"example.com/marketplace/internal/platform/kafka"
	"example.com/marketplace/services/notifications"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"testing"
)

type failedSender struct{}

func (failedSender) Send(context.Context, string, string, string) error {
	return errors.New("SMTP unavailable")
}
func TestNotificationsDuplicateAndRetryLimit(t *testing.T) {
	c := cfg(t)
	p := db(t, "notifications", notifications.Migrations)
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"email":"anna@example.com"}`))
	}))
	defer a.Close()
	c.AccountsURL = a.URL
	in := notifications.Inbox{DB: p}
	conv := uuid.NewString()
	event := kafka.New("message.created", conv, 1, map[string]string{"message_id": uuid.NewString(), "conversation_id": conv, "sender_id": uuid.NewString(), "recipient_id": uuid.NewString()})
	for i := 0; i < 2; i++ {
		if e := in.Accept(context.Background(), event); e != nil {
			t.Fatal(e)
		}
	}
	var count int
	p.QueryRow(context.Background(), "SELECT count(*) FROM jobs").Scan(&count)
	if count != 1 {
		t.Fatal("duplicate notification", count)
	}
	worker := notifications.Worker{DB: p, Config: c, Sender: failedSender{}}
	for i := 0; i < 5; i++ {
		if _, e := worker.Tick(context.Background()); e != nil {
			t.Fatal(e)
		}
		p.Exec(context.Background(), "UPDATE jobs SET next_attempt=now()")
	}
	var state string
	var attempts int
	if e := p.QueryRow(context.Background(), "SELECT state,attempts FROM jobs").Scan(&state, &attempts); e != nil || state != "failed" || attempts != 5 {
		t.Fatal(state, attempts, e)
	}
	if worked, e := worker.Tick(context.Background()); e != nil || worked {
		t.Fatal("sixth send attempted", worked, e)
	}
}
