package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/events"
	"github.com/anxi0uz/logiflow/internal/models"
	"github.com/google/uuid"
)

func TestBreakdownUndoLoadingAndReplaceVehicle(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 40)
	target := newExpiryFixture(t, pool, 42)
	manager := warehouseTestManager(t, pool, f.warehouseID)
	if _, err := f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	loadTestAssignment(t, f.service, f.offer, f.ownerID)
	req := api.OrderIncidentCreate{Kind: "vehicle_breakdown", Description: "Не запускается двигатель после погрузки"}
	if _, err := f.service.CreateOrderIncident(ctx, f.order.ID, target.driverUserID, "driver", req); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unrelated driver: %v", err)
	}
	incident, err := f.service.CreateOrderIncident(ctx, f.order.ID, f.driverUserID, "driver", req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); !errors.Is(err, ErrIncidentOpen) {
		t.Fatalf("departed with breakdown: %v", err)
	}
	resolution := api.OrderIncidentResolve{Resolution: "resources_replaced", Note: "Выделена другая машина"}
	if _, err = f.service.ResolveOrderIncident(ctx, incident.ID, manager, "manager", resolution); !errors.Is(err, ErrIncidentResolutionInvalid) {
		t.Fatalf("invented replacement: %v", err)
	}
	reason := "Фактически разгрузили неисправную машину"
	_, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", api.WarehouseOperationCreate{OperationType: "undo_load", AssignmentId: &f.offer.ID, PackageCount: 1, CargoCondition: "Без повреждений", Comment: &reason})
	if err != nil {
		t.Fatal(err)
	}
	assertOfferStatus(t, f, models.AssignmentReleased)
	order, err := f.service.GetOrder(ctx, f.order.ID, f.ownerID, "client")
	if err != nil || order.Status != models.OrderReadyForDispatch || order.DriverID != nil || order.StartedAt != nil {
		t.Fatalf("undo rewrote lifecycle: %+v %v", order, err)
	}
	queue, err := f.service.ListWarehouseQueue(ctx, f.warehouseID, manager, "manager", api.ListWarehouseQueueParams{})
	if err != nil || len(queue) != 1 || queue[0].WarehouseStage != "incident_open" || queue[0].OpenIncidentCount != 1 {
		t.Fatalf("hidden problem: %+v %v", queue, err)
	}
	if _, err = f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); !errors.Is(err, ErrInvalidOrderTransition) {
		t.Fatalf("old loaded assignment usable: %v", err)
	}
	next, err := f.service.CreateAssignment(ctx, f.order.ID, manager, "manager", api.AssignmentCreate{DriverId: f.driver.ID, VehicleId: target.vehicle.ID, PlannedFrom: f.from, PlannedTo: f.to})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.AcceptAssignment(ctx, next.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	load := api.WarehouseOperationCreate{OperationType: "load", AssignmentId: &next.ID, PackageCount: 1, CargoCondition: "Без замечаний"}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", load); !errors.Is(err, ErrIncidentOpen) {
		t.Fatalf("unresolved breakdown ignored: %v", err)
	}
	if _, err = f.service.ResolveOrderIncident(ctx, incident.ID, f.ownerID, "client", resolution); !errors.Is(err, ErrForbidden) {
		t.Fatalf("client resolved breakdown: %v", err)
	}
	if _, err = f.service.ResolveOrderIncident(ctx, incident.ID, manager, "manager", resolution); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ResolveOrderIncident(ctx, incident.ID, manager, "manager", resolution); !errors.Is(err, ErrIncidentStale) {
		t.Fatalf("resolution rewritten: %v", err)
	}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", load); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.StartAssignment(ctx, next.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", api.WarehouseOperationCreate{OperationType: "undo_load", AssignmentId: &next.ID, PackageCount: 1, CargoCondition: "Целый", Comment: &reason}); !errors.Is(err, ErrInvalidOrderTransition) {
		t.Fatalf("unloaded moving trip at origin: %v", err)
	}
	ops, err := f.service.ListOrderWarehouseOperations(ctx, f.order.ID, f.ownerID, "client")
	if err != nil || len(ops) != 4 {
		t.Fatalf("loading history lost: %+v %v", ops, err)
	}
	var released int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM dispatch_release_queue WHERE assignment_id=$1`, f.offer.ID).Scan(&released); err != nil || released != 1 {
		t.Fatalf("reservation release not queued: %d %v", released, err)
	}
}

func TestWarehouseShortageCreatesIncidentAndBlocksCompletion(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	for index, actual := range []int{4, 0} {
		t.Run(map[int]string{4: "partial", 0: "nothing_received"}[actual], func(t *testing.T) {
			f := newExpiryFixture(t, pool, 50+index*4)
			destination := newExpiryFixture(t, pool, 52+index*4)
			manager := warehouseTestManager(t, pool, destination.warehouseID)
			originManager := warehouseTestManager(t, pool, f.warehouseID)
			if _, err := pool.Exec(ctx, `UPDATE orders SET destination_warehouse_id=$1 WHERE id=$2`, destination.warehouseID, f.order.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []api.WarehouseOperationCreateOperationType{"receive", "load"} {
				req := api.WarehouseOperationCreate{OperationType: kind, PackageCount: 5, CargoCondition: "Упаковка цела"}
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
			op, err := f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", api.WarehouseOperationCreate{OperationType: "unload", AssignmentId: &f.offer.ID, PackageCount: actual, CargoCondition: "Недостача"})
			if err != nil {
				t.Fatal(err)
			}
			list, err := f.service.ListOrderIncidents(ctx, f.order.ID, f.ownerID, "client")
			if err != nil || len(list) != 1 || list[0].ExpectedPackages == nil || *list[0].ExpectedPackages != 5 || *list[0].ActualPackages != actual || list[0].SourceOperationID == nil || *list[0].SourceOperationID != op.ID {
				t.Fatalf("shortage not recorded: %+v %v", list, err)
			}
			if _, err = f.service.CompleteAssignment(ctx, f.offer.ID, manager, "manager", api.DeliveryComplete{}); !errors.Is(err, ErrIncidentOpen) {
				t.Fatalf("shortage silently completed: %v", err)
			}
			resolution := api.OrderIncidentResolve{Resolution: "accept_discrepancy", Note: "Расхождение принято, претензия зарегистрирована отдельно"}
			if _, err = f.service.ResolveOrderIncident(ctx, list[0].ID, originManager, "manager", resolution); !errors.Is(err, ErrForbidden) {
				t.Fatalf("wrong warehouse resolved shortage: %v", err)
			}
			if _, err = f.service.ResolveOrderIncident(ctx, list[0].ID, manager, "manager", api.OrderIncidentResolve{Resolution: "remediated", Note: "Закрыть"}); !errors.Is(err, ErrIncidentResolutionInvalid) {
				t.Fatalf("shortage dismissed: %v", err)
			}
			if _, err = f.service.ResolveOrderIncident(ctx, list[0].ID, manager, "manager", resolution); err != nil {
				t.Fatal(err)
			}
			if _, err = f.service.CompleteAssignment(ctx, f.offer.ID, manager, "manager", api.DeliveryComplete{}); err != nil {
				t.Fatal(err)
			}
			var payload []byte
			if err = pool.QueryRow(ctx, `SELECT payload FROM integration_outbox WHERE subject='document.requested.v1' AND payload->>'reference_id'=$1 AND payload->>'type'='service_report'`, f.order.ID.String()).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			var report events.DocumentRequested
			if err = json.Unmarshal(payload, &report); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, field := range report.Fields {
				if field.Label == "Проблема / quantity_discrepancy" {
					found = true
				}
			}
			if !found {
				t.Fatal("report concealed shortage")
			}
		})
	}
}

func TestCancelledCargoReturnRemainsInWarehouseQueue(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 60)
	manager := warehouseTestManager(t, pool, f.warehouseID)
	if _, err := f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", api.WarehouseOperationCreate{OperationType: "receive", PackageCount: 3, CargoCondition: "Без замечаний"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CancelOrder(ctx, f.order.ID, f.ownerID, "client", api.OrderCancel{}); err != nil {
		t.Fatal(err)
	}
	queue, err := f.service.ListWarehouseQueue(ctx, f.warehouseID, manager, "manager", api.ListWarehouseQueueParams{})
	if err != nil || len(queue) != 1 || queue[0].WarehouseStage != "awaiting_return" {
		t.Fatalf("cancel hid stored cargo: %+v %v", queue, err)
	}
	comment, recipient := "Выдано представителю отправителя", "Иван Петров"
	req := api.WarehouseOperationCreate{OperationType: "return_to_sender", PackageCount: 3, CargoCondition: "Целый", Comment: &comment}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", req); !errors.Is(err, ErrInvalidOrderInput) {
		t.Fatalf("return without recipient: %v", err)
	}
	req.RecipientName = &recipient
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", req); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", req); !errors.Is(err, ErrWarehouseOperationConflict) {
		t.Fatalf("duplicate return: %v", err)
	}
	queue, err = f.service.ListWarehouseQueue(ctx, f.warehouseID, manager, "manager", api.ListWarehouseQueueParams{})
	if err != nil || len(queue) != 0 {
		t.Fatalf("returned cargo still in queue: %+v %v", queue, err)
	}
	include := true
	queue, err = f.service.ListWarehouseQueue(ctx, f.warehouseID, manager, "manager", api.ListWarehouseQueueParams{IncludeClosed: &include})
	if err != nil || len(queue) != 1 || queue[0].WarehouseStage != "returned_to_sender" {
		t.Fatalf("return history hidden: %+v %v", queue, err)
	}
}

func TestRecipientRefusalAndConcurrentResolution(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 62)
	if _, err := pool.Exec(ctx, `UPDATE orders SET origin_warehouse_id=NULL WHERE id=$1`, f.order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ArriveAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	incident, err := f.service.CreateOrderIncident(ctx, f.order.ID, f.driverUserID, "driver", api.OrderIncidentCreate{Kind: "recipient_refusal", Description: "Получатель отказался принять груз"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CompleteAssignment(ctx, f.offer.ID, f.driverUserID, "driver", api.DeliveryComplete{}); !errors.Is(err, ErrIncidentOpen) {
		t.Fatalf("refusal ignored: %v", err)
	}
	req := api.OrderIncidentResolve{Resolution: "recipient_accepted", Note: "После согласования получатель принял груз"}
	if _, err = f.service.ResolveOrderIncident(ctx, incident.ID, f.ownerID, "admin", req); !errors.Is(err, ErrIncidentResolutionInvalid) {
		t.Fatalf("acceptance without person: %v", err)
	}
	recipient := "Получатель Петров"
	req.RecipientName = &recipient
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.service.ResolveOrderIncident(ctx, incident.ID, f.ownerID, "admin", req)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, stale := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrIncidentStale) {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatalf("resolution race: %d %d", success, stale)
	}
	if _, err = f.service.CompleteAssignment(ctx, f.offer.ID, f.driverUserID, "driver", api.DeliveryComplete{RecipientName: &recipient}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CreateOrderIncident(ctx, f.order.ID, f.driverUserID, "driver", api.OrderIncidentCreate{Kind: "other", Description: "Позднее изменение"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("terminal incident edited: %v", err)
	}
	if _, err = f.service.ListOrderIncidents(ctx, f.order.ID, uuid.New(), "client"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign incident history: %v", err)
	}
}

func TestCargoDamageBlocksLoadingUntilManagerDecision(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	f := newExpiryFixture(t, pool, 64)
	manager := warehouseTestManager(t, pool, f.warehouseID)
	if _, err := f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", api.WarehouseOperationCreate{OperationType: "receive", PackageCount: 1, CargoCondition: "Повреждена упаковка"}); err != nil {
		t.Fatal(err)
	}
	incident, err := f.service.CreateOrderIncident(ctx, f.order.ID, manager, "manager", api.OrderIncidentCreate{Kind: "cargo_damage", Description: "Надорвана наружная упаковка"})
	if err != nil {
		t.Fatal(err)
	}
	req := api.WarehouseOperationCreate{OperationType: "load", AssignmentId: &f.offer.ID, PackageCount: 1, CargoCondition: "Повреждена упаковка"}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", req); !errors.Is(err, ErrIncidentOpen) {
		t.Fatalf("damaged cargo loaded without decision: %v", err)
	}
	if _, err = f.service.ResolveOrderIncident(ctx, incident.ID, manager, "manager", api.OrderIncidentResolve{Resolution: "remediated", Note: "Упаковка заменена, содержимое проверено"}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", req); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
		t.Fatal(err)
	}
}

func TestOriginWarehouseDiscrepanciesPreserveFacts(t *testing.T) {
	pool := expiryTestPool(t)
	ctx := context.Background()
	for index, kind := range []api.WarehouseOperationCreateOperationType{"load", "undo_load", "return_to_sender"} {
		t.Run(string(kind), func(t *testing.T) {
			f := newExpiryFixture(t, pool, 70+index)
			manager := warehouseTestManager(t, pool, f.warehouseID)
			if _, err := f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", api.WarehouseOperationCreate{OperationType: "receive", PackageCount: 5, CargoCondition: "Целый"}); err != nil {
				t.Fatal(err)
			}
			comment, recipient := "Фактически передано четыре места", "Представитель отправителя"
			req := api.WarehouseOperationCreate{OperationType: kind, PackageCount: 4, CargoCondition: "Недостача", Comment: &comment}
			if kind == "return_to_sender" {
				if _, err := f.service.CancelOrder(ctx, f.order.ID, f.ownerID, "client", api.OrderCancel{}); err != nil {
					t.Fatal(err)
				}
				req.RecipientName = &recipient
			} else {
				if _, err := f.service.AcceptAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); err != nil {
					t.Fatal(err)
				}
				req.AssignmentId = &f.offer.ID
				if kind == "undo_load" {
					if _, err := f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", api.WarehouseOperationCreate{OperationType: "load", AssignmentId: &f.offer.ID, PackageCount: 5, CargoCondition: "Целый"}); err != nil {
						t.Fatal(err)
					}
				}
			}
			op, err := f.service.CreateWarehouseOperation(ctx, f.order.ID, manager, "manager", req)
			if err != nil {
				t.Fatal(err)
			}
			items, err := f.service.ListOrderIncidents(ctx, f.order.ID, f.ownerID, "client")
			if err != nil || len(items) != 1 || items[0].ExpectedPackages == nil || items[0].ActualPackages == nil || items[0].SourceOperationID == nil || *items[0].ExpectedPackages != 5 || *items[0].ActualPackages != 4 || *items[0].SourceOperationID != op.ID {
				t.Fatalf("lost %s discrepancy: %+v %v", kind, items, err)
			}
			queue, err := f.service.ListWarehouseQueue(ctx, f.warehouseID, manager, "manager", api.ListWarehouseQueueParams{})
			if err != nil || len(queue) != 1 || queue[0].WarehouseStage != "incident_open" {
				t.Fatalf("hidden %s discrepancy: %+v %v", kind, queue, err)
			}
			if kind == "load" {
				if _, err := f.service.StartAssignment(ctx, f.offer.ID, f.driverUserID, "driver"); !errors.Is(err, ErrIncidentOpen) {
					t.Fatalf("departed after loading shortage: %v", err)
				}
			}
			if kind == "undo_load" {
				assertOfferStatus(t, f, models.AssignmentReleased)
			}
		})
	}
}
