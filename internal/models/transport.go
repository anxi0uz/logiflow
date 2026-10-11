package models

import (
	"time"

	"github.com/google/uuid"
)

type Counterparty struct {
	ID          uuid.UUID `db:"id" json:"id"`
	OwnerUserID uuid.UUID `db:"owner_user_id" json:"ownerUserId"`
	Kind        string    `db:"kind" json:"kind"`
	Name        string    `db:"name" json:"name"`
	INN         string    `db:"inn" json:"inn"`
	KPP         string    `db:"kpp" json:"kpp"`
	Address     string    `db:"address" json:"address"`
	ContactName string    `db:"contact_name" json:"contactName"`
	Phone       string    `db:"phone" json:"phone"`
	Email       string    `db:"email" json:"email"`
	Shared      bool      `db:"shared" json:"shared"`
	Status      string    `db:"status" json:"status"`
	CreatedAt   time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt   time.Time `db:"updated_at" json:"updatedAt"`
}

type TransportDetails struct {
	Sender           Counterparty `json:"sender"`
	Recipient        Counterparty `json:"recipient"`
	Carrier          Counterparty `json:"carrier"`
	BasisType        string       `json:"basisType"`
	BasisNumber      string       `json:"basisNumber"`
	BasisDate        string       `json:"basisDate"`
	PackageCount     int          `json:"packageCount"`
	Terms            string       `json:"terms"`
	RecordedByUserID uuid.UUID    `json:"recordedByUserId"`
	RecordedAt       time.Time    `json:"recordedAt"`
}

type DeliveryReceipt struct {
	ID               uuid.UUID `db:"id" json:"id"`
	OrderID          uuid.UUID `db:"order_id" json:"orderId"`
	AssignmentID     uuid.UUID `db:"assignment_id" json:"assignmentId"`
	RecipientName    string    `db:"recipient_name" json:"recipientName"`
	RecipientRole    string    `db:"recipient_role" json:"recipientRole"`
	AuthorityBasis   string    `db:"authority_basis" json:"authorityBasis"`
	PackageCount     int       `db:"package_count" json:"packageCount"`
	CargoCondition   string    `db:"cargo_condition" json:"cargoCondition"`
	SealNumber       string    `db:"seal_number" json:"sealNumber"`
	Comment          string    `db:"comment" json:"comment"`
	ReceivedAt       time.Time `db:"received_at" json:"receivedAt"`
	RecordedAt       time.Time `db:"recorded_at" json:"recordedAt"`
	RecordedByUserID uuid.UUID `db:"recorded_by_user_id" json:"recordedByUserId"`
}
