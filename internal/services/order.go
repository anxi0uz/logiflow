package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	api "github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/config"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/anxi0uz/logiflow/pkg/routing"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

type OrderService struct {
	db           *pgxpool.Pool
	config       config.Config
	routePlanner routing.Planner
}
type OrderServicer interface {
	CreateOrder(ctx context.Context, req api.OrderCreate, userID uuid.UUID, role string) (*CreateOrderResult, error)
	ListOrders(ctx context.Context, userID uuid.UUID, role string, params api.ListOrdersParams) ([]models.Order, error)
	GetOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Order, error)
	UpdateDraftOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.OrderDraftUpdate) (*models.Order, error)
	ListAssignments(ctx context.Context, userID uuid.UUID, role string, params api.ListAssignmentsParams) ([]models.Assignment, error)
	ListOrderAssignments(ctx context.Context, orderID uuid.UUID, userID uuid.UUID, role string) ([]models.Assignment, error)
	SubmitOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Order, error)
	CancelOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.OrderCancel) (*models.Order, error)
	CreateAssignment(ctx context.Context, orderID uuid.UUID, userID uuid.UUID, role string, req api.AssignmentCreate) (*models.Assignment, error)
	AcceptAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Assignment, error)
	RejectAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.AssignmentReject) (*models.Assignment, error)
	StartAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Assignment, error)
	ArriveAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Assignment, error)
	CompleteAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.DeliveryComplete) (*models.Assignment, error)
	GetOrdersReport(ctx context.Context, userID uuid.UUID, role string, params api.GetOrdersReportParams) ([]models.Order, error)
	GetDashboard(ctx context.Context, userID uuid.UUID, role string) (*models.DashboardReport, error)
}

var ErrCannotCancel = ErrInvalidOrderTransition

type CreateOrderResult struct {
	Order models.Order
	Route models.Route
}

func NewOrderService(db *pgxpool.Pool, cfg config.Config) *OrderService {
	return &OrderService{db: db, config: cfg, routePlanner: routing.OSRMPlanner{BaseURL: cfg.Routing.BaseURL}}
}

func (s *OrderService) orderPrice(distanceKm, weightKg, volumeM3 float64) float64 {
	return s.config.Pricing.BaseFee +
		distanceKm*s.config.Pricing.PerKm +
		weightKg*s.config.Pricing.PerKg +
		volumeM3*s.config.Pricing.PerM3
}

func (s *OrderService) CreateOrder(ctx context.Context, req api.OrderCreate, userID uuid.UUID, role string) (*CreateOrderResult, error) {
	if role != "client" && role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	if role == "manager" {
		warehouseID, err := s.managerWarehouseID(ctx, s.db, userID)
		if err != nil {
			return nil, err
		}
		if req.OriginWarehouseId == nil || *req.OriginWarehouseId != warehouseID {
			return nil, ErrForbidden
		}
	}
	if (req.OriginWarehouseId == nil && (req.OriginAddress == nil || *req.OriginAddress == "")) || req.DestinationAddress == "" ||
		(req.WeightKg != nil && *req.WeightKg < 0) || (req.VolumeM3 != nil && *req.VolumeM3 < 0) ||
		(req.PickupFrom != nil && req.PickupTo != nil && !req.PickupFrom.Before(*req.PickupTo)) {
		return nil, ErrInvalidOrderInput
	}
	origin := routing.Endpoint{}
	if req.OriginAddress != nil {
		origin.Address = *req.OriginAddress
	}
	if req.OriginWarehouseId != nil {
		warehouse, err := storage.GetOne[models.Warehouse](ctx, s.db, "warehouses", func(sb *sqlbuilder.SelectBuilder) {
			sb.Where(sb.EQ("id", *req.OriginWarehouseId))
		})
		if err != nil {
			return nil, err
		}
		if warehouse.Latitude == nil || warehouse.Longitude == nil {
			return nil, ErrInvalidOrderInput
		}
		origin.Coordinates = &routing.Coordinates{Latitude: *warehouse.Latitude, Longitude: *warehouse.Longitude}
	}
	destination := routing.Endpoint{Address: req.DestinationAddress}
	if req.DestinationWarehouseId != nil {
		warehouse, err := storage.GetOne[models.Warehouse](ctx, s.db, "warehouses", func(sb *sqlbuilder.SelectBuilder) {
			sb.Where(sb.EQ("id", *req.DestinationWarehouseId))
		})
		if err != nil {
			return nil, err
		}
		if warehouse.Latitude == nil || warehouse.Longitude == nil {
			return nil, ErrInvalidOrderInput
		}
		destination.Coordinates = &routing.Coordinates{Latitude: *warehouse.Latitude, Longitude: *warehouse.Longitude}
	}
	estimate, err := s.routePlanner.Plan(ctx, origin, destination)
	if err != nil {
		return nil, err
	}

	var weightKg, volumeM3 float64
	if req.WeightKg != nil {
		weightKg = float64(*req.WeightKg)
	}
	if req.VolumeM3 != nil {
		volumeM3 = float64(*req.VolumeM3)
	}
	price := s.orderPrice(estimate.DistanceKm, weightKg, volumeM3)

	orderID := uuid.New()
	order := models.Order{
		ID:                 orderID,
		CreatedByID:        &userID,
		OriginWarehouseID:  req.OriginWarehouseId,
		DestinationAddress: req.DestinationAddress,
		Status:             models.OrderDraft,
		TotalPrice:         &price,
		CreatedAt:          time.Now(),
		PickupFrom:         req.PickupFrom,
		PickupTo:           req.PickupTo,
	}
	if req.OriginAddress != nil && *req.OriginAddress != "" {
		order.OriginAddress = *req.OriginAddress
	}
	if req.CargoDescription != nil {
		order.CargoDescription = *req.CargoDescription
	}
	if req.WeightKg != nil {
		order.WeightKg = weightKg
	}
	if req.VolumeM3 != nil {
		order.VolumeM3 = volumeM3
	}

	coordsJSON, err := json.Marshal(estimate.Coordinates)
	if err != nil {
		return nil, fmt.Errorf("marshal coordinates: %w", err)
	}
	routeModel := models.Route{
		ID:          uuid.New(),
		OrderID:     orderID,
		Coordinates: coordsJSON,
		DurationSec: &estimate.DurationSec,
		DistanceKm:  &estimate.DistanceKm,
		Status:      "pending",
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.ErrorContext(ctx, "tx rollback failed", slog.String("error", err.Error()))
		}
	}()

	if err := storage.Create(ctx, "orders", order, tx); err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	if err := storage.Create(ctx, "routes", routeModel, tx); err != nil {
		return nil, fmt.Errorf("create route: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &CreateOrderResult{Order: order, Route: routeModel}, nil
}

func (s *OrderService) ListOrders(ctx context.Context, userID uuid.UUID, role string, params api.ListOrdersParams) ([]models.Order, error) {
	if role != "client" && role != "driver" && role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	var driverID *uuid.UUID
	var warehouseID uuid.UUID
	if role == "manager" {
		var err error
		warehouseID, err = s.managerWarehouseID(ctx, s.db, userID)
		if err != nil {
			return nil, err
		}
	}
	if role == "driver" {
		driver, err := storage.GetOne[models.Driver](ctx, s.db, "drivers", func(sb *sqlbuilder.SelectBuilder) {
			sb.Where(sb.EQ("user_id", userID))
		})
		if err != nil {
			return nil, fmt.Errorf("get driver: %w", err)
		}
		driverID = &driver.ID
	}

	orders, err := storage.GetAll[models.Order](ctx, "orders", s.db, func(sb *sqlbuilder.SelectBuilder) {
		switch role {
		case "client":
			sb.Where(sb.EQ("created_by_id", userID))
		case "driver":
			sb.Where(sb.EQ("driver_id", driverID))
		case "manager":
			sb.Where(sb.EQ("origin_warehouse_id", warehouseID))
		}
		if params.Status != nil {
			sb.Where(sb.EQ("status", *params.Status))
		}
		if params.DriverId != nil {
			sb.Where(sb.EQ("driver_id", *params.DriverId))
		}
	})

	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	return orders, nil
}
func (s *OrderService) GetOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Order, error) {
	if role != "client" && role != "driver" && role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	order, err := storage.GetOne[models.Order](ctx, s.db, "orders", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", id))
	})

	if err != nil {
		return nil, err
	}

	if role == "client" && (order.CreatedByID == nil || *order.CreatedByID != userID) {
		return nil, ErrForbidden
	}
	if role == "manager" {
		warehouseID, err := s.managerWarehouseID(ctx, s.db, userID)
		if err != nil {
			return nil, err
		}
		if order.OriginWarehouseID == nil || *order.OriginWarehouseID != warehouseID {
			return nil, ErrForbidden
		}
	}
	if role == "driver" {
		driver, err := storage.GetOne[models.Driver](ctx, s.db, "drivers", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("user_id", userID)) })
		if err != nil {
			return nil, ErrForbidden
		}
		if _, err := storage.GetOne[models.Assignment](ctx, s.db, "assignments", func(sb *sqlbuilder.SelectBuilder) {
			sb.Where(
				sb.EQ("order_id", id),
				sb.EQ("driver_id", driver.ID),
				sb.Or(
					sb.In("status", models.AssignmentAccepted, models.AssignmentActive, models.AssignmentCompleted),
					sb.And(sb.EQ("status", models.AssignmentPendingAcceptance), sb.GT("offer_expires_at", time.Now())),
				),
			).Limit(1)
		}); err != nil {
			return nil, ErrForbidden
		}
	}
	return order, nil
}

func (s *OrderService) managerWarehouseID(ctx context.Context, db storage.Querier, userID uuid.UUID) (uuid.UUID, error) {
	manager, err := storage.GetOne[models.Manager](ctx, db, "managers", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("user_id", userID))
	})
	if errors.Is(err, storage.ErrNotFound) || (err == nil && manager.WarehouseID == nil) {
		return uuid.Nil, ErrForbidden
	}
	if err != nil {
		return uuid.Nil, err
	}
	return *manager.WarehouseID, nil
}

func (s *OrderService) UpdateDraftOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.OrderDraftUpdate) (*models.Order, error) {
	if role != "client" && role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	if req.CargoDescription == nil && req.WeightKg == nil && req.VolumeM3 == nil && req.PickupFrom == nil && req.PickupTo == nil {
		return nil, ErrInvalidOrderInput
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	order, err := storage.GetOne[models.Order](ctx, tx, "orders", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", id)).ForUpdate()
	})
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
	if req.CargoDescription != nil {
		order.CargoDescription = *req.CargoDescription
	}
	if req.WeightKg != nil {
		order.WeightKg = float64(*req.WeightKg)
	}
	if req.VolumeM3 != nil {
		order.VolumeM3 = float64(*req.VolumeM3)
	}
	if req.PickupFrom != nil {
		order.PickupFrom = req.PickupFrom
	}
	if req.PickupTo != nil {
		order.PickupTo = req.PickupTo
	}
	if order.WeightKg < 0 || order.VolumeM3 < 0 || (order.PickupFrom != nil && order.PickupTo != nil && !order.PickupFrom.Before(*order.PickupTo)) {
		return nil, ErrInvalidOrderInput
	}
	route, err := storage.GetOne[models.Route](ctx, tx, "routes", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("order_id", id)) })
	if err != nil {
		return nil, err
	}
	if route.DistanceKm == nil || route.DurationSec == nil || len(route.Coordinates) == 0 {
		return nil, ErrInvalidOrderInput
	}
	price := s.orderPrice(*route.DistanceKm, order.WeightKg, order.VolumeM3)
	order.TotalPrice = &price
	if err := storage.Update(ctx, "orders", *order, tx, func(ub *sqlbuilder.UpdateBuilder) { ub.Where(ub.EQ("id", id)) }); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return order, nil
}

func (s *OrderService) ListAssignments(ctx context.Context, userID uuid.UUID, role string, params api.ListAssignmentsParams) ([]models.Assignment, error) {
	if role != "driver" && role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	var driverID uuid.UUID
	var warehouseID uuid.UUID
	if role == "manager" {
		var err error
		warehouseID, err = s.managerWarehouseID(ctx, s.db, userID)
		if err != nil {
			return nil, err
		}
	}
	if role == "driver" {
		driver, err := storage.GetOne[models.Driver](ctx, s.db, "drivers", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("user_id", userID)) })
		if err != nil {
			return nil, err
		}
		driverID = driver.ID
	}
	return storage.GetAll[models.Assignment](ctx, "assignments", s.db, func(sb *sqlbuilder.SelectBuilder) {
		if role == "driver" {
			sb.Where(sb.EQ("driver_id", driverID))
		}
		if role == "manager" {
			sb.Where("order_id IN (SELECT id FROM orders WHERE origin_warehouse_id = " + sb.Var(warehouseID) + ")")
		}
		if params.Status != nil {
			sb.Where(sb.EQ("status", *params.Status))
		}
		if params.OrderId != nil {
			sb.Where(sb.EQ("order_id", *params.OrderId))
		}
		sb.OrderBy("assigned_at DESC", "id DESC")
	})
}

func (s *OrderService) ListOrderAssignments(ctx context.Context, orderID uuid.UUID, userID uuid.UUID, role string) ([]models.Assignment, error) {
	if role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	if _, err := s.GetOrder(ctx, orderID, userID, role); err != nil {
		return nil, err
	}
	return storage.GetAll[models.Assignment](ctx, "assignments", s.db, func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("order_id", orderID)).OrderBy("assigned_at DESC", "id DESC")
	})
}
func (s *OrderService) GetOrdersReport(ctx context.Context, userID uuid.UUID, role string, params api.GetOrdersReportParams) ([]models.Order, error) {
	if role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	var warehouseID uuid.UUID
	if role == "manager" {
		var err error
		warehouseID, err = s.managerWarehouseID(ctx, s.db, userID)
		if err != nil {
			return nil, err
		}
	}
	orders, err := storage.GetAll[models.Order](ctx, "orders", s.db, func(sb *sqlbuilder.SelectBuilder) {
		if role == "manager" {
			sb.Where(sb.EQ("origin_warehouse_id", warehouseID))
		}
		if params.Status != nil {
			sb.Where(sb.EQ("status", string(*params.Status)))
		}
		if params.DriverId != nil {
			sb.Where(sb.EQ("driver_id", *params.DriverId))
		}
		if params.WarehouseId != nil {
			sb.Where(sb.EQ("origin_warehouse_id", *params.WarehouseId))
		}
		if params.From != nil {
			sb.Where(sb.GE("created_at", params.From.Time))
		}
		if params.To != nil {
			sb.Where(sb.LE("created_at", params.To.Time))
		}
	})
	if err != nil {
		return nil, fmt.Errorf("get orders report: %w", err)
	}
	return orders, nil
}
func (s *OrderService) GetDashboard(ctx context.Context, userID uuid.UUID, role string) (*models.DashboardReport, error) {
	if role != "manager" && role != "admin" {
		return nil, ErrForbidden
	}
	var warehouseID *uuid.UUID
	if role == "manager" {
		id, err := s.managerWarehouseID(ctx, s.db, userID)
		if err != nil {
			return nil, err
		}
		warehouseID = &id
	}

	var report models.DashboardReport
	g, gctx := errgroup.WithContext(ctx)

	// query 1: order counts + revenue
	g.Go(func() error {
		row := s.db.QueryRow(gctx, `
            SELECT
                COUNT(*)                                                                 AS total,
                COUNT(*) FILTER (WHERE status = 'completed')                             AS delivered,
                COUNT(*) FILTER (WHERE status = 'in_transit')                            AS in_transit,
                COUNT(*) FILTER (WHERE status IN ('draft', 'ready_for_dispatch'))         AS pending,
                COUNT(*) FILTER (WHERE status = 'cancelled')                             AS cancelled,
                COALESCE(SUM(total_price) FILTER (WHERE status = 'completed'), 0)        AS revenue_total,
                COALESCE(SUM(total_price) FILTER (WHERE status = 'completed'
                    AND created_at >= date_trunc('month', NOW())), 0)                    AS revenue_this_month
            FROM orders
            WHERE ($1::uuid IS NULL OR origin_warehouse_id = $1)
        `, warehouseID)
		return row.Scan(
			&report.Orders.Total,
			&report.Orders.Delivered,
			&report.Orders.InTransit,
			&report.Orders.Pending,
			&report.Orders.Cancelled,
			&report.Revenue.Total,
			&report.Revenue.ThisMonth,
		)
	})

	// query 2: top drivers by completed orders
	g.Go(func() error {
		rows, err := s.db.Query(gctx, `
            SELECT
                d.id,
                u.full_name,
                d.status,
                d.rating,
                COUNT(o.id) FILTER (WHERE o.status = 'completed') AS completed_orders
            FROM drivers d
            JOIN users u ON u.id = d.user_id
            LEFT JOIN orders o ON o.driver_id = d.id AND ($1::uuid IS NULL OR o.origin_warehouse_id = $1)
            WHERE $1::uuid IS NULL OR o.id IS NOT NULL
            GROUP BY d.id, u.full_name, d.status, d.rating
            ORDER BY completed_orders DESC
            LIMIT 10
        `, warehouseID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var ds models.DashboardDriverStat
			if err := rows.Scan(&ds.ID, &ds.FullName, &ds.Status, &ds.Rating, &ds.CompletedOrders); err != nil {
				return err
			}
			report.Drivers = append(report.Drivers, ds)
		}
		return rows.Err()
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}
	if report.Drivers == nil {
		report.Drivers = []models.DashboardDriverStat{}
	}
	return &report, nil
}
