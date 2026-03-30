package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/anxi0uz/logiflow/pkg/geocode"
	"github.com/google/uuid"
	"github.com/gosimple/slug"
	"github.com/huandu/go-sqlbuilder"
)

func (s *Server) ListWarehouses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	warehouses, err := storage.GetAll[models.Warehouse](ctx, "warehouses", s.DB)
	if err != nil {
		slog.ErrorContext(ctx, "Error while getting all warehouses", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, warehouses, RespSuccess)
}

func (s *Server) CreateWarehouse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req api.WarehouseCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.ErrorContext(ctx, "Error while decoding request body", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	fullAddress := fmt.Sprintf("%s, %s", req.City, req.Address)
	lat, lon, err := geocode.Geocode(ctx, fullAddress)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to geocode address", slog.String("address", req.Address), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusBadRequest, "Не удалось определить координаты по адресу", RespError)
		return
	}

	now := time.Now()
	id := uuid.New()
	warehouse := models.Warehouse{
		ID:        id,
		Name:      req.Name,
		Address:   req.Address,
		City:      req.City,
		Slug:      slug.Make(req.Name),
		Latitude:  lat,
		Longitude: lon,
		CreatedAt: now,
	}
	if err := storage.Create(ctx, "warehouses", warehouse, s.DB); err != nil {
		slog.ErrorContext(ctx, "Error while insert warehouse", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusCreated, warehouse, RespSuccess)
}

func (s *Server) GetWarehouse(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()

	warehouse, err := storage.GetOne[models.Warehouse](ctx, s.DB, "warehouses", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("slug", slug))
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			slog.WarnContext(ctx, "No warehouse with that slug was found", slog.String("slug", slug))
			s.JSON(w, r, http.StatusNotFound, MsgNotFound, RespNotFound)
			return
		}
		slog.ErrorContext(ctx, "Error while getting warehouse", slog.String("slug", slug), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, warehouse, RespSuccess)
}

func (s *Server) UpdateWarehouse(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()
	var req api.WarehouseUpdate

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.ErrorContext(ctx, "Invalid json body", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	warehouse, err := storage.GetOne[models.Warehouse](ctx, s.DB, "warehouses", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.Equal("slug", slug))
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			slog.WarnContext(ctx, "No warehouse with that slug was found", slog.String("slug", slug), slog.String("error", err.Error()))
			s.JSON(w, r, http.StatusNotFound, MsgNotFound, RespNotFound)
			return
		}
		slog.ErrorContext(ctx, "Error while getting warehouse with that slug", slog.String("slug", slug), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	warehouse.Address = *req.Address
	warehouse.City = *req.City
	warehouse.Latitude = float64(*req.Latitude)
	warehouse.Longitude = float64(*req.Longitude)
	warehouse.Status = string(*req.Status)
	warehouse.Name = *req.Name
	if err := storage.Update(ctx, "warehouses", warehouse, s.DB, func(sb *sqlbuilder.UpdateBuilder) {
		sb.Where(sb.Equal("slug", slug))
	}); err != nil {
		slog.ErrorContext(ctx, "Error while updating warehouse with that slug", slog.String("slug", slug), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, warehouse, RespSuccess)
}

func (s *Server) DeleteWarehouse(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()

	if err := storage.Delete[models.Warehouse](ctx, "warehouses", s.DB, func(sb *sqlbuilder.DeleteBuilder) {
		sb.Where(sb.EQ("slug", slug))
	}); err != nil {
		slog.ErrorContext(ctx, "Error while deleting warehoue", slog.String("slug", slug), slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	s.JSON(w, r, http.StatusOK, slug, RespSuccess)
}
