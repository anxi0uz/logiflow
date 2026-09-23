package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/anxi0uz/logiflow/internal/api"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (s *Server) CreateAssignment(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	var req api.AssignmentCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	assignment, err := s.OrderSerice.CreateAssignment(r.Context(), id, claims.ID, claims.Role, req)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusCreated, assignment, "assignment")
}

func (s *Server) ListAssignments(w http.ResponseWriter, r *http.Request, params api.ListAssignmentsParams) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	items, err := s.OrderSerice.ListAssignments(r.Context(), claims.ID, claims.Role, params)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, items, "assignments")
}

func (s *Server) ListOrderAssignments(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	items, err := s.OrderSerice.ListOrderAssignments(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, items, "assignments")
}

func (s *Server) AcceptAssignment(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	assignment, err := s.OrderSerice.AcceptAssignment(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, assignment, "assignment")
}

func (s *Server) RejectAssignment(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	var req api.AssignmentReject
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	assignment, err := s.OrderSerice.RejectAssignment(r.Context(), id, claims.ID, claims.Role, req)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, assignment, "assignment")
}

func (s *Server) StartAssignment(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	assignment, err := s.OrderSerice.StartAssignment(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	if s.DB != nil {
		go s.startRouteTracker(assignment.OrderID)
	}
	s.JSON(w, r, http.StatusOK, assignment, "assignment")
}

func (s *Server) ArriveAssignment(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	assignment, err := s.OrderSerice.ArriveAssignment(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, assignment, "assignment")
}

func (s *Server) CompleteAssignment(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	var req api.DeliveryComplete
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	assignment, err := s.OrderSerice.CompleteAssignment(r.Context(), id, claims.ID, claims.Role, req)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, assignment, "assignment")
}
