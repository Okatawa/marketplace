package kafka

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEnvelopeRejectsInvalid(t *testing.T) {
	e := Envelope{EventID: "a", EventType: "listing.changed", SchemaVersion: 1, OccurredAt: time.Now(), AggregateID: "b", AggregateVersion: 1, Payload: json.RawMessage(`{}`)}
	if e.Validate() == nil {
		t.Fatal("invalid UUID accepted")
	}
	e.EventID = "bd1fc33c-19bb-4133-aaed-cc26b7027192"
	e.AggregateID = "ed365080-6dca-4570-ab9b-616e93470e5b"
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	e.SchemaVersion = 99
	if e.Validate() == nil {
		t.Fatal("unknown schema accepted")
	}
}
