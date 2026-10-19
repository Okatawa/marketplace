package notifications

import (
	"context"
	"encoding/json"
	"example.com/marketplace/internal/platform/httpx"
	"example.com/marketplace/internal/platform/kafka"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Inbox struct{ DB *pgxpool.Pool }

func (i Inbox) Accept(ctx context.Context, e kafka.Envelope) error {
	if err := e.Validate(); err != nil {
		return err
	}
	var p struct {
		MessageID      string `json:"message_id"`
		ConversationID string `json:"conversation_id"`
		SenderID       string `json:"sender_id"`
		RecipientID    string `json:"recipient_id"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return kafka.PermanentError{Err: err}
	}
	if e.EventType != "message.created" || p.ConversationID != e.AggregateID || !httpx.ValidID(p.RecipientID) || !httpx.ValidID(p.MessageID) || !httpx.ValidID(p.SenderID) {
		return kafka.PermanentError{Err: fmt.Errorf("invalid message event")}
	}
	tx, err := i.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, "INSERT INTO inbox(event_id) VALUES($1) ON CONFLICT DO NOTHING", e.EventID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if _, err = tx.Exec(ctx, "INSERT INTO jobs(id,recipient_id,conversation_id) VALUES($1,$2,$3)", e.EventID, p.RecipientID, p.ConversationID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
