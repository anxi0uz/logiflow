package handler

import (
	"net/http"

	"github.com/anxi0uz/logiflow/internal/api"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (s *Server) ListNotifications(w http.ResponseWriter, r *http.Request, params api.ListNotificationsParams) {
}

func (s *Server) MarkNotificationRead(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
}
