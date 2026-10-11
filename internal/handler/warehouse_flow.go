package handler

import (
	"encoding/json"
	"net/http"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/google/uuid"
)

func (s *Server) CreateWarehouseOperation(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	var req api.WarehouseOperationCreate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	item, err := s.OrderSerice.CreateWarehouseOperation(r.Context(), id, claims.ID, claims.Role, req)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusCreated, item, "operation")
}
func (s *Server) ListOrderWarehouseOperations(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	items, err := s.OrderSerice.ListOrderWarehouseOperations(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, items, "operations")
}
func (s *Server) ListWarehouseQueue(w http.ResponseWriter, r *http.Request, id uuid.UUID, params api.ListWarehouseQueueParams) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	items, err := s.OrderSerice.ListWarehouseQueue(r.Context(), id, claims.ID, claims.Role, params)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, items, "orders")
}
