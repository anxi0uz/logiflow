package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	api "github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/config"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/anxi0uz/logiflow/pkg/geocode"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
)

type OrderService struct {
	db     *pgxpool.Pool
	config config.Config
}
type OrderServicer interface {
	CreateOrder(ctx context.Context, req api.OrderCreate, userID uuid.UUID) (*CreateOrderResult, error)
	ListOrders(ctx context.Context, userID uuid.UUID, role string, params api.ListOrdersParams) ([]models.Order, error)
	GetOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Order, error)
	CancelOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) error
	UpdateOrderStatus(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.OrderStatusUpdate) (*models.Order, error)
	GetOrdersReport(ctx context.Context, role string, params api.GetOrdersReportParams) ([]models.Order, error)
}

var ErrForbidden = errors.New("forbidden")
var ErrCannotCancel = errors.New("cant cancel")

type CreateOrderResult struct {
	Order models.Order
	Route models.Route
}

func NewOrderService(db *pgxpool.Pool, cfg config.Config) *OrderService {
	return &OrderService{db: db, config: cfg}
}

func (s *OrderService) CreateOrder(ctx context.Context, req api.OrderCreate, userID uuid.UUID) (*CreateOrderResult, error) {
	var (
		originLat, originLon float64
		destLat, destLon     float64
	)
	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		if req.OriginWarehouseId != nil {
			wh, err := storage.GetOne[models.Warehouse](gctx, s.db, "warehouses", func(sb *sqlbuilder.SelectBuilder) {
				sb.Where(sb.Equal("id", req.OriginWarehouseId))
			})
			if err != nil {
				return err
			}
			originLat = wh.Latitude
			originLon = wh.Longitude
			return nil
		}

		var err error
		originLat, originLon, err = geocode.Geocode(gctx, *req.OriginAddress)
		return err
	})
	g.Go(func() error {
		if req.DestinationWarehouseId != nil {
			wh, err := storage.GetOne[models.Warehouse](gctx, s.db, "warehouses", func(sb *sqlbuilder.SelectBuilder) {
				sb.Where(sb.Equal("id", req.DestinationWarehouseId))
			})
			if err != nil {
				return err
			}
			destLat = wh.Latitude
			destLon = wh.Longitude
			return nil
		}
		var err error
		destLat, destLon, err = geocode.Geocode(ctx, req.DestinationAddress)
		return err
	})

	if err := g.Wait(); err != nil {
		slog.ErrorContext(ctx, "error while getting coordinates", slog.String("error", err.Error()))
		return nil, fmt.Errorf("error while getting coordinates: %w", err)
	}
	osrmURL := fmt.Sprintf(
		"http://router.project-osrm.org/route/v1/driving/%f,%f;%f,%f?overview=full&geometries=geojson",
		originLon, originLat, destLon, destLat,
	)
	osrmReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, osrmURL, nil)
	osrmReq.Header.Set("User-Agent", "logiflow/1.0")

	osrmResp, err := http.DefaultClient.Do(osrmReq)
	if err != nil {
		return nil, fmt.Errorf("osrm request: %w", err)
	}
	defer osrmResp.Body.Close()

	var osrmResult geocode.OsrmResult
	if err := json.NewDecoder(osrmResp.Body).Decode(&osrmResult); err != nil || len(osrmResult.Routes) == 0 {
		return nil, fmt.Errorf("osrm: no route found")
	}
	route := osrmResult.Routes[0]
	distanceKm := route.Distance / 1000

	var weightKg, volumeM3 float64
	if req.WeightKg != nil {
		weightKg = float64(*req.WeightKg)
	}
	if req.VolumeM3 != nil {
		volumeM3 = float64(*req.VolumeM3)
	}
	price := s.config.Pricing.BaseFee +
		distanceKm*s.config.Pricing.PerKm +
		weightKg*s.config.Pricing.PerKg +
		volumeM3*s.config.Pricing.PerM3

	orderID := uuid.New()
	order := models.Order{
		ID:                 orderID,
		CreatedByID:        &userID,
		DestinationAddress: req.DestinationAddress,
		Status:             "pending",
		TotalPrice:         price,
		CreatedAt:          time.Now(),
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

	coordsJSON, err := json.Marshal(route.Geometry.Coordinates)
	if err != nil {
		return nil, fmt.Errorf("marshal coordinates: %w", err)
	}
	routeModel := models.Route{
		ID:          uuid.New(),
		OrderID:     orderID,
		Coordinates: coordsJSON,
		DurationSec: int(route.Duration),
		Status:      "pending",
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

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
	var driverID *uuid.UUID
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
		default:
			if params.Status != nil {
				sb.Where(sb.EQ("status", params.Status))
			}
			if params.DriverId != nil {
				sb.Where(sb.EQ("driver_id", params.DriverId))
			}
		}
	})

	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	return orders, nil
}
func (s *OrderService) GetOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Order, error) {
	order, err := storage.GetOne[models.Order](ctx, s.db, "orders", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", id))
	})

	if err != nil {
		return nil, err
	}

	if role == "client" && (order.CreatedByID == nil || *order.CreatedByID != userID) {
		return nil, ErrForbidden
	}

	return order, nil
}
func (s *OrderService) CancelOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) error {
	order, err := storage.GetOne[models.Order](ctx, s.db, "orders", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", id))
	})
	if err != nil {
		return err
	}

	if role == "client" && (order.CreatedByID == nil || *order.CreatedByID != userID) {
		return ErrForbidden
	}

	if order.Status != "pending" {
		return ErrCannotCancel
	}

	order.Status = "cancelled"
	if err := storage.Update(ctx, "orders", order, s.db, func(sb *sqlbuilder.UpdateBuilder) {
		sb.Where(sb.EQ("id", id))
	}); err != nil {
		return err
	}
	return nil
}
func (s *OrderService) UpdateOrderStatus(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.OrderStatusUpdate) (*models.Order, error) {
	if role == "client" {
		return nil, ErrForbidden
	}

	order, err := storage.GetOne[models.Order](ctx, s.db, "orders", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", id))
	})
	if err != nil {
		return nil, err
	}
	if role == "driver" {
		driver, err := storage.GetOne[models.Driver](ctx, s.db, "drivers", func(sb *sqlbuilder.SelectBuilder) {
			sb.Where(sb.EQ("user_id", userID))
		})
		if err != nil {
			return nil, fmt.Errorf("get driver: %w", err)
		}
		if order.DriverID == nil || *order.DriverID != driver.ID {
			return nil, ErrForbidden
		}
		if req.Status != api.OrderStatusUpdateStatusInTransit && req.Status != api.OrderStatusUpdateStatusDelivered {
			return nil, ErrForbidden
		}
	}
	if !req.Status.Valid() {
		return nil, fmt.Errorf("invalid status: %s", req.Status)
	}
	now := time.Now()
	order.Status = string(req.Status)
	if req.Status == api.OrderStatusUpdateStatusAssigned {
		if req.DriverId == nil {
			return nil, fmt.Errorf("driverId required when assigned")
		}
		order.DriverID = req.DriverId
		order.AssignedAt = &now
	}
	if req.Status == api.OrderStatusUpdateStatusDelivered {
		order.DeliveredAt = &now
	}
	if err := storage.Update(ctx, "orders", *order, s.db, func(sb *sqlbuilder.UpdateBuilder) {
		sb.Where(sb.EQ("id", id))
	}); err != nil {
		return nil, fmt.Errorf("update order: %w", err)
	}
	return order, nil
}
func (s *OrderService) GetOrdersReport(ctx context.Context, role string, params api.GetOrdersReportParams) ([]models.Order, error) {
	if role != "manager" {
		return nil, ErrForbidden
	}
	orders, err := storage.GetAll[models.Order](ctx, "orders", s.db, func(sb *sqlbuilder.SelectBuilder) {
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
