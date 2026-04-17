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
	"github.com/xuri/excelize/v2"
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
	if req.Status == api.OrderStatusUpdateStatusInTransit {
		go s.startRouteTracker(id)
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

	f := excelize.NewFile()
	sheet := "Orders"
	f.SetSheetName("Sheet1", sheet)

	headers := []string{"ID", "Status", "Origin", "Destination", "Weight", "Volume", "Price", "Created At"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}

	for row, o := range orders {
		values := []any{
			o.ID.String(),
			o.Status,
			o.OriginAddress,
			o.DestinationAddress,
			o.WeightKg,
			o.VolumeM3,
			o.TotalPrice,
			o.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		for col, v := range values {
			cell, _ := excelize.CoordinatesToCellName(col+1, row+2)
			f.SetCellValue(sheet, cell, v)
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		slog.ErrorContext(ctx, "excel write failed", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=orders_report.xlsx")
	w.WriteHeader(http.StatusOK)
	w.Write(buf.Bytes())
}

func (s *Server) GetDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := ctx.Value("user").(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	report, err := s.OrderSerice.GetDashboard(ctx, claims.Role)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
			return
		}
		slog.ErrorContext(ctx, "dashboard failed", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, report, RespSuccess)
}
