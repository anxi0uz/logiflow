package handler

import (
	"encoding/json"
	"net/http"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/google/uuid"
)

func (s *Server) CreateTripHandover(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	var req api.TripHandoverCreate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	item, err := s.OrderSerice.CreateTripHandover(r.Context(), id, claims.ID, claims.Role, req)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusCreated, item, "handover")
}

func (s *Server) ListIncomingTripHandovers(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	items, err := s.OrderSerice.ListIncomingTripHandovers(r.Context(), claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, items, "handovers")
}

func (s *Server) ListTripHandovers(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	items, err := s.OrderSerice.ListTripHandovers(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, items, "handovers")
}

func (s *Server) updateTripHandover(w http.ResponseWriter, r *http.Request, id uuid.UUID, action string) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	item, err := s.OrderSerice.UpdateTripHandover(r.Context(), id, claims.ID, claims.Role, action)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, item, "handover")
}
func (s *Server) ConfirmTripHandoverRelease(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	s.updateTripHandover(w, r, id, "confirm-release")
}
func (s *Server) AcceptTripHandover(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	s.updateTripHandover(w, r, id, "accept")
}
func (s *Server) CancelTripHandover(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	s.updateTripHandover(w, r, id, "cancel")
}
