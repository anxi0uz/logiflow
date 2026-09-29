package events

import (
	"context"
	"errors"
	"testing"
)

func TestInvalidDispatchResultIsolatedBeforeDatabase(t *testing.T) {
	for _, payload := range [][]byte{
		[]byte("{broken"),
		[]byte(`{"event_id":"00000000-0000-0000-0000-000000000000"}`),
		[]byte(`{"event_id":"not-a-uuid"}`),
	} {
		err := receiveDispatchRecommended(context.Background(), nil, payload)
		var invalid invalidEventError
		if !errors.As(err, &invalid) {
			t.Fatalf("payload %q should be invalid, got %v", payload, err)
		}
	}
}
