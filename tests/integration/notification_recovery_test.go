//go:build integration

package integration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/marketplace/internal/platform/kafka"
	"example.com/marketplace/services/notifications"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// acceptedMail models the external SMTP acceptance boundary: the receiver
// keeps the accepted message even when the worker cannot record its success.
type acceptedMail struct{ bodies []string }

func (m *acceptedMail) Send(_ context.Context, address, subject, body string) error {
	if address != "recipient@example.test" || subject == "" || !strings.Contains(body, "/#chat/") {
		return errors.New("incorrect notification delivery arguments")
	}
	m.bodies = append(m.bodies, body)
	return nil
}

func notificationRecoveryFixture(t *testing.T) (*pgxpool.Pool, notifications.Worker, *acceptedMail, string) {
	t.Helper()
	c := cfg(t)
	pool := db(t, "notifications", notifications.Migrations)
	recipient := uuid.NewString()
	accounts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/users/"+recipient+"/notification-address" || r.Header.Get("Authorization") != "Bearer "+c.ServiceToken {
			http.Error(w, "unexpected request", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"email":"recipient@example.test"}`))
	}))
	t.Cleanup(accounts.Close)
	c.AccountsURL = accounts.URL
	conversation := uuid.NewString()
	event := kafka.New("message.created", conversation, 1, map[string]string{
		"message_id": uuid.NewString(), "conversation_id": conversation,
		"sender_id": uuid.NewString(), "recipient_id": recipient,
	})
	if err := (notifications.Inbox{DB: pool}).Accept(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	mail := &acceptedMail{}
	return pool, notifications.Worker{DB: pool, Config: c, Sender: mail}, mail, event.EventID
}

// A worker crash must not strand a processing job after its lease expires.
func TestNotificationsRecoverExpiredLease(t *testing.T) {
	ctx := context.Background()
	pool, worker, mail, id := notificationRecoveryFixture(t)
	_, err := pool.Exec(ctx, "UPDATE jobs SET state='processing',attempts=1,lease_owner=$2,lease_until=now()-interval '1 second' WHERE id=$1", id, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.Tick(ctx); err != nil || !worked {
		t.Fatal("expired lease was not recovered", worked, err)
	}
	var state string
	var attempts int
	var released bool
	if err := pool.QueryRow(ctx, "SELECT state,attempts,lease_owner IS NULL AND lease_until IS NULL FROM jobs WHERE id=$1", id).Scan(&state, &attempts, &released); err != nil {
		t.Fatal(err)
	}
	if state != "sent" || attempts != 2 || !released || len(mail.bodies) != 1 {
		t.Fatalf("recovered job: state=%s attempts=%d released=%v deliveries=%d", state, attempts, released, len(mail.bodies))
	}
	if worked, err := worker.Tick(ctx); err != nil || worked {
		t.Fatal("sent job was processed again", worked, err)
	}
}

// Exhausted jobs must leave processing without sending a sixth message.
func TestNotificationsExpiredFinalLeaseBecomesFailed(t *testing.T) {
	ctx := context.Background()
	pool, worker, mail, id := notificationRecoveryFixture(t)
	_, err := pool.Exec(ctx, "UPDATE jobs SET state='processing',attempts=5,lease_owner=$2,lease_until=now()-interval '1 second' WHERE id=$1", id, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.Tick(ctx); err != nil || worked {
		t.Fatal("exhausted expired job should not be delivered again", worked, err)
	}
	var state string
	var attempts int
	if err := pool.QueryRow(ctx, "SELECT state,attempts FROM jobs WHERE id=$1", id).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || attempts != 5 || len(mail.bodies) != 0 {
		t.Fatalf("exhausted job: state=%s attempts=%d deliveries=%d", state, attempts, len(mail.bodies))
	}
}

// SMTP and PostgreSQL cannot commit atomically. A crash in this gap may cause
// duplicate email; the event itself and the durable job still remain unique.
func TestNotificationsSMTPAcceptedThenDBFailure(t *testing.T) {
	ctx := context.Background()
	pool, worker, mail, id := notificationRecoveryFixture(t)
	_, err := pool.Exec(ctx, `CREATE FUNCTION fail_delivery_ack() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
	IF NEW.state='sent' THEN RAISE EXCEPTION 'simulated DB acknowledgement failure'; END IF; RETURN NEW; END $$;
	CREATE TRIGGER fail_delivery_ack BEFORE UPDATE ON jobs FOR EACH ROW EXECUTE FUNCTION fail_delivery_ack();`)
	if err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.Tick(ctx); !worked || err == nil || !strings.Contains(err.Error(), "simulated DB acknowledgement failure") {
		t.Fatal("expected failure recording accepted SMTP delivery", worked, err)
	}
	var state string
	if err := pool.QueryRow(ctx, "SELECT state FROM jobs WHERE id=$1", id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "processing" || len(mail.bodies) != 1 {
		t.Fatalf("SMTP acceptance must survive DB failure: state=%s deliveries=%d", state, len(mail.bodies))
	}
	// No immediate retry while another worker still owns a valid lease.
	if worked, err := worker.Tick(ctx); err != nil || worked {
		t.Fatal("active lease was ignored", worked, err)
	}
	if _, err := pool.Exec(ctx, "DROP TRIGGER fail_delivery_ack ON jobs; UPDATE jobs SET lease_until=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	if worked, err := worker.Tick(ctx); err != nil || !worked {
		t.Fatal("accepted delivery was not retried after lease expiration", worked, err)
	}
	var count, attempts int
	if err := pool.QueryRow(ctx, "SELECT state,attempts,(SELECT count(*) FROM jobs) FROM jobs WHERE id=$1", id).Scan(&state, &attempts, &count); err != nil {
		t.Fatal(err)
	}
	if state != "sent" || attempts != 2 || count != 1 || len(mail.bodies) != 2 || mail.bodies[0] != mail.bodies[1] {
		t.Fatalf("retry result: state=%s attempts=%d jobs=%d deliveries=%d", state, attempts, count, len(mail.bodies))
	}
}
