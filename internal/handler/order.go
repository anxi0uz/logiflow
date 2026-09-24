package handler

import (
	"encoding/json"
	"errors"
	"io"
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
	claims, ok := ctx.Value(UserKey).(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "Error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	orders, err := s.OrderSerice.ListOrders(ctx, claims.ID, claims.Role, params)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
			return
		}
		slog.ErrorContext(ctx, "Error while getting list of orders", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, orders, RespSuccess)
}

func (s *Server) CreateOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := ctx.Value(UserKey).(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	if claims.Role != "client" && claims.Role != "manager" && claims.Role != "admin" {
		s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
		return
	}

	var req api.OrderCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}

	result, err := s.OrderSerice.CreateOrder(ctx, req, claims.ID, claims.Role)
	if err != nil {
		if _, ok := services.BusinessErrorCode(err); ok {
			s.writeOrderServiceError(w, r, err)
			return
		}
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
	claims, ok := ctx.Value(UserKey).(*Claims)
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

func (s *Server) UpdateDraftOrder(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	var req api.OrderDraftUpdate
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	order, err := s.OrderSerice.UpdateDraftOrder(r.Context(), id, claims.ID, claims.Role, req)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, order, "order")
}

func (s *Server) CancelOrder(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	ctx := r.Context()
	claims, ok := ctx.Value(UserKey).(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "Error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	var req api.OrderCancel
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	order, err := s.OrderSerice.CancelOrder(ctx, id, claims.ID, claims.Role, req)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, order, "order")
}

func (s *Server) UpdateOrderStatus(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	s.JSON(w, r, http.StatusGone, "use explicit /api/v1 order and assignment commands", RespError)
}

func (s *Server) SubmitOrder(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	order, err := s.OrderSerice.SubmitOrder(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, order, "order")
}

func (s *Server) writeOrderServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, storage.ErrNotFound) {
		s.JSON(w, r, http.StatusNotFound, MsgNotFound, RespNotFound)
		return
	}
	if errors.Is(err, services.ErrForbidden) {
		s.JSON(w, r, http.StatusForbidden, services.ErrForbidden.Code, RespError)
		return
	}
	if code, ok := services.BusinessErrorCode(err); ok {
		status := http.StatusConflict
		if errors.Is(err, services.ErrInvalidOrderInput) || errors.Is(err, services.ErrInvalidTimeWindow) || errors.Is(err, services.ErrVehicleDocumentInvalid) || errors.Is(err, services.ErrVehicleCapacityExceeded) || errors.Is(err, services.ErrDriverLicenseExpired) {
			status = http.StatusUnprocessableEntity
		}
		s.JSON(w, r, status, code, RespError)
		return
	}
	slog.ErrorContext(r.Context(), "order command failed", slog.String("error", err.Error()))
	s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
}

func (s *Server) GetOrdersReport(w http.ResponseWriter, r *http.Request, params api.GetOrdersReportParams) {
	ctx := r.Context()
	claims, ok := ctx.Value(UserKey).(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "Error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	orders, err := s.OrderSerice.GetOrdersReport(ctx, claims.ID, claims.Role, params)
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
	f.SetSheetName("Sheet1", sheet) //nolint:errcheck

	headers := []string{"ID", "Status", "Origin", "Destination", "Weight", "Volume", "Price", "Created At"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h) //nolint:errcheck
	}

	for row, o := range orders {
		var price any = ""
		if o.TotalPrice != nil {
			price = *o.TotalPrice
		}
		values := []any{
			o.ID.String(),
			o.Status,
			o.OriginAddress,
			o.DestinationAddress,
			o.WeightKg,
			o.VolumeM3,
			price,
			o.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		for col, v := range values {
			cell, _ := excelize.CoordinatesToCellName(col+1, row+2)
			f.SetCellValue(sheet, cell, v) //nolint:errcheck
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
	w.Write(buf.Bytes()) //nolint:errcheck
}

func (s *Server) GetDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := ctx.Value(UserKey).(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	report, err := s.OrderSerice.GetDashboard(ctx, claims.ID, claims.Role)
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
