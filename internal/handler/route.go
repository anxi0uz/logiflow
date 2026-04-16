package handler

import (
	"log/slog"
	"net/http"

	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/huandu/go-sqlbuilder"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (s *Server) GetRoute(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	ctx := r.Context()
	route, err := storage.GetOne[models.Route](ctx, s.DB, "routes", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("order_id", id))
	})
	if err != nil {
		slog.ErrorContext(ctx, "Error while getting route", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, route, RespSuccess)
}

func (s *Server) RouteWebSocket(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {}
