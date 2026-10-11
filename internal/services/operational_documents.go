package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/anxi0uz/logiflow/internal/events"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
)

type documentField events.DocumentField

// Snapshot values are serialized before commit; the document service never
// rereads mutable Core profiles to reconstruct historical facts.
func (s *OrderService) enqueueOperationalDocument(ctx context.Context, tx pgx.Tx, order *models.Order, assignment *models.Assignment, kind string, referenceID, actorID uuid.UUID, extra []documentField, extraRecipients ...uuid.UUID) error {
	if order.CreatedByID == nil {
		return nil
	}
	recipients := append([]uuid.UUID{*order.CreatedByID, actorID}, extraRecipients...)
	receipt, err := storage.GetOne[models.DeliveryReceipt](ctx, tx, "delivery_receipts", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("order_id", order.ID)) })
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	transportFields, err := transportDocumentFields(order, receipt)
	if err != nil {
		return err
	}
	fields := []documentField{
		{"Заказ", order.ID.String()}, {"Статус заказа", order.Status},
		{"Адрес отправления", order.OriginAddress}, {"Адрес назначения", order.DestinationAddress},
		{"Груз", order.CargoDescription}, {"Масса, кг", strconv.FormatFloat(order.WeightKg, 'f', 2, 64)},
		{"Объем, м3", strconv.FormatFloat(order.VolumeM3, 'f', 2, 64)},
	}
	for _, field := range transportFields {
		fields = append(fields, documentField(field))
	}
	if order.TotalPrice != nil {
		fields = append(fields, documentField{"Расчетная стоимость, RUB", strconv.FormatFloat(*order.TotalPrice, 'f', 2, 64)})
	}
	if order.PickupFrom != nil {
		fields = append(fields, documentField{"Плановое начало", order.PickupFrom.UTC().Format(time.RFC3339)})
	}
	if order.PickupTo != nil {
		fields = append(fields, documentField{"Плановое окончание", order.PickupTo.UTC().Format(time.RFC3339)})
	}
	if order.StartedAt != nil {
		fields = append(fields, documentField{"Начало перевозки", order.StartedAt.UTC().Format(time.RFC3339)})
	}
	if order.DeliveredAt != nil {
		fields = append(fields, documentField{"Доставка подтверждена", order.DeliveredAt.UTC().Format(time.RFC3339)})
	}
	for _, person := range []struct {
		label string
		id    uuid.UUID
	}{{"Заказчик", *order.CreatedByID}, {"Пользователь, зафиксировавший событие", actorID}} {
		user, err := storage.GetOne[models.User](ctx, tx, "users", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", person.id)) })
		if err != nil {
			return err
		}
		fields = append(fields, documentField{person.label, user.FullName + " / " + person.id.String()})
	}
	if assignment != nil {
		driver, err := storage.GetOne[models.Driver](ctx, tx, "drivers", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", assignment.DriverID)) })
		if err != nil {
			return err
		}
		user, err := storage.GetOne[models.User](ctx, tx, "users", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", driver.UserID)) })
		if err != nil {
			return err
		}
		vehicle, err := storage.GetOne[models.Vehicle](ctx, tx, "vehicles", func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("id", assignment.VehicleID)) })
		if err != nil {
			return err
		}
		recipients = append(recipients, driver.UserID, assignment.CreatedByUserID)
		fields = append(fields, documentField{"Назначение", assignment.ID.String()}, documentField{"Водитель", user.FullName + " / " + driver.ID.String()}, documentField{"Удостоверение водителя", driver.LicenseNumber}, documentField{"Транспортное средство", vehicle.PlateNumber + " / " + vehicle.ID.String()})
		if assignment.RecipientName != nil {
			fields = append(fields, documentField{"Получатель (со слов подтверждающего)", *assignment.RecipientName})
		}
		if assignment.DeliveryComment != nil {
			fields = append(fields, documentField{"Комментарий при передаче", *assignment.DeliveryComment})
		}
	}

	if order.OriginWarehouseID != nil {
		fields = append(fields, documentField{"Склад отправления", order.OriginWarehouseID.String()})
	}
	if order.DestinationWarehouseID != nil {
		fields = append(fields, documentField{"Склад назначения", order.DestinationWarehouseID.String()})
	}
	operations, err := storage.GetAll[models.WarehouseOperation](ctx, "warehouse_operations", tx, func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("order_id", order.ID)).OrderBy("occurred_at", "id") })
	if err != nil {
		return err
	}
	for _, op := range operations {
		fields = append(fields, documentField{"Складская операция / " + op.OperationType, fmt.Sprintf("Склад %s; мест %d; состояние %s; примечание %s; подтвердил %s; время %s", op.WarehouseID, op.PackageCount, op.CargoCondition, op.Comment, op.CreatedByUserID, op.OccurredAt.UTC().Format(time.RFC3339)+"; получил: "+op.RecipientName)})
	}

	incidents, err := storage.GetAll[models.OrderIncident](ctx, "order_incidents", tx, func(b *sqlbuilder.SelectBuilder) { b.Where(b.EQ("order_id", order.ID)).OrderBy("reported_at", "id") })
	if err != nil {
		return err
	}
	for _, incident := range incidents {
		text := fmt.Sprintf("%s; %s; статус %s; сообщил %s, %s; решение %s: %s; получатель %s", incident.ID, incident.Description, incident.Status, incident.ReportedByUserID, incident.ReportedAt.UTC().Format(time.RFC3339), incident.Resolution, incident.ResolutionNote, incident.RecipientName)
		if incident.ExpectedPackages != nil && incident.ActualPackages != nil {
			text += fmt.Sprintf("; ожидалось %d, фактически %d", *incident.ExpectedPackages, *incident.ActualPackages)
		}
		if incident.ResolvedAt != nil && incident.ResolvedByUserID != nil {
			text += "; решил " + incident.ResolvedByUserID.String() + ", " + incident.ResolvedAt.UTC().Format(time.RFC3339)
		}
		fields = append(fields, documentField{"Проблема / " + incident.Kind, text})
	}
	fields = append(fields, extra...)
	eventFields := make([]events.DocumentField, len(fields))
	for i, field := range fields {
		eventFields[i] = events.DocumentField(field)
	}
	event := events.DocumentRequested{EventID: uuid.New(), OrderID: order.ID, UserID: *order.CreatedByID, ReferenceID: referenceID, Type: kind, OccurredAt: time.Now().UTC(), Fields: eventFields, Recipients: recipients}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return storage.Create(ctx, "integration_outbox", models.IntegrationEvent{ID: event.EventID, Subject: "document.requested.v1", Payload: payload, CreatedAt: event.OccurredAt}, tx)
}
