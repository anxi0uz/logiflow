package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/config"
	"github.com/anxi0uz/logiflow/internal/database"
	"github.com/anxi0uz/logiflow/internal/models"
	"github.com/anxi0uz/logiflow/internal/services"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRouteAccessAndDraftWebSocketDoesNotTrack(t *testing.T) {
	databaseURL := os.Getenv("LOGIFLOW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LOGIFLOW_TEST_DATABASE_URL is not set")
	}
	t.Chdir("../..")
	ctx := context.Background()
	if err := database.RunMigrations(ctx, databaseURL); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	ownerID := uuid.New()
	owner := models.User{ID: ownerID, Email: ownerID.String() + "@route.test", Slug: "route-" + ownerID.String(), PasswordHash: "test", FullName: "Route Owner", Role: "client", CreatedAt: time.Now()}
	if err := storage.Create(ctx, "users", owner, pool); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	t.Cleanup(func() {
		_ = storage.Delete[models.User](ctx, "users", pool, func(db *sqlbuilder.DeleteBuilder) { db.Where(db.EQ("id", ownerID)) })
	})
	order := models.Order{ID: uuid.New(), CreatedByID: &ownerID, OriginAddress: "Origin", DestinationAddress: "Destination", Status: models.OrderDraft, CreatedAt: time.Now()}
	if err := storage.Create(ctx, "orders", order, pool); err != nil {
		t.Fatalf("create order: %v", err)
	}
	t.Cleanup(func() {
		_ = storage.Delete[models.Order](ctx, "orders", pool, func(db *sqlbuilder.DeleteBuilder) { db.Where(db.EQ("id", order.ID)) })
	})
	route := models.Route{ID: uuid.New(), OrderID: order.ID, Coordinates: json.RawMessage(`[[30,60],[31,61]]`), Status: "pending"}
	if err := storage.Create(ctx, "routes", route, pool); err != nil {
		t.Fatalf("create route: %v", err)
	}
	server := &Server{DB: pool, OrderSerice: services.NewOrderService(pool, config.Config{}), Hub: NewHub()}
	for _, tc := range []struct {
		name  string
		actor uuid.UUID
		call  func(http.ResponseWriter, *http.Request, uuid.UUID)
		want  int
	}{
		{"owner route", ownerID, server.GetRoute, http.StatusOK},
		{"other client route", uuid.New(), server.GetRoute, http.StatusForbidden},
		{"other client websocket", uuid.New(), server.RouteWebSocket, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/orders/"+order.ID.String()+"/route", nil)
			req = req.WithContext(context.WithValue(req.Context(), UserKey, &Claims{ID: tc.actor, Role: "client"}))
			res := httptest.NewRecorder()
			tc.call(res, req, order.ID)
			if res.Code != tc.want {
				t.Fatalf("got %d, want %d: %s", res.Code, tc.want, res.Body.String())
			}
		})
	}

	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), UserKey, &Claims{ID: ownerID, Role: "client"}))
		server.RouteWebSocket(w, r, order.ID)
	}))
	defer wsServer.Close()
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
	if err != nil {
		t.Fatalf("owner websocket: status=%v err=%v", response, err)
	}
	defer conn.Close() //nolint:errcheck
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read initial route position: %v", err)
	}
	server.Hub.mu.RLock()
	trackers := len(server.Hub.trackers)
	server.Hub.mu.RUnlock()
	if trackers != 0 {
		t.Fatalf("draft websocket started %d trackers", trackers)
	}
	storedRoute, err := storage.GetOne[models.Route](ctx, pool, "routes", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("order_id", order.ID)) })
	if err != nil || storedRoute.CurrentIndex != 0 {
		t.Fatalf("draft route moved: %+v err=%v", storedRoute, err)
	}
}
