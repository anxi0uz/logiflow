package models

import (
	"time"

	"github.com/google/uuid"
)

const (
	AssignmentPendingAcceptance = "pending_acceptance"
	AssignmentAccepted          = "accepted"
	AssignmentActive            = "active"
	AssignmentRejected          = "rejected"
	AssignmentReleased          = "released"
	AssignmentExpired           = "expired"
	AssignmentCompleted         = "completed"
)

type Assignment struct {
	ID                      uuid.UUID  `db:"id" json:"id"`
	OrderID                 uuid.UUID  `db:"order_id" json:"orderId"`
	DriverID                uuid.UUID  `db:"driver_id" json:"driverId"`
	VehicleID               uuid.UUID  `db:"vehicle_id" json:"vehicleId"`
	Status                  string     `db:"status" json:"status"`
	PlannedFrom             time.Time  `db:"planned_from" json:"plannedFrom"`
	PlannedTo               time.Time  `db:"planned_to" json:"plannedTo"`
	CreatedByUserID         uuid.UUID  `db:"created_by_user_id" json:"createdByUserId"`
	Source                  string     `db:"source" json:"source"`
	RecommendationID        *string    `db:"recommendation_id" json:"recommendationId,omitempty"`
	RecommendationCreatedAt *time.Time `db:"recommendation_created_at" json:"recommendationCreatedAt,omitempty"`
	AssignedAt              time.Time  `db:"assigned_at" json:"assignedAt"`
	OfferExpiresAt          time.Time  `db:"offer_expires_at" json:"offerExpiresAt"`
	AcceptedAt              *time.Time `db:"accepted_at" json:"acceptedAt,omitempty"`
	RejectedAt              *time.Time `db:"rejected_at" json:"rejectedAt,omitempty"`
	ReleasedAt              *time.Time `db:"released_at" json:"releasedAt,omitempty"`
	StartedAt               *time.Time `db:"started_at" json:"startedAt,omitempty"`
	CompletedAt             *time.Time `db:"completed_at" json:"completedAt,omitempty"`
	RecipientName           *string    `db:"recipient_name" json:"recipientName,omitempty"`
	DeliveryComment         *string    `db:"delivery_comment" json:"deliveryComment,omitempty"`
	RejectionReasonCode     *string    `db:"rejection_reason_code" json:"rejectionReasonCode,omitempty"`
	RejectionComment        *string    `db:"rejection_comment" json:"rejectionComment,omitempty"`
	ReleaseReasonCode       *string    `db:"release_reason_code" json:"releaseReasonCode,omitempty"`
}

type OrderStatusHistory struct {
	ID          uuid.UUID  `db:"id"`
	OrderID     uuid.UUID  `db:"order_id"`
	FromStatus  *string    `db:"from_status"`
	ToStatus    string     `db:"to_status"`
	ActorUserID *uuid.UUID `db:"actor_user_id"`
	ReasonCode  *string    `db:"reason_code"`
	CreatedAt   time.Time  `db:"created_at"`
}

type DriverShift struct {
	ID       uuid.UUID `db:"id"`
	DriverID uuid.UUID `db:"driver_id"`
	StartsAt time.Time `db:"starts_at"`
	EndsAt   time.Time `db:"ends_at"`
}
