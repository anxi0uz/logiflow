package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anxi0uz/logiflow/internal/models"
	"github.com/anxi0uz/logiflow/internal/services"
	"github.com/google/uuid"
)

func TestMasterDataMutationsRequireAdmin(t *testing.T) {
	server := &Server{}
	tests := []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
	}{
		{"create manager", server.CreateManager},
		{"delete manager", func(w http.ResponseWriter, r *http.Request) { server.DeleteManager(w, r, "manager") }},
		{"update driver", func(w http.ResponseWriter, r *http.Request) { server.UpdateDriver(w, r, "driver") }},
		{"delete driver", func(w http.ResponseWriter, r *http.Request) { server.DeleteDriver(w, r, "driver") }},
		{"create warehouse", server.CreateWarehouse},
		{"update warehouse", func(w http.ResponseWriter, r *http.Request) { server.UpdateWarehouse(w, r, "warehouse") }},
		{"delete warehouse", func(w http.ResponseWriter, r *http.Request) { server.DeleteWarehouse(w, r, "warehouse") }},
	}
	for _, role := range []string{"client", "driver", "manager"} {
		for _, tt := range tests {
			t.Run(role+"/"+tt.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
				req = req.WithContext(context.WithValue(req.Context(), UserKey, &Claims{ID: uuid.New(), Role: role}))
				res := httptest.NewRecorder()
				tt.call(res, req)
				if res.Code != http.StatusForbidden {
					t.Fatalf("got %d, want 403", res.Code)
				}
			})
		}
	}
}

func TestManagerCreationRequiresWarehouse(t *testing.T) {
	server := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/managers", strings.NewReader(`{"email":"manager@example.com","fullName":"Manager","password":"password123"}`))
	req = req.WithContext(context.WithValue(req.Context(), UserKey, &Claims{ID: uuid.New(), Role: "admin"}))
	res := httptest.NewRecorder()
	server.CreateManager(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", res.Code)
	}
}

func TestGlobalFleetMutationsRequireAdmin(t *testing.T) {
	server := &Server{}
	tests := []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
	}{
		{"create vehicle", server.CreateVehicle},
		{"update vehicle", func(w http.ResponseWriter, r *http.Request) { server.UpdateVehicle(w, r, "truck") }},
		{"delete vehicle", func(w http.ResponseWriter, r *http.Request) { server.DeleteVehicle(w, r, "truck") }},
		{"create vehicle document", func(w http.ResponseWriter, r *http.Request) { server.CreateVehicleDocument(w, r, "truck") }},
		{"update vehicle document", func(w http.ResponseWriter, r *http.Request) { server.UpdateVehicleDocument(w, r, "truck", uuid.New()) }},
	}
	for _, role := range []string{"client", "driver", "manager"} {
		for _, tt := range tests {
			t.Run(role+"/"+tt.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "/vehicles", strings.NewReader(`{}`))
				req = req.WithContext(context.WithValue(req.Context(), UserKey, &Claims{ID: uuid.New(), Role: role}))
				res := httptest.NewRecorder()
				tt.call(res, req)
				if res.Code != http.StatusForbidden {
					t.Fatalf("got %d, want 403", res.Code)
				}
			})
		}
	}
}

type denyingOrderService struct{ services.OrderServicer }

func (denyingOrderService) GetOrder(context.Context, uuid.UUID, uuid.UUID, string) (*models.Order, error) {
	return nil, services.ErrForbidden
}

func TestRouteEndpointsCheckOrderAccessBeforeReadingRouteOrUpgrading(t *testing.T) {
	server := &Server{OrderSerice: denyingOrderService{}}
	orderID := uuid.New()
	for _, tt := range []struct {
		name string
		call func(http.ResponseWriter, *http.Request, uuid.UUID)
	}{
		{"http", server.GetRoute},
		{"websocket", server.RouteWebSocket},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/orders/"+orderID.String()+"/route", nil)
			req = req.WithContext(context.WithValue(req.Context(), UserKey, &Claims{ID: uuid.New(), Role: "client"}))
			res := httptest.NewRecorder()
			tt.call(res, req, orderID)
			if res.Code != http.StatusForbidden {
				t.Fatalf("got %d, want 403", res.Code)
			}
		})
	}
}
