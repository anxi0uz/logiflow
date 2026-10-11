package handler

import (
	"encoding/json"
	"net/http"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/google/uuid"
)

func (s *Server) CreateOrderIncident(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	var req api.OrderIncidentCreate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	item, err := s.OrderSerice.CreateOrderIncident(r.Context(), id, claims.ID, claims.Role, req)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusCreated, item, "incident")
}
func (s *Server) ListOrderIncidents(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	items, err := s.OrderSerice.ListOrderIncidents(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, items, "incidents")
}
func (s *Server) ResolveOrderIncident(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	var req api.OrderIncidentResolve
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	item, err := s.OrderSerice.ResolveOrderIncident(r.Context(), id, claims.ID, claims.Role, req)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, item, "incident")
}
