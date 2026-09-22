package models

import "testing"

func TestOrderTransitions(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		to      string
		allowed bool
	}{
		{"submit draft", OrderDraft, OrderReadyForDispatch, true},
		{"cancel draft", OrderDraft, OrderCancelled, true},
		{"accept assignment", OrderReadyForDispatch, OrderAssigned, true},
		{"cancel ready order", OrderReadyForDispatch, OrderCancelled, true},
		{"reassign resources", OrderAssigned, OrderReadyForDispatch, true},
		{"start delivery", OrderAssigned, OrderInTransit, true},
		{"cancel assigned order", OrderAssigned, OrderCancelled, true},
		{"confirm arrival", OrderInTransit, OrderArrived, true},
		{"confirm delivery", OrderArrived, OrderCompleted, true},
		{"skip assignment", OrderReadyForDispatch, OrderInTransit, false},
		{"complete in transit", OrderInTransit, OrderCompleted, false},
		{"leave completed", OrderCompleted, OrderReadyForDispatch, false},
		{"leave cancelled", OrderCancelled, OrderDraft, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CanTransitionOrder(test.from, test.to); got != test.allowed {
				t.Fatalf("CanTransitionOrder(%q, %q) = %v, want %v", test.from, test.to, got, test.allowed)
			}
		})
	}
}
