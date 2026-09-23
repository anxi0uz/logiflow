package tests

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/handler"
	"github.com/anxi0uz/logiflow/internal/models"
	"github.com/anxi0uz/logiflow/internal/services"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestCoreHTTPFlowSmoke(t *testing.T) {
	clientID := uuid.New()
	managerID := uuid.New()
	driverUserID := uuid.New()
	orderID := uuid.New()
	assignmentID := uuid.New()
	order := models.Order{ID: orderID, CreatedByID: &clientID, Status: models.OrderDraft}
	assignment := models.Assignment{ID: assignmentID, OrderID: orderID, DriverID: uuid.New(), VehicleID: uuid.New()}

	svc := &mockOrderService{
		createOrder: func(_ context.Context, _ api.OrderCreate, userID uuid.UUID) (*services.CreateOrderResult, error) {
			if userID != clientID {
				t.Fatalf("create actor = %s", userID)
			}
			return &services.CreateOrderResult{Order: order}, nil
		},
		updateDraftOrder: func(_ context.Context, id, userID uuid.UUID, role string, req api.OrderDraftUpdate) (*models.Order, error) {
			if id != orderID || userID != clientID || role != "client" || req.WeightKg == nil || *req.WeightKg != 200 || order.Status != models.OrderDraft {
				t.Fatalf("unexpected draft edit: %+v", req)
			}
			order.WeightKg = float64(*req.WeightKg)
			return &order, nil
		},
		listAssignments: func(_ context.Context, userID uuid.UUID, role string, _ api.ListAssignmentsParams) ([]models.Assignment, error) {
			if userID != driverUserID || role != "driver" || assignment.Status != models.AssignmentPendingAcceptance {
				t.Fatalf("unexpected driver offers")
			}
			return []models.Assignment{assignment}, nil
		},
		listOrderAssignments: func(_ context.Context, id, userID uuid.UUID, role string) ([]models.Assignment, error) {
			if id != orderID || userID != managerID || role != "manager" {
				t.Fatalf("unexpected manager history")
			}
			return []models.Assignment{assignment}, nil
		},
		submitOrder: func(_ context.Context, id uuid.UUID, _ uuid.UUID, _ string) (*models.Order, error) {
			if id != orderID || order.Status != models.OrderDraft {
				t.Fatalf("unexpected submit state: %+v", order)
			}
			order.Status = models.OrderReadyForDispatch
			return &order, nil
		},
		createAssignment: func(_ context.Context, id uuid.UUID, _ uuid.UUID, role string, req api.AssignmentCreate) (*models.Assignment, error) {
			if id != orderID || role != "manager" || req.DriverId != assignment.DriverID || req.VehicleId != assignment.VehicleID {
				t.Fatalf("unexpected assignment command: role=%s request=%+v", role, req)
			}
			assignment.Status = models.AssignmentPendingAcceptance
			return &assignment, nil
		},
		acceptAssignment: func(_ context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Assignment, error) {
			if id != assignmentID || userID != driverUserID || role != "driver" || assignment.Status != models.AssignmentPendingAcceptance {
				t.Fatalf("unexpected accept command")
			}
			assignment.Status = models.AssignmentAccepted
			order.Status = models.OrderAssigned
			return &assignment, nil
		},
		startAssignment: func(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string) (*models.Assignment, error) {
			assignment.Status = models.AssignmentActive
			order.Status = models.OrderInTransit
			return &assignment, nil
		},
		arriveAssignment: func(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string) (*models.Assignment, error) {
			order.Status = models.OrderArrived
			return &assignment, nil
		},
		completeAssignment: func(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string, req api.DeliveryComplete) (*models.Assignment, error) {
			if req.RecipientName == nil || *req.RecipientName != "Recipient" {
				t.Fatalf("delivery payload was not decoded: %+v", req)
			}
			assignment.Status = models.AssignmentCompleted
			order.Status = models.OrderCompleted
			return &assignment, nil
		},
	}

	server := newTestServer(svc)
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := r.Header.Get("X-Test-Role")
			userID := clientID
			switch role {
			case "manager":
				userID = managerID
			case "driver":
				userID = driverUserID
			}
			ctx := context.WithValue(r.Context(), handler.UserKey, &handler.Claims{ID: userID, Role: role})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	httpHandler := api.HandlerFromMux(server, router)

	from := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	to := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	steps := []struct {
		method string
		path   string
		role   string
		body   string
		want   int
	}{
		{http.MethodPost, "/api/v1/orders", "client", `{"destinationAddress":"Destination"}`, http.StatusCreated},
		{http.MethodPatch, "/api/v1/orders/" + orderID.String(), "client", `{"weightKg":200}`, http.StatusOK},
		{http.MethodPost, "/api/v1/orders/" + orderID.String() + "/submit", "client", "", http.StatusOK},
		{http.MethodPost, "/api/v1/orders/" + orderID.String() + "/assignments", "manager", `{"driverId":"` + assignment.DriverID.String() + `","vehicleId":"` + assignment.VehicleID.String() + `","plannedFrom":"` + from + `","plannedTo":"` + to + `"}`, http.StatusCreated},
		{http.MethodGet, "/api/v1/assignments", "driver", "", http.StatusOK},
		{http.MethodGet, "/api/v1/orders/" + orderID.String() + "/assignments", "manager", "", http.StatusOK},
		{http.MethodPost, "/api/v1/assignments/" + assignmentID.String() + "/accept", "driver", "", http.StatusOK},
		{http.MethodPost, "/api/v1/assignments/" + assignmentID.String() + "/start", "driver", "", http.StatusOK},
		{http.MethodPost, "/api/v1/assignments/" + assignmentID.String() + "/arrive", "driver", "", http.StatusOK},
		{http.MethodPost, "/api/v1/assignments/" + assignmentID.String() + "/complete", "driver", `{"recipientName":"Recipient","comment":"received"}`, http.StatusOK},
	}
	for _, step := range steps {
		req := httptest.NewRequest(step.method, step.path, bytes.NewBufferString(step.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-Role", step.role)
		response := httptest.NewRecorder()
		httpHandler.ServeHTTP(response, req)
		if response.Code != step.want {
			t.Fatalf("%s %s: got %d want %d: %s", step.method, step.path, response.Code, step.want, response.Body.String())
		}
	}
	if order.Status != models.OrderCompleted || assignment.Status != models.AssignmentCompleted {
		t.Fatalf("smoke flow did not complete: order=%s assignment=%s", order.Status, assignment.Status)
	}
}
