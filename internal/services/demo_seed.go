package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/events"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

func demoID(key string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("logiflow/demo/v1/"+key))
}

func demoPtr[T any](v T) *T { return &v }

// SeedDemo creates fixtures, not business commands. Everything, including document
// snapshots, commits together. Existing data is never deleted or overwritten.
// Repeating the seed preserves passwords and any subsequent user progress.
func (s *OrderService) SeedDemo(ctx context.Context, password string) (bool, error) {
	if len(password) < 12 || len(password) > 72 {
		return false, fmt.Errorf("demo password must contain 12..72 bytes")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('logiflow-demo-v1'))`); err != nil {
		return false, err
	}
	var exists, currentSchema bool
	if err = tx.QueryRow(ctx, `SELECT to_regclass('public.delivery_receipts') IS NOT NULL, EXISTS(SELECT 1 FROM users WHERE id=$1)`, demoID("user/admin")).Scan(&currentSchema, &exists); err != nil {
		return false, err
	}
	if !currentSchema {
		return false, fmt.Errorf("database schema is old; start the current Core image to apply migrations first")
	}
	if exists {
		return false, nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return false, err
	}
	f := &demoFixture{ctx: ctx, tx: tx, service: s, now: time.Now().UTC().Truncate(time.Second)}
	f.catalog(string(hash))
	for i, scenario := range []string{"draft", "ready", "offer", "assigned", "transit", "shortage", "handover", "cancelled", "direct"} {
		if f.err != nil {
			break
		}
		f.order(i, scenario)
	}
	if f.err != nil {
		return false, fmt.Errorf("create demo fixtures: %w", f.err)
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

type demoFixture struct {
	ctx        context.Context
	tx         pgx.Tx
	service    *OrderService
	now        time.Time
	err        error
	warehouses []models.Warehouse
	parties    []models.Counterparty
}

func demoInsert[T any](f *demoFixture, table string, item T) {
	if f.err == nil {
		f.err = storage.Create(f.ctx, table, item, f.tx)
	}
}

func (f *demoFixture) catalog(hash string) {
	people := []struct{ key, name, role string }{
		{"admin", "Демо Администратор", "admin"},
		{"manager.moscow", "Анна Смирнова", "manager"},
		{"manager.spb", "Игорь Козлов", "manager"},
		{"manager.kazan", "Елена Сафина", "manager"},
		{"client1", "Алексей Орлов", "client"}, {"client2", "Мария Белова", "client"},
		{"driver1", "Михаил Соколов", "driver"}, {"driver2", "Дмитрий Новиков", "driver"},
		{"driver3", "Сергей Волков", "driver"}, {"driver4", "Андрей Морозов", "driver"},
		{"driver5", "Павел Егоров", "driver"}, {"driver6", "Олег Павлов", "driver"},
	}
	for _, p := range people {
		demoInsert(f, "users", models.User{ID: demoID("user/" + p.key), Email: p.key + "@demo.logiflow.test", Slug: "demo-" + p.key, PasswordHash: hash, FullName: p.name, Role: p.role, CreatedAt: f.now.AddDate(0, -1, 0), UpdatedAt: f.now})
	}
	for i, city := range []struct {
		key, name, address string
		lat, lon           float64
	}{
		{"moscow", "Москва", "Москва, Ленинградский проспект, 80", 55.802, 37.510},
		{"spb", "Санкт-Петербург", "Санкт-Петербург, Московский проспект, 212", 59.849, 30.318},
		{"kazan", "Казань", "Казань, улица Техническая, 10", 55.749, 49.130},
	} {
		wh := models.Warehouse{ID: demoID("warehouse/" + city.key), Slug: "demo-" + city.key, Name: "ДЕМО · Склад " + city.name, Address: city.address, City: city.name, Latitude: demoPtr(city.lat), Longitude: demoPtr(city.lon), Status: "active", CreatedAt: f.now.AddDate(0, -1, 0)}
		f.warehouses = append(f.warehouses, wh)
		demoInsert(f, "warehouses", wh)
		demoInsert(f, "managers", models.Manager{ID: demoID("manager/" + city.key), UserID: demoID("user/manager." + city.key), WarehouseID: &f.warehouses[i].ID, Slug: "demo-manager-" + city.key})
	}
	for i, car := range []struct {
		plate, brand, model string
		kg, m3              float64
	}{
		{"А101ДМ77", "ГАЗ", "Газель Next", 1500, 12}, {"А102ДМ77", "Ford", "Transit", 2000, 15},
		{"А103ДМ77", "MAN", "TGX", 18000, 90}, {"А104ДМ77", "КАМАЗ", "5490", 20000, 92},
		{"А105ДМ77", "Volvo", "FH", 18000, 85}, {"А106ДМ77", "Scania", "R450", 20000, 90},
	} {
		key := fmt.Sprint(i + 1)
		vehicle := models.Vehicle{ID: demoID("vehicle/" + key), PlateNumber: car.plate, Brand: &car.brand, Model: &car.model, Year: demoPtr(2023), CapacityKg: car.kg, CapacityM3: car.m3, Status: "available", Slug: "demo-vehicle-" + key}
		driver := models.Driver{ID: demoID("driver/" + key), UserID: demoID("user/driver" + key), VehicleID: &vehicle.ID, LicenseNumber: "ДЕМО-ВУ-00" + key, LicenseExpiry: f.now.AddDate(2, 0, 0), Rating: 4.5 + float64(i)/10, Status: "available", Slug: "demo-driver-" + key}
		demoInsert(f, "vehicles", vehicle)
		demoInsert(f, "drivers", driver)
		demoInsert(f, "driver_documents", models.DriverDocument{ID: demoID("license/" + key), DriverID: driver.ID, Type: "license", Number: driver.LicenseNumber, ValidUntil: driver.LicenseExpiry, Status: "valid", CreatedAt: f.now})
		demoInsert(f, "vehicle_documents", models.VehicleDocument{ID: demoID("registration/" + key), VehicleID: vehicle.ID, Type: "registration", Number: "ДЕМО-СТС-00" + key, ValidUntil: f.now.AddDate(2, 0, 0), Status: "valid", CreatedAt: f.now})
	}
	for i, name := range []string{"ДЕМО · ООО Техноснаб", "ДЕМО · ООО Казань-Сервис", "ДЕМО · ООО Логифлоу", "ДЕМО · ООО Мебельный дом", "ДЕМО · ООО Северный склад", "ДЕМО · ИП Иванов", "ДЕМО · Петров Пётр"} {
		owner := "client1"
		if i == 2 {
			owner = "admin"
		}
		if i >= 3 {
			owner = "client2"
		}
		req := api.CounterpartyWrite{Kind: "organization", Name: name, Inn: demoPtr("7707083893"), Kpp: demoPtr("770701001"), Address: f.warehouses[i%3].Address, ContactName: "Тестовый представитель", Phone: demoPtr("+7 900 000-00-00"), Email: demoPtr(fmt.Sprintf("party%d@demo.logiflow.test", i+1)), Shared: demoPtr(i == 2)}
		if i == 5 {
			req.Kind, req.Inn, req.Kpp = "entrepreneur", demoPtr("500100732259"), nil
		}
		if i == 6 {
			req.Kind, req.Inn, req.Kpp = "individual", nil, nil
		}
		party, err := normalizeCounterparty(req)
		if err != nil {
			f.err = err
			return
		}
		party.ID, party.OwnerUserID, party.CreatedAt, party.UpdatedAt = demoID(fmt.Sprintf("party/%d", i+1)), demoID("user/"+owner), f.now, f.now
		f.parties = append(f.parties, party)
		demoInsert(f, "counterparties", party)
	}
}

func (f *demoFixture) order(index int, scenario string) {
	key := fmt.Sprint(index + 1)
	owner, sender, recipient := "client1", 0, 1
	if scenario == "cancelled" || scenario == "direct" {
		owner, sender, recipient = "client2", 3, 4
	}
	dest := 2
	if scenario == "ready" || scenario == "cancelled" {
		dest = 1
	}
	start := f.now.Add(2 * time.Hour)
	if scenario == "transit" || scenario == "shortage" {
		start = f.now.Add(-2 * time.Hour)
	}
	if scenario == "handover" {
		start = f.now.Add(-72 * time.Hour)
	}
	if scenario == "direct" {
		start = f.now.Add(-120 * time.Hour)
	}
	end := start.Add(24 * time.Hour)
	created := start.Add(-6 * time.Hour)
	if created.After(f.now) {
		created = f.now.Add(-time.Hour)
	}
	labels := []string{"Черновик заявки", "Ожидает назначения", "Предложение водителю", "Ожидает погрузки", "Груз в пути", "Прибыл: недостача 1 места", "Доставлен со сменой водителя", "Отмена: груз ожидает возврата", "Доставлен на обычный адрес"}
	order := models.Order{ID: demoID("order/" + key), CreatedByID: demoPtr(demoID("user/" + owner)), OriginWarehouseID: &f.warehouses[0].ID, DestinationWarehouseID: &f.warehouses[dest].ID, OriginAddress: f.warehouses[0].Address, DestinationAddress: f.warehouses[dest].Address, CargoDescription: "ДЕМО · " + labels[index] + " · Запчасти в коробках", WeightKg: 800, VolumeM3: 5, Status: models.OrderDraft, PickupFrom: &start, PickupTo: &end, CreatedAt: created, TransportPolicyVersion: 1}
	if scenario == "direct" {
		order.DestinationWarehouseID = nil
		order.DestinationAddress = "Казань, улица Баумана, 45"
	}
	details := models.TransportDetails{Sender: f.parties[sender], Recipient: f.parties[recipient], Carrier: f.parties[2], BasisType: "transport_request", BasisNumber: "ДЕМО-2026-" + key, BasisDate: created.Format(time.DateOnly), PackageCount: 10, Terms: "Учебные данные: вымышленные организации; реквизиты и маршрут предназначены для демонстрации.", RecordedByUserID: *order.CreatedByID, RecordedAt: created}
	var err error
	order.TransportDetails, err = json.Marshal(details)
	if err != nil {
		f.err = err
		return
	}
	// Fixed illustrative geometry keeps demo setup independent of routing providers.
	coords := [][]float64{{37.510, 55.802}, {40.407, 56.129}, {44.006, 56.326}, {47.252, 56.143}, {49.130, 55.749}}
	distance, seconds := 820.0, 36000
	if dest == 1 {
		coords = [][]float64{{37.510, 55.802}, {35.912, 56.858}, {31.277, 58.522}, {30.318, 59.849}}
		distance, seconds = 705, 32400
	}
	order.TotalPrice = demoPtr(f.service.orderPrice(distance, order.WeightKg, order.VolumeM3))
	demoInsert(f, "orders", order)
	geometry, err := json.Marshal(coords)
	if err != nil {
		f.err = err
		return
	}
	route := models.Route{ID: demoID("route/" + key), OrderID: order.ID, Coordinates: geometry, DistanceKm: &distance, DurationSec: &seconds, Status: "pending"}
	demoInsert(f, "routes", route)
	f.history(&order, "", order.Status, created, *order.CreatedByID)
	if scenario == "draft" {
		return
	}
	order.SubmittedAt = demoPtr(created.Add(10 * time.Minute))
	f.transition(&order, models.OrderReadyForDispatch, *order.SubmittedAt, *order.CreatedByID)
	f.document(&order, nil, "transport_order", order.ID, nil)
	if scenario == "ready" {
		f.operation(&order, nil, "receive", 10, created.Add(20*time.Minute))
		if f.err == nil {
			f.err = enqueueDispatchRequest(f.ctx, f.tx, &order)
		}
		return
	}
	if scenario == "cancelled" {
		f.operation(&order, nil, "receive", 10, created.Add(20*time.Minute))
		order.CancelledAt = demoPtr(created.Add(time.Hour))
		f.transition(&order, models.OrderCancelled, *order.CancelledAt, *order.CreatedByID)
		f.document(&order, nil, "cancellation_notice", order.ID, []documentField{{"Причина отмены", "Заказчик отменил заявку; принятый груз ожидает выдачи отправителю"}})
		return
	}
	driver := index - 1
	if scenario == "handover" || scenario == "direct" {
		driver = 5
	}
	assigned := created.Add(30 * time.Minute)
	if scenario == "offer" {
		assigned = f.now
	}
	assignment := models.Assignment{ID: demoID("assignment/" + key), OrderID: order.ID, DriverID: demoID(fmt.Sprintf("driver/%d", driver)), VehicleID: demoID(fmt.Sprintf("vehicle/%d", driver)), Status: models.AssignmentPendingAcceptance, PlannedFrom: start, PlannedTo: end, CreatedByUserID: demoID("user/manager.moscow"), Source: "manual", AssignedAt: assigned, OfferExpiresAt: assigned.Add(15 * time.Minute)}
	demoInsert(f, "assignments", assignment)
	f.operation(&order, nil, "receive", 10, created.Add(20*time.Minute))
	if scenario == "offer" {
		return
	}
	assignment.Status, assignment.AcceptedAt = models.AssignmentAccepted, demoPtr(assigned.Add(5*time.Minute))
	order.AssignedAt, order.DriverID = &assignment.AssignedAt, &assignment.DriverID
	f.saveAssignment(&assignment)
	f.transition(&order, models.OrderAssigned, *assignment.AcceptedAt, demoID(fmt.Sprintf("user/driver%d", driver)))
	if scenario == "assigned" {
		return
	}
	f.operation(&order, &assignment, "load", 10, start.Add(-10*time.Minute))
	assignment.Status, assignment.StartedAt = models.AssignmentActive, &start
	order.StartedAt = &start
	f.saveAssignment(&assignment)
	f.transition(&order, models.OrderInTransit, start, demoID(fmt.Sprintf("user/driver%d", driver)))
	route.DriverID, route.StartedAt, route.Status, route.CurrentIndex = &assignment.DriverID, &start, "active", 1
	f.saveRoute(&route)
	f.document(&order, &assignment, "trip_assignment", assignment.ID, nil)
	if scenario == "transit" {
		return
	}
	var legs []documentField
	if scenario == "handover" {
		at := start.Add(8 * time.Hour)
		previous := assignment
		previous.Status, previous.ReleasedAt, previous.ReleaseReasonCode = models.AssignmentReleased, &at, demoPtr("trip_handover")
		f.saveAssignment(&previous)
		assignment.ID, assignment.DriverID, assignment.VehicleID = demoID("assignment/"+key+"/next"), demoID("driver/6"), demoID("vehicle/6")
		assignment.AssignedAt, assignment.OfferExpiresAt, assignment.AcceptedAt, assignment.StartedAt = at, at.Add(15*time.Minute), &at, &at
		assignment.PlannedFrom = at
		demoInsert(f, "assignments", assignment)
		handover := models.TripHandover{ID: demoID("handover/" + key), OrderID: order.ID, PreviousAssignmentID: previous.ID, NextAssignmentID: &assignment.ID, DriverID: assignment.DriverID, VehicleID: assignment.VehicleID, CreatedByUserID: assignment.CreatedByUserID, Reason: "Плановая замена водителя и машины", Location: "Нижний Новгород, площадка перегрузки", CargoCondition: "10 мест, упаковка без повреждений", PlannedTo: end, Status: "completed", CreatedAt: at.Add(-5 * time.Minute), ExpiresAt: at.Add(10 * time.Minute), OutgoingConfirmedAt: demoPtr(at.Add(-time.Minute)), CompletedAt: &at}
		demoInsert(f, "trip_handovers", handover)
		order.DriverID, route.DriverID = &assignment.DriverID, &assignment.DriverID
		f.saveOrder(&order)
		f.saveRoute(&route)
		legs = []documentField{{"Участок 1", fmt.Sprintf("%s; водитель %s; машина %s; %s — %s", previous.ID, previous.DriverID, previous.VehicleID, start.Format(time.RFC3339), at.Format(time.RFC3339))}, {"Участок 2", fmt.Sprintf("%s; водитель %s; машина %s; начало %s", assignment.ID, assignment.DriverID, assignment.VehicleID, at.Format(time.RFC3339))}}
		f.document(&order, &assignment, "cargo_handover", handover.ID, append([]documentField{{"Причина передачи", handover.Reason}, {"Место передачи", handover.Location}, {"Передающий подтвердил", handover.OutgoingConfirmedAt.Format(time.RFC3339)}, {"Принимающий подтвердил", at.Format(time.RFC3339)}}, legs...), demoID("user/driver5"))
		f.document(&order, &assignment, "trip_assignment", assignment.ID, nil)
		driver = 6
	}
	arrived := f.now.Add(-30 * time.Minute)
	if scenario == "handover" || scenario == "direct" {
		arrived = start.Add(12 * time.Hour)
	}
	order.ArrivedAt = &arrived
	f.transition(&order, models.OrderArrived, arrived, demoID(fmt.Sprintf("user/driver%d", driver)))
	route.Status, route.CurrentIndex, route.FinishedAt = "finished", len(coords)-1, &arrived
	f.saveRoute(&route)
	count := 10
	if scenario == "shortage" {
		count = 9
	}
	var unload *models.WarehouseOperation
	if order.DestinationWarehouseID != nil {
		op := f.operation(&order, &assignment, "unload", count, arrived.Add(5*time.Minute))
		unload = &op
	}
	actor := demoID("user/manager.kazan")
	if scenario == "direct" {
		actor = demoID("user/driver5")
	}
	receipt := models.DeliveryReceipt{ID: demoID("receipt/" + key), OrderID: order.ID, AssignmentID: assignment.ID, RecipientName: "Тестовый получатель", RecipientRole: "Кладовщик", AuthorityBasis: "Учебная доверенность ДЕМО-" + key + ", сведения со слов подтверждающего", PackageCount: count, CargoCondition: "Упаковка без повреждений", SealNumber: "DEMO-SEAL-" + key, ReceivedAt: arrived.Add(10 * time.Minute), RecordedAt: arrived.Add(10 * time.Minute), RecordedByUserID: actor}
	if scenario == "shortage" {
		receipt.Comment = "Одно место отсутствует; требуется решение менеджера"
	}
	demoInsert(f, "delivery_receipts", receipt)
	if scenario == "shortage" {
		demoInsert(f, "order_incidents", models.OrderIncident{ID: demoID("incident/" + key), OrderID: order.ID, AssignmentID: &assignment.ID, WarehouseID: order.DestinationWarehouseID, SourceOperationID: &unload.ID, Kind: "quantity_discrepancy", Description: "ДЕМО · при разгрузке принято 9 мест из 10; завершение заблокировано до решения", ExpectedPackages: demoPtr(10), ActualPackages: &count, Status: "open", ReportedByUserID: actor, ReportedAt: arrived.Add(5 * time.Minute)})
		return
	}
	completed := arrived.Add(15 * time.Minute)
	assignment.Status, assignment.CompletedAt, assignment.RecipientName, assignment.DeliveryComment = models.AssignmentCompleted, &completed, &receipt.RecipientName, &receipt.Comment
	order.DeliveredAt = &completed
	f.saveAssignment(&assignment)
	f.transition(&order, models.OrderCompleted, completed, actor)
	if len(legs) > 1 {
		legs[1].Value += "; окончание " + completed.Format(time.RFC3339)
	}
	f.document(&order, &assignment, "service_report", order.ID, legs)
	if f.err == nil {
		fields, err := transportDocumentFields(&order, &receipt)
		if err != nil {
			f.err = err
			return
		}
		event := events.DeliveryCompleted{EventID: demoID("delivery-event/" + key), OrderID: order.ID, UserID: *order.CreatedByID, OriginAddress: order.OriginAddress, DestinationAddress: order.DestinationAddress, CargoDescription: order.CargoDescription, WeightKg: order.WeightKg, VolumeM3: order.VolumeM3, TotalPrice: order.TotalPrice, DeliveredAt: completed, RecipientName: &receipt.RecipientName, DriverID: assignment.DriverID, VehicleID: assignment.VehicleID, Fields: fields, Recipients: []uuid.UUID{demoID("user/admin")}}
		payload, err := json.Marshal(event)
		if err != nil {
			f.err = err
			return
		}
		demoInsert(f, "integration_outbox", models.IntegrationEvent{ID: event.EventID, Subject: "delivery.completed.v1", Payload: payload, CreatedAt: f.now})
	}
}

func (f *demoFixture) saveOrder(o *models.Order) {
	if f.err == nil {
		f.err = storage.Update(f.ctx, "orders", *o, f.tx, func(b *sqlbuilder.UpdateBuilder) { b.Where(b.EQ("id", o.ID)) })
	}
}
func (f *demoFixture) saveAssignment(a *models.Assignment) {
	if f.err == nil {
		f.err = storage.Update(f.ctx, "assignments", *a, f.tx, func(b *sqlbuilder.UpdateBuilder) { b.Where(b.EQ("id", a.ID)) })
	}
}
func (f *demoFixture) saveRoute(r *models.Route) {
	if f.err == nil {
		f.err = storage.Update(f.ctx, "routes", *r, f.tx, func(b *sqlbuilder.UpdateBuilder) { b.Where(b.EQ("id", r.ID)) })
	}
}
func (f *demoFixture) transition(o *models.Order, status string, at time.Time, actor uuid.UUID) {
	from := o.Status
	o.Status = status
	f.saveOrder(o)
	f.history(o, from, status, at, actor)
	body := "ДЕМО · " + o.CargoDescription + ": " + status
	demoInsert(f, "notifications", models.Notification{ID: demoID("notification/" + o.ID.String() + "/" + status), UserID: *o.CreatedByID, Title: "Изменение статуса заказа", Body: &body, CreatedAt: at})
}
func (f *demoFixture) history(o *models.Order, from, to string, at time.Time, actor uuid.UUID) {
	var previous *string
	if from != "" {
		previous = &from
	}
	demoInsert(f, "order_status_history", models.OrderStatusHistory{ID: demoID("history/" + o.ID.String() + "/" + to), OrderID: o.ID, FromStatus: previous, ToStatus: to, ActorUserID: &actor, ReasonCode: demoPtr("demo_fixture"), CreatedAt: at})
}
func (f *demoFixture) operation(o *models.Order, a *models.Assignment, kind string, count int, at time.Time) models.WarehouseOperation {
	op := models.WarehouseOperation{ID: demoID("operation/" + o.ID.String() + "/" + kind), OrderID: o.ID, WarehouseID: *o.OriginWarehouseID, OperationType: kind, PackageCount: count, CargoCondition: "Упаковка без повреждений", Comment: "Учебная операция", CreatedByUserID: demoID("user/manager.moscow"), OccurredAt: at}
	if a != nil {
		op.AssignmentID = &a.ID
	}
	if kind == "unload" {
		op.WarehouseID = *o.DestinationWarehouseID
		op.CreatedByUserID = demoID("user/manager.kazan")
	}
	demoInsert(f, "warehouse_operations", op)
	return op
}
func (f *demoFixture) document(o *models.Order, a *models.Assignment, kind string, reference uuid.UUID, fields []documentField, extra ...uuid.UUID) {
	if f.err == nil {
		extra = append(extra, demoID("user/admin"))
		f.err = f.service.enqueueOperationalDocument(f.ctx, f.tx, o, a, kind, reference, demoID("user/manager.moscow"), fields, extra...)
	}
}
