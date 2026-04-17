package handler

import (
	"log/slog"
	"net/http"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/huandu/go-sqlbuilder"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (s *Server) ListNotifications(w http.ResponseWriter, r *http.Request, params api.ListNotificationsParams) {
	ctx := r.Context()
	claims, ok := ctx.Value("user").(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "Error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	notifs, err := storage.GetAll[models.Notification](ctx, "notifications", s.DB, func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("user_id", claims.ID))
		if params.UnreadOnly != nil && *params.UnreadOnly {
			sb.Where(sb.EQ("is_read", false))
		}
	})
	if err != nil {
		slog.ErrorContext(ctx, "Error while getting notifications", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, notifs, RespSuccess)
}

func (s *Server) MarkNotificationRead(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	ctx := r.Context()
	claims, ok := ctx.Value("user").(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "Error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	notification, err := storage.GetOne[models.Notification](ctx, s.DB, "notifications", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("id", id))
	})
	if err != nil {
		slog.ErrorContext(ctx, "Error while getting notification", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	if notification.UserID != claims.ID {
		s.JSON(w, r, http.StatusForbidden, MsgForbidden, RespError)
		return
	}
	notification.IsRead = true

	if err := storage.Update(ctx, "notifications", *notification, s.DB, func(sb *sqlbuilder.UpdateBuilder) {
		sb.Where(sb.EQ("id", id))
	}); err != nil {
		slog.ErrorContext(ctx, "Error while updating notification", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, "Updated", RespSuccess)
}
