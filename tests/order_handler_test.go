package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/handler"
	"github.com/anxi0uz/logiflow/internal/models"
	"github.com/anxi0uz/logiflow/internal/services"
	"github.com/google/uuid"
)

// --- Mock ---

type mockOrderService struct {
	createOrder        func(ctx context.Context, req api.OrderCreate, userID uuid.UUID) (*services.CreateOrderResult, error)
	listOrders         func(ctx context.Context, userID uuid.UUID, role string, params api.ListOrdersParams) ([]models.Order, error)
	getOrder           func(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Order, error)
	submitOrder        func(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Order, error)
	cancelOrder        func(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.OrderCancel) (*models.Order, error)
	createAssignment   func(ctx context.Context, orderID uuid.UUID, userID uuid.UUID, role string, req api.AssignmentCreate) (*models.Assignment, error)
	acceptAssignment   func(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Assignment, error)
	rejectAssignment   func(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.AssignmentReject) (*models.Assignment, error)
	startAssignment    func(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Assignment, error)
	arriveAssignment   func(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Assignment, error)
	completeAssignment func(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.DeliveryComplete) (*models.Assignment, error)
	getOrdersReport    func(ctx context.Context, role string, params api.GetOrdersReportParams) ([]models.Order, error)
	getDashboard       func(ctx context.Context, role string) (*models.DashboardReport, error)
}

func (m *mockOrderService) CreateOrder(ctx context.Context, req api.OrderCreate, userID uuid.UUID) (*services.CreateOrderResult, error) {
	return m.createOrder(ctx, req, userID)
}
func (m *mockOrderService) ListOrders(ctx context.Context, userID uuid.UUID, role string, params api.ListOrdersParams) ([]models.Order, error) {
	return m.listOrders(ctx, userID, role, params)
}
func (m *mockOrderService) GetOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Order, error) {
	return m.getOrder(ctx, id, userID, role)
}
func (m *mockOrderService) SubmitOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Order, error) {
	return m.submitOrder(ctx, id, userID, role)
}
func (m *mockOrderService) CancelOrder(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.OrderCancel) (*models.Order, error) {
	return m.cancelOrder(ctx, id, userID, role, req)
}
func (m *mockOrderService) CreateAssignment(ctx context.Context, orderID uuid.UUID, userID uuid.UUID, role string, req api.AssignmentCreate) (*models.Assignment, error) {
	return m.createAssignment(ctx, orderID, userID, role, req)
}
func (m *mockOrderService) AcceptAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Assignment, error) {
	return m.acceptAssignment(ctx, id, userID, role)
}
func (m *mockOrderService) RejectAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.AssignmentReject) (*models.Assignment, error) {
	return m.rejectAssignment(ctx, id, userID, role, req)
}
func (m *mockOrderService) StartAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Assignment, error) {
	return m.startAssignment(ctx, id, userID, role)
}
func (m *mockOrderService) ArriveAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string) (*models.Assignment, error) {
	return m.arriveAssignment(ctx, id, userID, role)
}
func (m *mockOrderService) CompleteAssignment(ctx context.Context, id uuid.UUID, userID uuid.UUID, role string, req api.DeliveryComplete) (*models.Assignment, error) {
	return m.completeAssignment(ctx, id, userID, role, req)
}
func (m *mockOrderService) GetOrdersReport(ctx context.Context, role string, params api.GetOrdersReportParams) ([]models.Order, error) {
	return m.getOrdersReport(ctx, role, params)
}
func (m *mockOrderService) GetDashboard(ctx context.Context, role string) (*models.DashboardReport, error) {
	return m.getDashboard(ctx, role)
}

// --- Helpers ---

func newTestServer(svc services.OrderServicer) *handler.Server {
	return &handler.Server{
		OrderSerice: svc,
	}
}

func withClaims(r *http.Request, id uuid.UUID, role string) *http.Request {
	claims := &handler.Claims{ID: id, Role: role}
	ctx := context.WithValue(r.Context(), handler.UserKey, claims)
	return r.WithContext(ctx)
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return bytes.NewBuffer(b)
}

// --- CreateOrder ---

func TestCreateOrder_Success(t *testing.T) {
	orderID := uuid.New()
	svc := &mockOrderService{
		createOrder: func(_ context.Context, _ api.OrderCreate, _ uuid.UUID) (*services.CreateOrderResult, error) {
			return &services.CreateOrderResult{
				Order: models.Order{ID: orderID, Status: models.OrderDraft},
				Route: models.Route{ID: uuid.New(), OrderID: orderID},
			}, nil
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/orders", jsonBody(t, api.OrderCreate{DestinationAddress: "Moscow"}))
	r = withClaims(r, uuid.New(), "client")
	w := httptest.NewRecorder()

	s.CreateOrder(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}
}

func TestCreateOrder_InvalidBody(t *testing.T) {
	s := newTestServer(&mockOrderService{})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBufferString("not json"))
	r = withClaims(r, uuid.New(), "client")
	w := httptest.NewRecorder()

	s.CreateOrder(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCreateOrder_ServiceError(t *testing.T) {
	svc := &mockOrderService{
		createOrder: func(_ context.Context, _ api.OrderCreate, _ uuid.UUID) (*services.CreateOrderResult, error) {
			return nil, errors.New("geocode failed")
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/orders", jsonBody(t, api.OrderCreate{DestinationAddress: "Moscow"}))
	r = withClaims(r, uuid.New(), "client")
	w := httptest.NewRecorder()

	s.CreateOrder(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// --- ListOrders ---

func TestListOrders_Success(t *testing.T) {
	svc := &mockOrderService{
		listOrders: func(_ context.Context, _ uuid.UUID, _ string, _ api.ListOrdersParams) ([]models.Order, error) {
			return []models.Order{{ID: uuid.New(), Status: models.OrderDraft}}, nil
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	r = withClaims(r, uuid.New(), "client")
	w := httptest.NewRecorder()

	s.ListOrders(w, r, api.ListOrdersParams{})

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestListOrders_ServiceError(t *testing.T) {
	svc := &mockOrderService{
		listOrders: func(_ context.Context, _ uuid.UUID, _ string, _ api.ListOrdersParams) ([]models.Order, error) {
			return nil, errors.New("db error")
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	r = withClaims(r, uuid.New(), "manager")
	w := httptest.NewRecorder()

	s.ListOrders(w, r, api.ListOrdersParams{})

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// --- GetOrder ---

func TestGetOrder_Success(t *testing.T) {
	orderID := uuid.New()
	svc := &mockOrderService{
		getOrder: func(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string) (*models.Order, error) {
			return &models.Order{ID: orderID, Status: models.OrderDraft}, nil
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderID.String(), nil)
	r = withClaims(r, uuid.New(), "client")
	w := httptest.NewRecorder()

	s.GetOrder(w, r, orderID)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestGetOrder_Forbidden(t *testing.T) {
	orderID := uuid.New()
	svc := &mockOrderService{
		getOrder: func(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string) (*models.Order, error) {
			return nil, services.ErrForbidden
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderID.String(), nil)
	r = withClaims(r, uuid.New(), "client")
	w := httptest.NewRecorder()

	s.GetOrder(w, r, orderID)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestGetOrder_ServiceError(t *testing.T) {
	orderID := uuid.New()
	svc := &mockOrderService{
		getOrder: func(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string) (*models.Order, error) {
			return nil, errors.New("db error")
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderID.String(), nil)
	r = withClaims(r, uuid.New(), "manager")
	w := httptest.NewRecorder()

	s.GetOrder(w, r, orderID)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// --- CancelOrder ---

func TestCancelOrder_Success(t *testing.T) {
	orderID := uuid.New()
	svc := &mockOrderService{
		cancelOrder: func(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string, _ api.OrderCancel) (*models.Order, error) {
			return &models.Order{ID: orderID, Status: models.OrderCancelled}, nil
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderID.String()+"/cancel", nil)
	r = withClaims(r, uuid.New(), "client")
	w := httptest.NewRecorder()

	s.CancelOrder(w, r, orderID)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCancelOrder_CannotCancel(t *testing.T) {
	orderID := uuid.New()
	svc := &mockOrderService{
		cancelOrder: func(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string, _ api.OrderCancel) (*models.Order, error) {
			return nil, services.ErrCannotCancel
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderID.String()+"/cancel", nil)
	r = withClaims(r, uuid.New(), "client")
	w := httptest.NewRecorder()

	s.CancelOrder(w, r, orderID)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestCancelOrder_Forbidden(t *testing.T) {
	orderID := uuid.New()
	svc := &mockOrderService{
		cancelOrder: func(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string, _ api.OrderCancel) (*models.Order, error) {
			return nil, services.ErrForbidden
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderID.String()+"/cancel", nil)
	r = withClaims(r, uuid.New(), "client")
	w := httptest.NewRecorder()

	s.CancelOrder(w, r, orderID)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestCancelOrder_ServiceError(t *testing.T) {
	orderID := uuid.New()
	svc := &mockOrderService{
		cancelOrder: func(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string, _ api.OrderCancel) (*models.Order, error) {
			return nil, errors.New("db error")
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderID.String()+"/cancel", nil)
	r = withClaims(r, uuid.New(), "client")
	w := httptest.NewRecorder()

	s.CancelOrder(w, r, orderID)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// --- Legacy UpdateOrderStatus ---

func TestUpdateOrderStatus_Gone(t *testing.T) {
	s := newTestServer(&mockOrderService{})
	r := httptest.NewRequest(http.MethodPatch, "/orders/"+uuid.New().String()+"/status", nil)
	w := httptest.NewRecorder()

	s.UpdateOrderStatus(w, r, uuid.New())

	if w.Code != http.StatusGone {
		t.Errorf("expected 410, got %d", w.Code)
	}
}

func TestCompleteAssignment_PassesDeliveryPayload(t *testing.T) {
	assignmentID := uuid.New()
	recipient := "Ivan Petrov"
	comment := "cargo received intact"
	svc := &mockOrderService{
		completeAssignment: func(_ context.Context, id uuid.UUID, _ uuid.UUID, role string, req api.DeliveryComplete) (*models.Assignment, error) {
			if id != assignmentID {
				t.Fatalf("assignment id = %s, want %s", id, assignmentID)
			}
			if role != "driver" || req.RecipientName == nil || *req.RecipientName != recipient || req.Comment == nil || *req.Comment != comment {
				t.Fatalf("unexpected complete command: role=%s request=%+v", role, req)
			}
			return &models.Assignment{ID: id, Status: models.AssignmentCompleted, RecipientName: req.RecipientName, DeliveryComment: req.Comment}, nil
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/assignments/"+assignmentID.String()+"/complete", jsonBody(t, api.DeliveryComplete{
		RecipientName: &recipient,
		Comment:       &comment,
	}))
	r = withClaims(r, uuid.New(), "driver")
	w := httptest.NewRecorder()

	s.CompleteAssignment(w, r, assignmentID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// --- GetOrdersReport ---

func TestGetOrdersReport_Success(t *testing.T) {
	svc := &mockOrderService{
		getOrdersReport: func(_ context.Context, _ string, _ api.GetOrdersReportParams) ([]models.Order, error) {
			return []models.Order{{ID: uuid.New(), Status: models.OrderCompleted}}, nil
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodGet, "/reports/orders", nil)
	r = withClaims(r, uuid.New(), "manager")
	w := httptest.NewRecorder()

	s.GetOrdersReport(w, r, api.GetOrdersReportParams{})

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestGetOrdersReport_Forbidden(t *testing.T) {
	svc := &mockOrderService{
		getOrdersReport: func(_ context.Context, _ string, _ api.GetOrdersReportParams) ([]models.Order, error) {
			return nil, services.ErrForbidden
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodGet, "/reports/orders", nil)
	r = withClaims(r, uuid.New(), "client")
	w := httptest.NewRecorder()

	s.GetOrdersReport(w, r, api.GetOrdersReportParams{})

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestGetOrdersReport_ServiceError(t *testing.T) {
	svc := &mockOrderService{
		getOrdersReport: func(_ context.Context, _ string, _ api.GetOrdersReportParams) ([]models.Order, error) {
			return nil, errors.New("db error")
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodGet, "/reports/orders", nil)
	r = withClaims(r, uuid.New(), "manager")
	w := httptest.NewRecorder()

	s.GetOrdersReport(w, r, api.GetOrdersReportParams{})

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// --- GetDashboard ---

func TestGetDashboard_Success(t *testing.T) {
	svc := &mockOrderService{
		getDashboard: func(_ context.Context, _ string) (*models.DashboardReport, error) {
			return &models.DashboardReport{
				Revenue: models.DashboardRevenue{Total: 10000, ThisMonth: 3000},
				Orders:  models.DashboardOrderStatus{Total: 5, Delivered: 3, Pending: 2},
				Drivers: []models.DashboardDriverStat{},
			}, nil
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodGet, "/reports/dashboard", nil)
	r = withClaims(r, uuid.New(), "manager")
	w := httptest.NewRecorder()

	s.GetDashboard(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestGetDashboard_Forbidden(t *testing.T) {
	svc := &mockOrderService{
		getDashboard: func(_ context.Context, _ string) (*models.DashboardReport, error) {
			return nil, services.ErrForbidden
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodGet, "/reports/dashboard", nil)
	r = withClaims(r, uuid.New(), "client")
	w := httptest.NewRecorder()

	s.GetDashboard(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestGetDashboard_ServiceError(t *testing.T) {
	svc := &mockOrderService{
		getDashboard: func(_ context.Context, _ string) (*models.DashboardReport, error) {
			return nil, errors.New("db error")
		},
	}
	s := newTestServer(svc)
	r := httptest.NewRequest(http.MethodGet, "/reports/dashboard", nil)
	r = withClaims(r, uuid.New(), "manager")
	w := httptest.NewRecorder()

	s.GetDashboard(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}
