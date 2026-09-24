package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/config"
	"github.com/anxi0uz/logiflow/internal/database"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/anxi0uz/logiflow/pkg/routing"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5/pgxpool"
)

type testRoutePlanner struct {
	estimate    routing.Estimate
	err         error
	origin      routing.Endpoint
	destination routing.Endpoint
}

func (p *testRoutePlanner) Plan(_ context.Context, origin, destination routing.Endpoint) (routing.Estimate, error) {
	p.origin, p.destination = origin, destination
	return p.estimate, p.err
}

func TestCreateOrderSeparatesRouteEstimatePriceAndPersistence(t *testing.T) {
	databaseURL := os.Getenv("LOGIFLOW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LOGIFLOW_TEST_DATABASE_URL is not set")
	}
	t.Chdir("../..")
	ctx := context.Background()
	if err := database.RunMigrations(ctx, databaseURL); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	user := models.User{ID: uuid.New(), PasswordHash: "test", FullName: "Route Test", Role: "client", CreatedAt: time.Now()}
	user.Email = user.ID.String() + "@routing.test"
	user.Slug = "routing-" + user.ID.String()
	if err := storage.Create(ctx, "users", user, pool); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	originLat, originLon, destinationLat, destinationLon := 60.0, 30.0, 61.0, 31.0
	warehouses := []models.Warehouse{
		{ID: uuid.New(), Name: "Origin", Address: "Origin address", City: "Test", Latitude: &originLat, Longitude: &originLon, CreatedAt: time.Now()},
		{ID: uuid.New(), Name: "Destination", Address: "Destination address", City: "Test", Latitude: &destinationLat, Longitude: &destinationLon, CreatedAt: time.Now()},
	}
	for i := range warehouses {
		warehouses[i].Slug = "routing-" + warehouses[i].ID.String()
		if err := storage.Create(ctx, "warehouses", warehouses[i], pool); err != nil {
			t.Fatalf("seed warehouse: %v", err)
		}
	}
	var orderID uuid.UUID
	t.Cleanup(func() {
		if orderID != uuid.Nil {
			_ = storage.Delete[models.Order](ctx, "orders", pool, func(db *sqlbuilder.DeleteBuilder) { db.Where(db.EQ("id", orderID)) })
		}
		for _, warehouse := range warehouses {
			_ = storage.Delete[models.Warehouse](ctx, "warehouses", pool, func(db *sqlbuilder.DeleteBuilder) { db.Where(db.EQ("id", warehouse.ID)) })
		}
		_ = storage.Delete[models.User](ctx, "users", pool, func(db *sqlbuilder.DeleteBuilder) { db.Where(db.EQ("id", user.ID)) })
	})
	var cfg config.Config
	cfg.Pricing.BaseFee = 100
	cfg.Pricing.PerKm = 2
	cfg.Pricing.PerKg = 3
	cfg.Pricing.PerM3 = 4
	planner := &testRoutePlanner{estimate: routing.Estimate{
		Coordinates: [][]float64{{30, 60}, {31, 61}}, DistanceKm: 25, DurationSec: 900,
	}}
	service := NewOrderService(pool, cfg)
	service.routePlanner = planner
	incompleteWarehouse := models.Warehouse{ID: uuid.New(), Name: "No coordinates", Address: "Unknown", City: "Test", Slug: "routing-incomplete-" + uuid.NewString(), CreatedAt: time.Now()}
	if err := storage.Create(ctx, "warehouses", incompleteWarehouse, pool); err != nil {
		t.Fatalf("seed warehouse without coordinates: %v", err)
	}
	t.Cleanup(func() {
		_ = storage.Delete[models.Warehouse](ctx, "warehouses", pool, func(db *sqlbuilder.DeleteBuilder) { db.Where(db.EQ("id", incompleteWarehouse.ID)) })
	})
	if _, err := service.CreateOrder(ctx, api.OrderCreate{OriginWarehouseId: &incompleteWarehouse.ID, DestinationAddress: "Destination"}, user.ID, "client"); !errors.Is(err, ErrInvalidOrderInput) {
		t.Fatalf("warehouse without coordinates should be rejected: %v", err)
	}
	weight, volume := float32(10), float32(2)
	result, err := service.CreateOrder(ctx, api.OrderCreate{
		OriginWarehouseId: &warehouses[0].ID, DestinationWarehouseId: &warehouses[1].ID,
		DestinationAddress: "Destination address", WeightKg: &weight, VolumeM3: &volume,
	}, user.ID, "client")
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	orderID = result.Order.ID
	if planner.origin.Coordinates == nil || *planner.origin.Coordinates != (routing.Coordinates{Latitude: 60, Longitude: 30}) ||
		planner.destination.Coordinates == nil || *planner.destination.Coordinates != (routing.Coordinates{Latitude: 61, Longitude: 31}) {
		t.Fatalf("warehouse coordinates not passed to route planner: %+v %+v", planner.origin, planner.destination)
	}
	if result.Order.TotalPrice == nil || *result.Order.TotalPrice != 188 || result.Route.DistanceKm == nil || *result.Route.DistanceKm != 25 || result.Route.DurationSec == nil || *result.Route.DurationSec != 900 {
		t.Fatalf("wrong quote: order=%+v route=%+v", result.Order, result.Route)
	}
	var coordinates [][]float64
	if err := json.Unmarshal(result.Route.Coordinates, &coordinates); err != nil || len(coordinates) != 2 {
		t.Fatalf("route coordinates: %v %+v", err, coordinates)
	}
	storedOrder, err := storage.GetOne[models.Order](ctx, pool, "orders", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("id", orderID)) })
	if err != nil || storedOrder.TotalPrice == nil || *storedOrder.TotalPrice != 188 {
		t.Fatalf("order not persisted: %+v err=%v", storedOrder, err)
	}
	storedRoute, err := storage.GetOne[models.Route](ctx, pool, "routes", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("order_id", orderID)) })
	if err != nil || storedRoute.DistanceKm == nil || *storedRoute.DistanceKm != 25 {
		t.Fatalf("route not persisted: %+v err=%v", storedRoute, err)
	}
	newWeight := float32(20)
	updated, err := service.UpdateDraftOrder(ctx, orderID, user.ID, "client", api.OrderDraftUpdate{WeightKg: &newWeight})
	if err != nil || updated.TotalPrice == nil || *updated.TotalPrice != 218 {
		t.Fatalf("draft repricing: %+v err=%v", updated, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE routes SET distance_km = NULL WHERE order_id = $1`, orderID); err != nil {
		t.Fatalf("clear route distance: %v", err)
	}
	if _, err := service.UpdateDraftOrder(ctx, orderID, user.ID, "client", api.OrderDraftUpdate{WeightKg: &newWeight}); !errors.Is(err, ErrInvalidOrderInput) {
		t.Fatalf("unknown route distance must not be priced as zero: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE routes SET distance_km = 25 WHERE order_id = $1`, orderID); err != nil {
		t.Fatalf("restore route distance: %v", err)
	}
	from, to := time.Now().Add(time.Hour), time.Now().Add(2*time.Hour)
	if _, err := service.UpdateDraftOrder(ctx, orderID, user.ID, "client", api.OrderDraftUpdate{PickupFrom: &from, PickupTo: &to}); err != nil {
		t.Fatalf("set pickup window: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET total_price = NULL WHERE id = $1`, orderID); err != nil {
		t.Fatalf("clear order price: %v", err)
	}
	if _, err := service.SubmitOrder(ctx, orderID, user.ID, "client"); !errors.Is(err, ErrInvalidOrderInput) {
		t.Fatalf("order with unknown price must not be submitted: %v", err)
	}
	planner.err = errors.New("route unavailable")
	before := orderID
	_, err = service.CreateOrder(ctx, api.OrderCreate{
		OriginWarehouseId: &warehouses[0].ID, DestinationAddress: "Destination address",
	}, user.ID, "client")
	if !errors.Is(err, planner.err) {
		t.Fatalf("route error not returned: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM orders WHERE created_by_id = $1", user.ID).Scan(&count); err != nil || count != 1 || orderID != before {
		t.Fatalf("failed route created order: count=%d err=%v", count, err)
	}
}
