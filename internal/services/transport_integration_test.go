package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/anxi0uz/logiflow/pkg/routing"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func seedTransportParties(t *testing.T, f *expiryFixture) api.TransportDetailsWrite {
	t.Helper()
	ids := []uuid.UUID{}
	for _, name := range []string{"Отправитель", "Получатель", "Перевозчик"} {
		p, err := f.service.CreateCounterparty(context.Background(), f.ownerID, "client", testCounterpartyRequest(name))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	return api.TransportDetailsWrite{SenderId: ids[0], RecipientId: ids[1], CarrierId: ids[2], BasisType: "contract", BasisNumber: "ТЕСТ-1", BasisDate: openapi_types.Date{Time: time.Now().UTC()}, PackageCount: 5}
}

func TestCounterpartyCatalogIsolationAndImmutableOrderSnapshot(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 80)
	other := newExpiryFixture(t, pool, 82)
	manager := warehouseTestManager(t, pool, f.warehouseID)
	req := seedTransportParties(t, f)
	foreign, err := other.service.CreateCounterparty(ctx, other.ownerID, "client", testCounterpartyRequest("Чужая организация"))
	if err != nil {
		t.Fatal(err)
	}
	sharedReq := testCounterpartyRequest("Общий перевозчик")
	shared := true
	sharedReq.Shared = &shared
	if _, err = f.service.CreateCounterparty(ctx, f.ownerID, "client", sharedReq); !errors.Is(err, ErrForbidden) {
		t.Fatalf("client published party: %v", err)
	}
	public, err := f.service.CreateCounterparty(ctx, f.ownerID, "admin", sharedReq)
	if err != nil {
		t.Fatal(err)
	}
	list, err := f.service.ListCounterparties(ctx, other.ownerID, "client", api.ListCounterpartiesParams{})
	if err != nil || len(list) != 2 {
		t.Fatalf("tenant catalog: %+v %v", list, err)
	}
	if _, err = f.service.GetCounterparty(ctx, req.SenderId, other.ownerID, "client"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("private read: %v", err)
	}
	if _, err = f.service.UpdateCounterparty(ctx, public.ID, other.ownerID, "client", testCounterpartyRequest("Changed")); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("public edit: %v", err)
	}
	if _, err = f.service.ListCounterparties(ctx, f.driverUserID, "driver", api.ListCounterpartiesParams{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("driver catalog: %v", err)
	}
	draft := createDraftOrder(t, ctx, pool, f.ownerID, f.warehouseID, f.from, f.to)
	bad := req
	bad.SenderId = foreign.ID
	if _, err = f.service.SetTransportDetails(ctx, draft.ID, f.ownerID, "client", bad); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign party used: %v", err)
	}
	if _, err = f.service.SetTransportDetails(ctx, draft.ID, other.ownerID, "client", req); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign order edited: %v", err)
	}
	order, err := f.service.SetTransportDetails(ctx, draft.ID, manager, "manager", req)
	if err != nil {
		t.Fatal(err)
	}
	if order.TransportPolicyVersion != 1 {
		t.Fatal("legacy draft not upgraded")
	}
	if _, err = f.service.UpdateCounterparty(ctx, req.SenderId, f.ownerID, "client", testCounterpartyRequest("Новое название")); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SubmitOrder(ctx, draft.ID, f.ownerID, "client"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SetTransportDetails(ctx, draft.ID, manager, "manager", req); !errors.Is(err, ErrInvalidOrderTransition) {
		t.Fatalf("submitted snapshot rewritten: %v", err)
	}
	stored, err := f.service.GetOrder(ctx, draft.ID, f.ownerID, "client")
	if err != nil {
		t.Fatal(err)
	}
	details, err := readTransportDetails(stored)
	if err != nil || details.Sender.Name != "Отправитель" {
		t.Fatalf("snapshot changed: %+v %v", details, err)
	}
	var raw []byte
	if err = pool.QueryRow(ctx, `SELECT payload FROM integration_outbox WHERE subject='document.requested.v1' AND payload->>'reference_id'=$1`, draft.ID.String()).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Отправитель") || strings.Contains(string(raw), "Новое название") {
		t.Fatalf("document uses mutable profile: %s", raw)
	}
	archived := api.CounterpartyWriteStatus("archived")
	update := testCounterpartyRequest("Новое название")
	update.Status = &archived
	if _, err = f.service.UpdateCounterparty(ctx, req.SenderId, f.ownerID, "client", update); err != nil {
		t.Fatal(err)
	}
	next := createDraftOrder(t, ctx, pool, f.ownerID, f.warehouseID, f.from, f.to)
	if _, err = f.service.SetTransportDetails(ctx, next.ID, f.ownerID, "client", req); !errors.Is(err, ErrCounterpartyArchived) {
		t.Fatalf("archived party selected: %v", err)
	}
}

func TestNewOrderRequiresTransportDetailsBeforeSubmit(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 84)
	f.service.routePlanner = &testRoutePlanner{estimate: routing.Estimate{Coordinates: [][]float64{{30, 60}, {31, 61}}, DistanceKm: 25, DurationSec: 900}}
	address := "Адрес отправления"
	result, err := f.service.CreateOrder(ctx, api.OrderCreate{OriginAddress: &address, DestinationAddress: "Адрес получателя", PickupFrom: &f.from, PickupTo: &f.to}, f.ownerID, "client")
	if err != nil {
		t.Fatal(err)
	}
	if result.Order.TransportPolicyVersion != 1 {
		t.Fatal("new API order uses legacy rules")
	}
	if _, err = f.service.SubmitOrder(ctx, result.Order.ID, f.ownerID, "client"); !errors.Is(err, ErrTransportDetailsRequired) {
		t.Fatalf("submitted anonymous transport: %v", err)
	}
	if _, err = f.service.SetTransportDetails(ctx, result.Order.ID, f.ownerID, "client", seedTransportParties(t, f)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SubmitOrder(ctx, result.Order.ID, f.ownerID, "client"); err != nil {
		t.Fatal(err)
	}
}

func TestDirectReceiptShortageBlocksCompletionAndPreservesProof(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 86)
	req := seedTransportParties(t, f)
	details := models.TransportDetails{PackageCount: 5, Sender: models.Counterparty{Name: "Отправитель"}, Recipient: models.Counterparty{Name: "Получатель"}, Carrier: models.Counterparty{Name: "Перевозчик"}, BasisNumber: req.BasisNumber, BasisType: "contract", BasisDate: req.BasisDate.Format(time.DateOnly)}
	raw, _ := json.Marshal(details)
	if _, err := pool.Exec(ctx, `UPDATE orders SET origin_warehouse_id=NULL,transport_policy_version=1,transport_details=$1 WHERE id=$2`, raw, f.order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	receiptReq := api.DeliveryReceiptCreate{AssignmentId: f.offer.ID, RecipientName: "Петров Иван", RecipientRole: "Представитель получателя", AuthorityBasis: "Доверенность № 1, со слов представителя", PackageCount: 4, CargoCondition: "Одно место не доставлено"}
	if _, err := f.service.CreateDeliveryReceipt(ctx, f.order.ID, f.driverUserID, "driver", receiptReq); !errors.Is(err, ErrInvalidOrderTransition) {
		t.Fatalf("receipt before arrival: %v", err)
	}
	if _, err := f.service.ArriveAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CompleteAssignment(ctx, f.offer.ID, f.driverUserID, "driver", api.DeliveryComplete{}); !errors.Is(err, ErrDeliveryReceiptRequired) {
		t.Fatalf("completed without proof: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	receiptReq.ReceivedAt = &old
	if _, err := f.service.CreateDeliveryReceipt(ctx, f.order.ID, f.driverUserID, "driver", receiptReq); !errors.Is(err, ErrInvalidOrderInput) {
		t.Fatalf("backdated receipt: %v", err)
	}
	receiptReq.ReceivedAt = nil
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.service.CreateDeliveryReceipt(ctx, f.order.ID, f.driverUserID, "driver", receiptReq)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrDeliveryReceiptConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("duplicate receipt race: %d %d", success, conflict)
	}
	if _, err := f.service.CompleteAssignment(ctx, f.offer.ID, f.driverUserID, "driver", api.DeliveryComplete{}); !errors.Is(err, ErrIncidentOpen) {
		t.Fatalf("shortage ignored: %v", err)
	}
	incidents, err := f.service.ListOrderIncidents(ctx, f.order.ID, f.ownerID, "client")
	if err != nil || len(incidents) != 1 {
		t.Fatalf("duplicate shortage: %+v %v", incidents, err)
	}
	if _, err = f.service.ResolveOrderIncident(ctx, incidents[0].ID, f.ownerID, "admin", api.OrderIncidentResolve{Resolution: "accept_discrepancy", Note: "Расхождение зафиксировано для отдельной претензии"}); err != nil {
		t.Fatal(err)
	}
	wrong := "Другой человек"
	if _, err = f.service.CompleteAssignment(ctx, f.offer.ID, f.driverUserID, "driver", api.DeliveryComplete{RecipientName: &wrong}); !errors.Is(err, ErrInvalidOrderInput) {
		t.Fatalf("receiver overwritten: %v", err)
	}
	completed, err := f.service.CompleteAssignment(ctx, f.offer.ID, f.driverUserID, "driver", api.DeliveryComplete{})
	if err != nil || completed.RecipientName == nil || *completed.RecipientName != receiptReq.RecipientName {
		t.Fatalf("completion lost receipt: %+v %v", completed, err)
	}
	for _, subject := range []string{"delivery.completed.v1", "document.requested.v1"} {
		var payload []byte
		query := `SELECT payload FROM integration_outbox WHERE subject=$1 AND payload->>'order_id'=$2`
		if subject == "document.requested.v1" {
			query += ` AND payload->>'type'='service_report'`
		}
		if err = pool.QueryRow(ctx, query, subject, f.order.ID.String()).Scan(&payload); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(payload), "Фактически принято мест") || !strings.Contains(string(payload), "Доверенность") {
			t.Fatalf("receipt absent from %s: %s", subject, payload)
		}
	}
	if _, err = f.service.GetDeliveryReceipt(ctx, f.order.ID, uuid.New(), "client"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("private receipt exposed: %v", err)
	}
}

func TestDeclaredPackagesMismatchIsReportedAtWarehouseReceipt(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 88)
	raw, _ := json.Marshal(models.TransportDetails{PackageCount: 5})
	if _, err := pool.Exec(ctx, `UPDATE orders SET transport_policy_version=1,transport_details=$1 WHERE id=$2`, raw, f.order.ID); err != nil {
		t.Fatal(err)
	}
	op, err := f.service.CreateWarehouseOperation(ctx, f.order.ID, f.ownerID, "admin", api.WarehouseOperationCreate{OperationType: "receive", PackageCount: 4, CargoCondition: "Получено четыре места"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := f.service.ListOrderIncidents(ctx, f.order.ID, f.ownerID, "client")
	if err != nil || len(items) != 1 || items[0].SourceOperationID == nil || *items[0].SourceOperationID != op.ID {
		t.Fatalf("declared shortage hidden: %+v %v", items, err)
	}
}

func TestWarehouseDeliveryReceiptRequiresUnloadAndResponsibleManager(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 90)
	destination := newExpiryFixture(t, pool, 92)
	originManager := warehouseTestManager(t, pool, f.warehouseID)
	destinationManager := warehouseTestManager(t, pool, destination.warehouseID)
	raw, _ := json.Marshal(models.TransportDetails{PackageCount: 5})
	if _, err := pool.Exec(ctx, `UPDATE orders SET destination_warehouse_id=$1,transport_policy_version=1,transport_details=$2 WHERE id=$3`, destination.warehouseID, raw, f.order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []api.WarehouseOperationCreateOperationType{"receive", "load"} {
		req := api.WarehouseOperationCreate{OperationType: kind, PackageCount: 5, CargoCondition: "Целый"}
		if kind == "load" {
			req.AssignmentId = &f.offer.ID
		}
		if _, err := f.service.CreateWarehouseOperation(ctx, f.order.ID, originManager, "manager", req); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ArriveAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	req := api.DeliveryReceiptCreate{AssignmentId: f.offer.ID, RecipientName: "Иван Петров", RecipientRole: "Кладовщик", AuthorityBasis: "Приёмка представителем склада, со слов менеджера", PackageCount: 5, CargoCondition: "Целый"}
	for _, actor := range []struct {
		id   uuid.UUID
		role string
	}{{f.driverUserID, "driver"}, {originManager, "manager"}, {f.ownerID, "client"}} {
		if _, err := f.service.CreateDeliveryReceipt(ctx, f.order.ID, actor.id, actor.role, req); !errors.Is(err, ErrForbidden) {
			t.Fatalf("wrong actor confirmed: %v", err)
		}
	}
	if _, err := f.service.CreateDeliveryReceipt(ctx, f.order.ID, destinationManager, "manager", req); !errors.Is(err, ErrWarehouseOperationRequired) {
		t.Fatalf("receipt without unloading: %v", err)
	}
	if _, err := f.service.CreateWarehouseOperation(ctx, f.order.ID, destinationManager, "manager", api.WarehouseOperationCreate{OperationType: "unload", AssignmentId: &f.offer.ID, PackageCount: 4, CargoCondition: "Одно место отсутствует"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateDeliveryReceipt(ctx, f.order.ID, destinationManager, "manager", req); !errors.Is(err, ErrInvalidOrderInput) {
		t.Fatalf("receipt contradicted unloading: %v", err)
	}
	req.PackageCount = 4
	receipt, err := f.service.CreateDeliveryReceipt(ctx, f.order.ID, destinationManager, "manager", req)
	if err != nil || receipt.RecordedByUserID != destinationManager {
		t.Fatalf("warehouse receipt: %+v %v", receipt, err)
	}
	incidents, err := f.service.ListOrderIncidents(ctx, f.order.ID, f.ownerID, "client")
	if err != nil || len(incidents) != 1 {
		t.Fatalf("duplicated warehouse shortage: %+v %v", incidents, err)
	}
	if _, err = f.service.ResolveOrderIncident(ctx, incidents[0].ID, destinationManager, "manager", api.OrderIncidentResolve{Resolution: "accept_discrepancy", Note: "Недостача отражена в приёмке"}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CompleteAssignment(ctx, f.offer.ID, destinationManager, "manager", api.DeliveryComplete{}); err != nil {
		t.Fatal(err)
	}
}

func TestDirectReceiptUsesLoadedQuantityAfterAcceptedShortage(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 94)
	manager := warehouseTestManager(t, pool, f.warehouseID)
	raw, _ := json.Marshal(models.TransportDetails{PackageCount: 5})
	if _, err := pool.Exec(ctx, `UPDATE orders SET transport_policy_version=1,transport_details=$1 WHERE id=$2`, raw, f.order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", api.WarehouseOperationCreate{OperationType: "receive", PackageCount: 5, CargoCondition: "Целый"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", api.WarehouseOperationCreate{OperationType: "load", AssignmentId: &f.offer.ID, PackageCount: 4, CargoCondition: "Одно место осталось для отдельного разбирательства"}); err != nil {
		t.Fatal(err)
	}
	incidents, err := f.service.ListOrderIncidents(ctx, f.order.ID, f.ownerID, "client")
	if err != nil || len(incidents) != 1 {
		t.Fatalf("loading shortage: %+v %v", incidents, err)
	}
	if _, err = f.service.ResolveOrderIncident(ctx, incidents[0].ID, manager, "manager", api.OrderIncidentResolve{Resolution: "accept_discrepancy", Note: "Согласована перевозка четырёх мест"}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ArriveAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CreateDeliveryReceipt(ctx, f.order.ID, f.driverUserID, "driver", api.DeliveryReceiptCreate{AssignmentId: f.offer.ID, RecipientName: "Иван Петров", RecipientRole: "Получатель", AuthorityBasis: "Личная приёмка, со слов водителя", PackageCount: 4, CargoCondition: "Все загруженные места доставлены"}); err != nil {
		t.Fatal(err)
	}
	incidents, err = f.service.ListOrderIncidents(ctx, f.order.ID, f.ownerID, "client")
	if err != nil || len(incidents) != 1 || incidents[0].Status != "resolved" {
		t.Fatalf("settled shortage duplicated: %+v %v", incidents, err)
	}
	if _, err = f.service.CompleteAssignment(ctx, f.offer.ID, f.driverUserID, "driver", api.DeliveryComplete{}); err != nil {
		t.Fatal(err)
	}
}
