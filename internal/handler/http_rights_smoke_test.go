package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/config"
	"github.com/anxi0uz/logiflow/internal/database"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Run with isolated PostgreSQL and Redis, for example:
//
//	LOGIFLOW_TEST_DATABASE_URL=postgres://... LOGIFLOW_TEST_REDIS_ADDR=127.0.0.1:6379 \
//	  go test ./internal/handler -run TestHTTPRightsSmoke -count=1 -v
func TestHTTPRightsSmoke(t *testing.T) {
	databaseURL := os.Getenv("LOGIFLOW_TEST_DATABASE_URL")
	redisAddr := os.Getenv("LOGIFLOW_TEST_REDIS_ADDR")
	if databaseURL == "" || redisAddr == "" {
		t.Skip("LOGIFLOW_TEST_DATABASE_URL and LOGIFLOW_TEST_REDIS_ADDR are required")
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
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() { _ = redisClient.Close() })

	var cfg config.Config
	cfg.JwtOpt.Key = "http-smoke-secret"
	cfg.JwtOpt.Issuer = "logiflow-http-smoke"
	cfg.Redis.AccessTokenDur = time.Hour
	cfg.Redis.RefreshTokenDur = time.Hour
	s := NewServer(ctx, pool, redisClient, &cfg)
	mux := chi.NewRouter()
	mux.Use(s.MiddlewareRequestID)
	mux.Use(s.AuthMiddleware)
	httpServer := httptest.NewServer(api.HandlerFromMux(s, mux))
	t.Cleanup(httpServer.Close)
	client := &http.Client{Timeout: 5 * time.Second}

	users := []models.User{
		{ID: uuid.New(), Role: "admin"},
		{ID: uuid.New(), Role: "manager"},
		{ID: uuid.New(), Role: "manager"},
		{ID: uuid.New(), Role: "driver"},
	}
	tokens := make([]string, len(users))
	for i := range users {
		users[i].Email = users[i].ID.String() + "@http-smoke.test"
		users[i].Slug = "http-smoke-" + users[i].ID.String()
		users[i].FullName = "HTTP Smoke"
		users[i].PasswordHash = "test"
		users[i].CreatedAt = time.Now()
		if err := storage.Create(ctx, "users", users[i], pool); err != nil {
			t.Fatalf("seed user %d: %v", i, err)
		}
		token, err := s.generateAccessToken(&users[i], time.Hour)
		if err != nil {
			t.Fatalf("token %d: %v", i, err)
		}
		tokens[i] = token
		if err := redisClient.Set(ctx, "access_token:"+token, "valid", time.Hour).Err(); err != nil {
			t.Fatalf("store token %d: %v", i, err)
		}
	}
	warehouses := []models.Warehouse{
		{ID: uuid.New(), Name: "Own smoke warehouse", Address: "Origin", City: "Test"},
		{ID: uuid.New(), Name: "Foreign smoke warehouse", Address: "Other", City: "Test"},
	}
	for i := range warehouses {
		warehouses[i].Slug = "http-smoke-" + warehouses[i].ID.String()
		warehouses[i].CreatedAt = time.Now()
		if err := storage.Create(ctx, "warehouses", warehouses[i], pool); err != nil {
			t.Fatalf("seed warehouse: %v", err)
		}
	}
	managers := []models.Manager{
		{ID: uuid.New(), UserID: users[1].ID, WarehouseID: &warehouses[0].ID},
		{ID: uuid.New(), UserID: users[2].ID, WarehouseID: &warehouses[1].ID},
	}
	for i := range managers {
		managers[i].Slug = "http-smoke-" + managers[i].ID.String()
		if err := storage.Create(ctx, "managers", managers[i], pool); err != nil {
			t.Fatalf("seed manager: %v", err)
		}
	}
	vehicle := models.Vehicle{ID: uuid.New(), PlateNumber: "SM-" + uuid.NewString()[:8], CapacityKg: 1000, Status: "available"}
	vehicle.Slug = "http-smoke-" + vehicle.ID.String()
	if err := storage.Create(ctx, "vehicles", vehicle, pool); err != nil {
		t.Fatalf("seed vehicle: %v", err)
	}
	driver := models.Driver{ID: uuid.New(), UserID: users[3].ID, LicenseNumber: "HTTP-SMOKE", LicenseExpiry: time.Now().AddDate(1, 0, 0), Status: "available"}
	driver.Slug = "http-smoke-" + driver.ID.String()
	if err := storage.Create(ctx, "drivers", driver, pool); err != nil {
		t.Fatalf("seed driver: %v", err)
	}
	orders := []models.Order{
		{ID: uuid.New(), OriginWarehouseID: &warehouses[0].ID, OriginAddress: "Origin", DestinationAddress: "Destination", Status: models.OrderArrived, CreatedAt: time.Now()},
		{ID: uuid.New(), OriginWarehouseID: &warehouses[0].ID, OriginAddress: "Origin", DestinationAddress: "Destination", Status: models.OrderReadyForDispatch, CreatedAt: time.Now()},
	}
	for _, order := range orders {
		if err := storage.Create(ctx, "orders", order, pool); err != nil {
			t.Fatalf("seed order: %v", err)
		}
	}
	now := time.Now()
	acceptedAt, startedAt, rejectedAt := now.Add(-time.Hour), now.Add(-30*time.Minute), now.Add(-time.Minute)
	assignments := []models.Assignment{
		{
			ID: uuid.New(), OrderID: orders[0].ID, DriverID: driver.ID, VehicleID: vehicle.ID,
			Status: models.AssignmentActive, PlannedFrom: now.Add(-time.Hour), PlannedTo: now.Add(time.Hour),
			CreatedByUserID: users[0].ID, Source: "manual", AssignedAt: now.Add(-2 * time.Hour),
			OfferExpiresAt: now.Add(-90 * time.Minute), AcceptedAt: &acceptedAt, StartedAt: &startedAt,
		},
		{
			ID: uuid.New(), OrderID: orders[1].ID, DriverID: driver.ID, VehicleID: vehicle.ID,
			Status: models.AssignmentRejected, PlannedFrom: now.Add(time.Hour), PlannedTo: now.Add(2 * time.Hour),
			CreatedByUserID: users[0].ID, Source: "manual", AssignedAt: now.Add(-2 * time.Hour),
			OfferExpiresAt: now.Add(-time.Hour), RejectedAt: &rejectedAt,
		},
	}
	for _, assignment := range assignments {
		if err := storage.Create(ctx, "assignments", assignment, pool); err != nil {
			t.Fatalf("seed assignment: %v", err)
		}
	}
	if err := storage.Create(ctx, "routes", models.Route{
		ID: uuid.New(), OrderID: orders[1].ID, Coordinates: json.RawMessage(`[[30,60],[31,61]]`), Status: "pending",
	}, pool); err != nil {
		t.Fatalf("seed route: %v", err)
	}
	var adminVehicleID uuid.UUID
	t.Cleanup(func() {
		for _, order := range orders {
			if _, err := pool.Exec(ctx, "DELETE FROM orders WHERE id = $1", order.ID); err != nil {
				t.Errorf("clean order: %v", err)
			}
		}
		if _, err := pool.Exec(ctx, "DELETE FROM drivers WHERE id = $1", driver.ID); err != nil {
			t.Errorf("clean driver: %v", err)
		}
		if adminVehicleID != uuid.Nil {
			if _, err := pool.Exec(ctx, "DELETE FROM vehicles WHERE id = $1", adminVehicleID); err != nil {
				t.Errorf("clean admin vehicle: %v", err)
			}
		}
		if _, err := pool.Exec(ctx, "DELETE FROM vehicles WHERE id = $1", vehicle.ID); err != nil {
			t.Errorf("clean vehicles: %v", err)
		}
		for _, manager := range managers {
			if _, err := pool.Exec(ctx, "DELETE FROM managers WHERE id = $1", manager.ID); err != nil {
				t.Errorf("clean manager: %v", err)
			}
		}
		for _, warehouse := range warehouses {
			if _, err := pool.Exec(ctx, "DELETE FROM warehouses WHERE id = $1", warehouse.ID); err != nil {
				t.Errorf("clean warehouse: %v", err)
			}
		}
		for i, user := range users {
			_ = redisClient.Del(ctx, "access_token:"+tokens[i]).Err()
			if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
				t.Errorf("clean user: %v", err)
			}
		}
	})

	request := func(method, path, token, body string, want int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, httpServer.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer res.Body.Close() //nolint:errcheck
		payload, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, res.StatusCode, want, payload)
		}
		return payload
	}

	t.Run("foreign manager cannot complete delivery", func(t *testing.T) {
		path := "/api/v1/assignments/" + assignments[0].ID.String() + "/complete"
		request(http.MethodPost, path, tokens[2], `{"recipientName":"Recipient"}`, http.StatusForbidden)
		var orderStatus, assignmentStatus string
		if err := pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", orders[0].ID).Scan(&orderStatus); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, "SELECT status FROM assignments WHERE id = $1", assignments[0].ID).Scan(&assignmentStatus); err != nil {
			t.Fatal(err)
		}
		if orderStatus != models.OrderArrived || assignmentStatus != models.AssignmentActive {
			t.Fatalf("forbidden call changed state: order=%s assignment=%s", orderStatus, assignmentStatus)
		}
		request(http.MethodPost, path, tokens[1], `{"recipientName":"Recipient","comment":"Received"}`, http.StatusOK)
		var recipient, comment string
		if err := pool.QueryRow(ctx, "SELECT status, recipient_name, delivery_comment FROM assignments WHERE id = $1", assignments[0].ID).Scan(&assignmentStatus, &recipient, &comment); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, "SELECT status FROM orders WHERE id = $1", orders[0].ID).Scan(&orderStatus); err != nil {
			t.Fatal(err)
		}
		if orderStatus != models.OrderCompleted || assignmentStatus != models.AssignmentCompleted || recipient != "Recipient" || comment != "Received" {
			t.Fatalf("delivery not confirmed: order=%s assignment=%s recipient=%q comment=%q", orderStatus, assignmentStatus, recipient, comment)
		}
	})

	t.Run("global fleet is admin-only", func(t *testing.T) {
		plate := "HS-" + uuid.NewString()[:8]
		body := fmt.Sprintf(`{"plateNumber":%q,"capacityKg":1000}`, plate)
		request(http.MethodPost, "/vehicles", tokens[1], body, http.StatusForbidden)
		request(http.MethodPost, "/vehicles", tokens[0], body, http.StatusCreated)
		var count int
		if err := pool.QueryRow(ctx, "SELECT id, count(*) OVER () FROM vehicles WHERE plate_number = $1", plate).Scan(&adminVehicleID, &count); err != nil || count != 1 {
			t.Fatalf("admin vehicle create: count=%d err=%v", count, err)
		}
	})

	t.Run("rejected driver loses route access", func(t *testing.T) {
		path := "/orders/" + orders[1].ID.String() + "/route"
		request(http.MethodGet, path, tokens[3], "", http.StatusForbidden)
		request(http.MethodGet, path, tokens[1], "", http.StatusOK)
		wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + path + "/ws"
		header := http.Header{"Authorization": {"Bearer " + tokens[3]}}
		conn, response, err := websocket.DefaultDialer.Dial(wsURL, header)
		if conn != nil {
			_ = conn.Close()
		}
		if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("rejected driver websocket: response=%v err=%v", response, err)
		}
		_ = response.Body.Close()
	})

	t.Run("deleted manager token is rejected", func(t *testing.T) {
		request(http.MethodGet, "/me", tokens[2], "", http.StatusOK)
		request(http.MethodDelete, "/managers/"+managers[1].Slug, tokens[0], "", http.StatusOK)
		request(http.MethodGet, "/me", tokens[2], "", http.StatusUnauthorized)
		request(http.MethodPost, "/auth/login", "", fmt.Sprintf(`{"email":%q,"password":"test"}`, users[2].Email), http.StatusUnauthorized)
	})
}
