package services

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
)

func (s *OrderService) SetTransportDetails(ctx context.Context, orderID, userID uuid.UUID, role string, req api.TransportDetailsWrite) (*models.Order, error) {
	if !canManageCounterparties(role) {
		return nil, ErrForbidden
	}
	number, terms := strings.TrimSpace(req.BasisNumber), stringValue(req.Terms)
	if req.SenderId == uuid.Nil || req.RecipientId == uuid.Nil || req.CarrierId == uuid.Nil || (req.BasisType != "contract" && req.BasisType != "transport_request") || !boundedText(number, 1, 100) || !boundedText(terms, 0, 2000) || req.PackageCount < 1 || req.BasisDate.IsZero() {
		return nil, ErrInvalidOrderInput
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	order, err := storage.GetOne[models.Order](ctx, tx, "orders", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", orderID)).ForUpdate() })
	if err != nil {
		return nil, err
	}
	if role == "client" && (order.CreatedByID == nil || *order.CreatedByID != userID) {
		return nil, ErrForbidden
	}
	if role == "manager" {
		warehouseID, err := s.managerWarehouseID(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		if order.OriginWarehouseID == nil || *order.OriginWarehouseID != warehouseID {
			return nil, ErrForbidden
		}
	}
	if order.Status != models.OrderDraft {
		return nil, ErrInvalidOrderTransition
	}
	ids := []uuid.UUID{req.SenderId, req.RecipientId, req.CarrierId}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	parties := map[uuid.UUID]models.Counterparty{}
	for _, id := range ids {
		if _, ok := parties[id]; ok {
			continue
		}
		party, err := storage.GetOne[models.Counterparty](ctx, tx, "counterparties", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", id)); b.SQL("FOR SHARE") })
		if err != nil {
			return nil, err
		}
		allowed := role == "admin" || party.Shared || party.OwnerUserID == userID
		// A responsible manager can use the customer's records for this order only.
		if role == "manager" && order.CreatedByID != nil && party.OwnerUserID == *order.CreatedByID {
			allowed = true
		}
		if !allowed {
			return nil, ErrForbidden
		}
		if party.Status != "active" {
			return nil, ErrCounterpartyArchived
		}
		parties[id] = *party
	}
	details := models.TransportDetails{Sender: parties[req.SenderId], Recipient: parties[req.RecipientId], Carrier: parties[req.CarrierId], BasisType: string(req.BasisType), BasisNumber: number, BasisDate: req.BasisDate.Format(time.DateOnly), PackageCount: req.PackageCount, Terms: terms, RecordedByUserID: userID, RecordedAt: time.Now().UTC()}
	if details.Carrier.Kind == "individual" {
		return nil, ErrInvalidOrderInput
	}
	order.TransportDetails, err = json.Marshal(details)
	if err != nil {
		return nil, err
	}
	order.TransportPolicyVersion = 1
	if err = storage.Update(ctx, "orders", *order, tx, func(b *sqlbuilder.UpdateBuilder) { b.Where(b.EQ("id", orderID)) }); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return order, nil
}

func readTransportDetails(order *models.Order) (*models.TransportDetails, error) {
	if len(order.TransportDetails) == 0 {
		return nil, nil
	}
	var details models.TransportDetails
	if err := json.Unmarshal(order.TransportDetails, &details); err != nil {
		return nil, err
	}
	return &details, nil
}
