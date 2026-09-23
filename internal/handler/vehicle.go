package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5/pgconn"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"time"
)

func (s *Server) ListVehicleDocuments(w http.ResponseWriter, r *http.Request, slug string) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	if claims.Role != "manager" && claims.Role != "admin" {
		s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
		return
	}
	vehicle, err := storage.GetOne[models.Vehicle](r.Context(), s.DB, "vehicles", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("slug", slug)) })
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	items, err := storage.GetAll[models.VehicleDocument](r.Context(), "vehicle_documents", s.DB, func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("vehicle_id", vehicle.ID)).OrderBy("created_at DESC")
	})
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, items, "vehicleDocuments")
}

func (s *Server) CreateVehicleDocument(w http.ResponseWriter, r *http.Request, slug string) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	if claims.Role != "manager" && claims.Role != "admin" {
		s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
		return
	}
	var req api.VehicleDocumentCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Type != "registration" || req.Number == "" || req.ValidUntil.Time.Before(time.Now().Truncate(24*time.Hour)) {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	vehicle, err := storage.GetOne[models.Vehicle](r.Context(), tx, "vehicles", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("slug", slug)).ForUpdate() })
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	doc := models.VehicleDocument{ID: uuid.New(), VehicleID: vehicle.ID, Type: string(req.Type), Number: req.Number, ValidUntil: req.ValidUntil.Time, Status: "valid", CreatedAt: time.Now()}
	if err := storage.Create(r.Context(), "vehicle_documents", doc, tx); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			s.JSON(w, r, http.StatusConflict, "VEHICLE_DOCUMENT_EXISTS", RespError)
			return
		}
		s.writeOrderServiceError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusCreated, doc, "vehicleDocument")
}

func (s *Server) UpdateVehicleDocument(w http.ResponseWriter, r *http.Request, slug string, id openapi_types.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	if claims.Role != "manager" && claims.Role != "admin" {
		s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
		return
	}
	var req api.VehicleDocumentUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Status != "valid" && req.Status != "suspended" && req.Status != "expired") {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	tx, err := s.DB.Begin(r.Context())
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	vehicle, err := storage.GetOne[models.Vehicle](r.Context(), tx, "vehicles", func(sb *sqlbuilder.SelectBuilder) { sb.Where(sb.EQ("slug", slug)).ForUpdate() })
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	doc, err := storage.GetOne[models.VehicleDocument](r.Context(), tx, "vehicle_documents", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", id), sb.EQ("vehicle_id", vehicle.ID)).ForUpdate()
	})
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	doc.Status = string(req.Status)
	if err := storage.Update(r.Context(), "vehicle_documents", *doc, tx, func(ub *sqlbuilder.UpdateBuilder) { ub.Where(ub.EQ("id", id)) }); err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, doc, "vehicleDocument")
}

func (s *Server) ListVehicles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	vehicles, err := storage.GetAll[models.Vehicle](ctx, "vehicles", s.DB)
	if err != nil {
		slog.ErrorContext(ctx, "Unable to get all vehicles", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, vehicles, RespSuccess)
}

func (s *Server) CreateVehicle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := ctx.Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	if claims.Role != "manager" && claims.Role != "admin" {
		s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
		return
	}

	var req api.VehicleCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.ErrorContext(ctx, "Invalid request body", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}

	id := uuid.New()
	vehicle := models.Vehicle{
		ID:          id,
		PlateNumber: req.PlateNumber,
		Slug:        slug.Make(req.PlateNumber),
		Status:      "available",
	}

	if req.Brand != nil {
		vehicle.Brand = *req.Brand
	}
	if req.Model != nil {
		vehicle.Model = *req.Model
	}
	if req.Year != nil {
		vehicle.Year = *req.Year
	}
	if req.CapacityKg != nil {
		vehicle.CapacityKg = float64(*req.CapacityKg)
	}
	if req.CapacityM3 != nil {
		vehicle.CapacityM3 = float64(*req.CapacityM3)
	}
	if vehicle.CapacityKg < 0 || vehicle.CapacityM3 < 0 {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}

	if err := storage.Create(ctx, "vehicles", vehicle, s.DB); err != nil {
		slog.ErrorContext(ctx, "Unable to create vehicle", slog.Any("vehicle", vehicle))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusCreated, vehicle, "vehicle")
}

func (s *Server) GetVehicle(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()

	vehicle, err := storage.GetOne[models.Vehicle](ctx, s.DB, "vehicles", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.Equal("slug", slug))
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.JSON(w, r, http.StatusNotFound, "vehicle not found with that slug", RespNotFound)
			return
		}
		slog.ErrorContext(ctx, "no vehicle found in db with that slug", slog.String("slug", slug), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, vehicle, "vehicle")
}

func (s *Server) UpdateVehicle(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()
	claims, ok := ctx.Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	if claims.Role != "manager" && claims.Role != "admin" {
		s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
		return
	}

	var req api.VehicleUpdate

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.ErrorContext(ctx, "Invalid request body", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}

	vehicle, err := storage.GetOne[models.Vehicle](ctx, s.DB, "vehicles", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.Equal("slug", slug))
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.JSON(w, r, http.StatusNotFound, "vehicle not found with that slug", RespNotFound)
			return
		}
		slog.ErrorContext(ctx, "no vehicle found in db with that slug", slog.String("slug", slug), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	if req.Status != nil {
		vehicle.Status = string(*req.Status)
	}
	if req.Brand != nil {
		vehicle.Brand = *req.Brand
	}
	if req.Model != nil {
		vehicle.Model = *req.Model
	}
	if req.Year != nil {
		vehicle.Year = *req.Year
	}
	if req.CapacityKg != nil {
		vehicle.CapacityKg = float64(*req.CapacityKg)
	}
	if req.CapacityM3 != nil {
		vehicle.CapacityM3 = float64(*req.CapacityM3)
	}
	if vehicle.CapacityKg < 0 || vehicle.CapacityM3 < 0 {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	if err := storage.Update(ctx, "vehicles", *vehicle, s.DB, func(ub *sqlbuilder.UpdateBuilder) {
		ub.Where(ub.Equal("slug", slug))
	}); err != nil {
		slog.ErrorContext(ctx, "failed to update vehicle", slog.String("slug", slug), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, vehicle, "vehicle")
}

func (s *Server) DeleteVehicle(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()
	claims, ok := ctx.Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	if claims.Role != "manager" && claims.Role != "admin" {
		s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
		return
	}

	err := storage.Delete[models.Vehicle](ctx, "vehicles", s.DB, func(sb *sqlbuilder.DeleteBuilder) {
		sb.Where(sb.Equal("slug", slug))
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.JSON(w, r, http.StatusNotFound, "no vehicle with that slug found", RespNotFound)
			return
		}
		slog.ErrorContext(ctx, "unable to delete vehicle with that slug", slog.String("slug", slug), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, slug, "deleted")
}
