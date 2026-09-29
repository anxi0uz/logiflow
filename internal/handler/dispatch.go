package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/anxi0uz/logiflow/internal/dispatchpb"
	"github.com/anxi0uz/logiflow/internal/events"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (s *Server) GetDispatchRecommendations(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	result, err := s.OrderSerice.GetDispatchRecommendations(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, result, RespSuccess)
}

func (s *Server) RefreshDispatchRecommendations(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	request, err := s.OrderSerice.BuildDispatchRequest(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	if s.DispatchClient == nil {
		s.JSON(w, r, http.StatusServiceUnavailable, "dispatch unavailable", RespError)
		return
	}
	payload, err := json.Marshal(request)
	if err != nil {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	response, err := s.DispatchClient.Recommend(ctx, &dispatchpb.RecommendRequest{SnapshotJson: payload})
	if err != nil {
		s.JSON(w, r, http.StatusServiceUnavailable, "dispatch unavailable", RespError)
		return
	}
	var recommendation events.DispatchRecommended
	if err := json.Unmarshal(response.RecommendationsJson, &recommendation); err != nil ||
		recommendation.EventID != request.EventID || recommendation.OrderID != request.OrderID ||
		!recommendation.SubmittedAt.Equal(request.SubmittedAt) || !recommendation.RequestedAt.Equal(request.RequestedAt) {
		s.JSON(w, r, http.StatusBadGateway, "invalid dispatch response", RespError)
		return
	}
	if err := events.StoreDispatchRecommended(ctx, s.DB, response.RecommendationsJson); err != nil {
		s.JSON(w, r, http.StatusBadGateway, "invalid dispatch response", RespError)
		return
	}
	result, err := s.OrderSerice.GetDispatchRecommendations(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, result, RespSuccess)
}
