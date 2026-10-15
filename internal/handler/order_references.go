package handler

import (
	"net/http"

	"github.com/google/uuid"
)

func (s *Server) GetOrderReferences(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	claims, ok := r.Context().Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	refs, err := s.OrderSerice.GetOrderReferences(r.Context(), id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	s.JSON(w, r, http.StatusOK, refs, "references")
}
