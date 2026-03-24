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
)

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
