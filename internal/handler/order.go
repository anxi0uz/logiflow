package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/anxi0uz/logiflow/internal/api"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (s *Server) ListOrders(w http.ResponseWriter, r *http.Request, params api.ListOrdersParams) {}

func (s *Server) CreateOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := ctx.Value("user").(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	var req api.OrderCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}

	result, err := s.OrderSerice.CreateOrder(ctx, req, claims.ID)
	if err != nil {
		slog.ErrorContext(ctx, "create order failed", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	s.JSON(w, r, http.StatusCreated, map[string]any{
		"order": result.Order,
		"route": result.Route,
	}, "order")
}

func (s *Server) GetOrder(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {}

func (s *Server) CancelOrder(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {}

func (s *Server) UpdateOrderStatus(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {}

func (s *Server) GetOrdersReport(w http.ResponseWriter, r *http.Request, params api.GetOrdersReportParams) {
}

func (s *Server) GetDashboard(w http.ResponseWriter, r *http.Request) {}
