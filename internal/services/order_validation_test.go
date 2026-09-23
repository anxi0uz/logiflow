package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/google/uuid"
)

func TestCreateOrderRejectsInvalidInputBeforeExternalCalls(t *testing.T) {
	origin := "Origin"
	negative := float32(-1)
	now := time.Now()
	for _, req := range []api.OrderCreate{
		{DestinationAddress: "Destination"},
		{OriginAddress: &origin, DestinationAddress: "Destination", WeightKg: &negative},
		{OriginAddress: &origin, DestinationAddress: "Destination", PickupFrom: &now, PickupTo: &now},
	} {
		_, err := (&OrderService{}).CreateOrder(context.Background(), req, uuid.New())
		if !errors.Is(err, ErrInvalidOrderInput) {
			t.Fatalf("invalid order accepted: %+v, err=%v", req, err)
		}
	}
}

func TestUpdateDraftOrderRejectsEmptyPatch(t *testing.T) {
	_, err := (&OrderService{}).UpdateDraftOrder(context.Background(), uuid.New(), uuid.New(), "client", api.OrderDraftUpdate{})
	if !errors.Is(err, ErrInvalidOrderInput) {
		t.Fatalf("empty patch: %v", err)
	}
}
