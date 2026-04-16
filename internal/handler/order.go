package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/services"
	storage "github.com/anxi0uz/logiflow/pkg"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (s *Server) ListOrders(w http.ResponseWriter, r *http.Request, params api.ListOrdersParams) {
	ctx := r.Context()
	claims, ok := ctx.Value("user").(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "Error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	orders, err := s.OrderSerice.ListOrders(ctx, claims.ID, claims.Role, params)
	if err != nil {
		slog.ErrorContext(ctx, "Error while getting list of orders", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, orders, RespSuccess)
}

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

func (s *Server) GetOrder(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	ctx := r.Context()
	claims, ok := ctx.Value("user").(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "Error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	order, err := s.OrderSerice.GetOrder(ctx, id, claims.ID, claims.Role)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
			return
		}
		if errors.Is(err, storage.ErrNotFound) {
			s.JSON(w, r, http.StatusNotFound, MsgNotFound, RespNotFound)
			return
		}
		slog.ErrorContext(ctx, "Error while getting order", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, order, RespSuccess)
}

func (s *Server) CancelOrder(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	ctx := r.Context()
	claims, ok := ctx.Value("user").(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "Error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	if err := s.OrderSerice.CancelOrder(ctx, id, claims.ID, claims.Role); err != nil {
		if errors.Is(err, services.ErrCannotCancel) {
			slog.ErrorContext(ctx, "Cant cancel order with that id", slog.String("id", id.String()))
			s.JSON(w, r, http.StatusConflict, "order cant be cancelled in current status", RespError)
			return
		}
		if errors.Is(err, services.ErrForbidden) {
			s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
			return
		}
		slog.ErrorContext(ctx, "error while cancelling order with that id", slog.Any("id", id), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, "Cancelled", RespSuccess)
}

func (s *Server) UpdateOrderStatus(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	ctx := r.Context()
	claims, ok := ctx.Value("user").(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "Error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	var req api.OrderStatusUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	order, err := s.OrderSerice.UpdateOrderStatus(ctx, id, claims.ID, claims.Role, req)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrForbidden):
			s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
		case errors.Is(err, services.ErrCannotCancel):
			s.JSON(w, r, http.StatusConflict, "cannot cancel order in current status", RespError)
		default:
			slog.ErrorContext(ctx, "...", slog.String("error", err.Error()))
			s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		}
		return
	}
	s.JSON(w, r, http.StatusOK, order, RespSuccess)
}

func (s *Server) GetOrdersReport(w http.ResponseWriter, r *http.Request, params api.GetOrdersReportParams) {
	ctx := r.Context()
	claims, ok := ctx.Value("user").(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "Error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	orders, err := s.OrderSerice.GetOrdersReport(ctx, claims.Role, params)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
			return
		}
		slog.ErrorContext(ctx, "Error while getting orders report", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, orders, RespSuccess)
}

func (s *Server) GetDashboard(w http.ResponseWriter, r *http.Request) {}
