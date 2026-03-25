package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/gosimple/slug"
)

func (s *Server) ListWarehouses(w http.ResponseWriter, r *http.Request) {}

func (s *Server) CreateWarehouse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req api.WarehouseCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.ErrorContext(ctx, "Error while decoding request body", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	now := time.Now()
	id := uuid.New()
	warehouse := models.Warehouse{
		ID:        id,
		Name:      req.Name,
		Address:   req.Address,
		City:      *req.City,
		Slug:      slug.Make(req.Name),
		Latitude:  float64(*req.Latitude),
		Longitude: float64(*req.Longitude),
		CreatedAt: now,
	}
	if err := storage.Create(ctx, "warehouses", warehouse, s.DB); err != nil {
		slog.ErrorContext(ctx, "Error while insert warehouse", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusCreated, warehouse, RespSuccess)
}

func (s *Server) GetWarehouse(w http.ResponseWriter, r *http.Request, slug string) {}

func (s *Server) UpdateWarehouse(w http.ResponseWriter, r *http.Request, slug string) {}

func (s *Server) DeleteWarehouse(w http.ResponseWriter, r *http.Request, slug string) {}
