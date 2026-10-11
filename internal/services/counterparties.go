package services

import (
	"context"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
)

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func boundedText(value string, min, max int) bool {
	n := utf8.RuneCountInString(value)
	return n >= min && n <= max
}

func asciiDigits(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// A checksum validates syntax only; no registration or tax status is inferred.
func validINN(value string) bool {
	if !asciiDigits(value, 10) && !asciiDigits(value, 12) {
		return false
	}
	check := func(weights []int, index int) bool {
		sum := 0
		for i, weight := range weights {
			sum += int(value[i]-'0') * weight
		}
		return sum%11%10 == int(value[index]-'0')
	}
	if strings.Trim(value, "0") == "" {
		return false
	}
	if len(value) == 10 {
		return check([]int{2, 4, 10, 3, 5, 9, 4, 6, 8}, 9)
	}
	return check([]int{7, 2, 4, 10, 3, 5, 9, 4, 6, 8}, 10) && check([]int{3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8}, 11)
}

func normalizeCounterparty(req api.CounterpartyWrite) (models.Counterparty, error) {
	item := models.Counterparty{Kind: string(req.Kind), Name: strings.TrimSpace(req.Name), INN: stringValue(req.Inn), KPP: stringValue(req.Kpp), Address: strings.TrimSpace(req.Address), ContactName: strings.TrimSpace(req.ContactName), Phone: stringValue(req.Phone), Email: stringValue(req.Email), Status: "active"}
	if req.Shared != nil {
		item.Shared = *req.Shared
	}
	if req.Status != nil {
		item.Status = string(*req.Status)
	}
	if !boundedText(item.Name, 1, 300) || !boundedText(item.Address, 1, 1000) || !boundedText(item.ContactName, 1, 200) || !boundedText(item.Phone, 0, 40) || !boundedText(item.Email, 0, 254) || (item.Phone == "" && item.Email == "") || (item.Status != "active" && item.Status != "archived") {
		return item, ErrInvalidOrderInput
	}
	if item.Email != "" {
		address, err := mail.ParseAddress(item.Email)
		if err != nil || address.Address != item.Email {
			return item, ErrInvalidOrderInput
		}
	}
	switch item.Kind {
	case "organization":
		if len(item.INN) != 10 || !validINN(item.INN) || (item.KPP != "" && !asciiDigits(item.KPP, 9)) {
			return item, ErrInvalidOrderInput
		}
	case "entrepreneur":
		if len(item.INN) != 12 || !validINN(item.INN) || item.KPP != "" {
			return item, ErrInvalidOrderInput
		}
	case "individual":
		if (item.INN != "" && (len(item.INN) != 12 || !validINN(item.INN))) || item.KPP != "" {
			return item, ErrInvalidOrderInput
		}
	default:
		return item, ErrInvalidOrderInput
	}
	return item, nil
}

func canManageCounterparties(role string) bool {
	return role == "client" || role == "manager" || role == "admin"
}

func (s *OrderService) CreateCounterparty(ctx context.Context, userID uuid.UUID, role string, req api.CounterpartyWrite) (*models.Counterparty, error) {
	if !canManageCounterparties(role) {
		return nil, ErrForbidden
	}
	item, err := normalizeCounterparty(req)
	if err != nil {
		return nil, err
	}
	if item.Shared && role != "admin" {
		return nil, ErrForbidden
	}
	item.ID, item.OwnerUserID = uuid.New(), userID
	item.CreatedAt = time.Now().UTC()
	item.UpdatedAt = item.CreatedAt
	if err = storage.Create(ctx, "counterparties", item, s.db); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *OrderService) GetCounterparty(ctx context.Context, id, userID uuid.UUID, role string) (*models.Counterparty, error) {
	if !canManageCounterparties(role) {
		return nil, ErrForbidden
	}
	return storage.GetOne[models.Counterparty](ctx, s.db, "counterparties", func(b *sqlbuilder.SelectBuilder) {
		b.Where(b.EQ("id", id))
		if role != "admin" {
			b.Where(b.Or(b.EQ("owner_user_id", userID), b.EQ("shared", true)))
		}
	})
}

func (s *OrderService) ListCounterparties(ctx context.Context, userID uuid.UUID, role string, params api.ListCounterpartiesParams) ([]models.Counterparty, error) {
	if !canManageCounterparties(role) {
		return nil, ErrForbidden
	}
	limit, offset := 50, 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 10000 {
		return nil, ErrInvalidOrderInput
	}
	return storage.GetAll[models.Counterparty](ctx, "counterparties", s.db, func(b *sqlbuilder.SelectBuilder) {
		if role != "admin" {
			b.Where(b.Or(b.EQ("owner_user_id", userID), b.EQ("shared", true)))
		}
		if params.IncludeArchived == nil || !*params.IncludeArchived {
			b.Where(b.EQ("status", "active"))
		}
		b.OrderBy("created_at", "id").Limit(limit).Offset(offset)
	})
}

func (s *OrderService) UpdateCounterparty(ctx context.Context, id, userID uuid.UUID, role string, req api.CounterpartyWrite) (*models.Counterparty, error) {
	if !canManageCounterparties(role) {
		return nil, ErrForbidden
	}
	item, err := normalizeCounterparty(req)
	if err != nil {
		return nil, err
	}
	if item.Shared && role != "admin" {
		return nil, ErrForbidden
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	old, err := storage.GetOne[models.Counterparty](ctx, tx, "counterparties", func(b *sqlbuilder.SelectBuilder) {
		b.Where(b.EQ("id", id))
		if role != "admin" {
			b.Where(b.EQ("owner_user_id", userID))
		}
		b.ForUpdate()
	})
	if err != nil {
		return nil, err
	}
	item.ID, item.OwnerUserID, item.CreatedAt, item.UpdatedAt = old.ID, old.OwnerUserID, old.CreatedAt, time.Now().UTC()
	if err = storage.Update(ctx, "counterparties", item, tx, func(b *sqlbuilder.UpdateBuilder) { b.Where(b.EQ("id", id)) }); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &item, nil
}
