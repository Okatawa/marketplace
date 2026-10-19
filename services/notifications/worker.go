package notifications

import (
	"context"
	"errors"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/internal/platform/httpx"
	"example.com/marketplace/internal/platform/kafka"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Sender interface {
	Send(context.Context, string, string, string) error
}
type Worker struct {
	DB     *pgxpool.Pool
	Config config.Config
	Sender Sender
}

func (w Worker) Tick(ctx context.Context) (bool, error) {
	_, e := w.DB.Exec(ctx, "UPDATE jobs SET state='failed',last_error='lease_expired_after_final_attempt' WHERE state='processing' AND lease_until<now() AND attempts>=5")
	if e != nil {
		return false, e
	}
	owner := uuid.NewString()
	var id, recipient, conversation string
	var attempts int
	e = w.DB.QueryRow(ctx, `WITH candidate AS (SELECT id FROM jobs WHERE attempts<5 AND ((state='pending' AND next_attempt<=now()) OR (state='processing' AND lease_until<now())) ORDER BY next_attempt FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE jobs SET state='processing',lease_owner=$1,lease_until=now()+interval '60 seconds',attempts=attempts+1 FROM candidate WHERE jobs.id=candidate.id RETURNING jobs.id::text,recipient_id::text,conversation_id::text,attempts`, owner).Scan(&id, &recipient, &conversation, &attempts)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	var address struct {
		Email string `json:"email"`
	}
	work, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	sendErr := httpx.GetJSON(work, w.Config.AccountsURL+"/internal/v1/users/"+recipient+"/notification-address", w.Config.ServiceToken, &address)
	if sendErr == nil {
		sendErr = w.Sender.Send(work, address.Email, "Новое сообщение в Маркете", fmt.Sprintf("Вам пришло новое сообщение. Откройте переписку: %s/#chat/%s\n", w.Config.PublicURL, conversation))
	}
	state, reason := "sent", ""
	next := time.Now()
	if sendErr != nil {
		state = "pending"
		reason = "delivery_failed"
		delays := []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute, 30 * time.Minute}
		var he *httpx.Error
		if attempts >= 5 || (errors.As(sendErr, &he) && he.Status == 404) {
			state = "failed"
		} else {
			next = next.Add(delays[attempts-1])
		}
	}
	_, e = w.DB.Exec(ctx, "UPDATE jobs SET state=$3,last_error=$4,next_attempt=$5,lease_until=NULL,lease_owner=NULL WHERE id=$1 AND lease_owner=$2 AND state='processing'", id, owner, state, reason, next)
	return true, e
}
func (w Worker) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		worked, e := w.Tick(ctx)
		if e != nil {
			return e
		}
		if !worked && !kafka.Pause(ctx, time.Second) {
			break
		}
	}
	return ctx.Err()
}
