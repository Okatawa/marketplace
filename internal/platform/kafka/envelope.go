// Package kafka contains transport contracts, not service domain models.
package kafka

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"time"
)

type Envelope struct {
	EventID          string          `json:"event_id"`
	EventType        string          `json:"event_type"`
	SchemaVersion    int             `json:"schema_version"`
	OccurredAt       time.Time       `json:"occurred_at"`
	AggregateID      string          `json:"aggregate_id"`
	AggregateVersion int64           `json:"aggregate_version"`
	Payload          json.RawMessage `json:"payload"`
}

func (e Envelope) Validate() error {
	if _, err := uuid.Parse(e.EventID); err != nil {
		return PermanentError{err}
	}
	if _, err := uuid.Parse(e.AggregateID); err != nil {
		return PermanentError{err}
	}
	if e.SchemaVersion != 1 || e.AggregateVersion < 1 || e.EventType == "" || e.OccurredAt.IsZero() || !json.Valid(e.Payload) {
		return PermanentError{fmt.Errorf("invalid event envelope")}
	}
	return nil
}

type PermanentError struct{ Err error }

func (e PermanentError) Error() string { return e.Err.Error() }
func New(kind, id string, version int64, payload any) Envelope {
	raw, _ := json.Marshal(payload)
	return Envelope{uuid.NewString(), kind, 1, time.Now().UTC(), id, version, raw}
}
