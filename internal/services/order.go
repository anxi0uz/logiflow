package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
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
		destLat, originLon, err = geocode.Geocode(ctx, req.DestinationAddress)
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
