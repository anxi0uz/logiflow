package events

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestInvalidDocumentReadyIsolated(t *testing.T) {
	for _, data := range [][]byte{
		[]byte("{broken"),
		[]byte(`{"event_id":"00000000-0000-0000-0000-000000000000"}`),
		[]byte(`{"event_id":"not-a-uuid"}`),
	} {
		called := false
		err := handleDocumentReady(data,
			func(data []byte) error { return receiveDocumentReady(context.Background(), nil, data) },
			func(reason string) error { called = reason != ""; return nil },
		)
		if err != nil || !called {
			t.Fatalf("invalid event not isolated: err=%v called=%v", err, called)
		}
	}
	isolationFailure := errors.New("NATS unavailable")
	err := handleDocumentReady([]byte("{broken"),
		func(data []byte) error { return receiveDocumentReady(context.Background(), nil, data) },
		func(string) error { return isolationFailure },
	)
	if !errors.Is(err, isolationFailure) {
		t.Fatalf("isolation failure must retry: %v", err)
	}
}

func TestDocumentReadyTransientFailureThenDuplicate(t *testing.T) {
	event := DocumentReady{EventID: uuid.New(), DocumentID: uuid.New(), OrderID: uuid.New(), UserID: uuid.New(), Type: "delivery_confirmation"}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	temporary := errors.New("database unavailable")
	attempts, stored, isolated := 0, 0, 0
	receive := func([]byte) error {
		attempts++
		if attempts == 1 {
			return temporary
		}
		if stored == 0 {
			stored++
		}
		return nil
	}
	isolate := func(string) error { isolated++; return nil }
	if err := handleDocumentReady(data, receive, isolate); !errors.Is(err, temporary) {
		t.Fatalf("transient error must retry: %v", err)
	}
	for range 2 {
		if err := handleDocumentReady(data, receive, isolate); err != nil {
			t.Fatal(err)
		}
	}
	if attempts != 3 || stored != 1 || isolated != 0 {
		t.Fatalf("attempts=%d stored=%d isolated=%d", attempts, stored, isolated)
	}
}
