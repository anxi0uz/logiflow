package handler

import (
	"encoding/json"
	"net/http"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/google/uuid"
)

func (s *Server) CreateCounterparty(w http.ResponseWriter, r *http.Request) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	var req api.CounterpartyWrite
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	item, err := s.OrderSerice.CreateCounterparty(r.Context(), claims.ID, claims.Role, req)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusCreated, item, "counterparty")
}

func (s *Server) GetCounterparty(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	item, err := s.OrderSerice.GetCounterparty(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, item, "counterparty")
}

func (s *Server) ListCounterparties(w http.ResponseWriter, r *http.Request, params api.ListCounterpartiesParams) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	item, err := s.OrderSerice.ListCounterparties(r.Context(), claims.ID, claims.Role, params)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, item, "counterparties")
}

func (s *Server) UpdateCounterparty(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	var req api.CounterpartyWrite
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	item, err := s.OrderSerice.UpdateCounterparty(r.Context(), id, claims.ID, claims.Role, req)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, item, "counterparty")
}

func (s *Server) SetTransportDetails(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	var req api.TransportDetailsWrite
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	item, err := s.OrderSerice.SetTransportDetails(r.Context(), id, claims.ID, claims.Role, req)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, item, "order")
}

func (s *Server) CreateDeliveryReceipt(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	// Zero is a valid physical count; distinguish it from an omitted/null field.
	var body struct {
		api.DeliveryReceiptCreate
		PackageCount *int `json:"packageCount"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.PackageCount == nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}
	req := body.DeliveryReceiptCreate
	req.PackageCount = *body.PackageCount
	item, err := s.OrderSerice.CreateDeliveryReceipt(r.Context(), id, claims.ID, claims.Role, req)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusCreated, item, "receipt")
}

func (s *Server) GetDeliveryReceipt(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	item, err := s.OrderSerice.GetDeliveryReceipt(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, item, "receipt")
}
