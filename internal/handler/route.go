package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/huandu/go-sqlbuilder"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Server) GetRoute(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	ctx := r.Context()
	claims, ok := ctx.Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	if _, err := s.OrderSerice.GetOrder(ctx, id, claims.ID, claims.Role); err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	route, err := storage.GetOne[models.Route](ctx, s.DB, "routes", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("order_id", id))
	})
	if err != nil {
		if err == storage.ErrNotFound {
			s.JSON(w, r, http.StatusNotFound, MsgNotFound, RespNotFound)
			return
		}
		slog.ErrorContext(ctx, "Error while getting route", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	s.JSON(w, r, http.StatusOK, route, RespSuccess)
}

func (s *Server) RouteWebSocket(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	ctx := r.Context()
	claims, ok := ctx.Value(UserKey).(*Claims)
	if !ok {
		s.JSON(w, r, http.StatusUnauthorized, MsgUnauthorized, RespError)
		return
	}
	order, err := s.OrderSerice.GetOrder(ctx, id, claims.ID, claims.Role)
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	route, err := storage.GetOne[models.Route](ctx, s.DB, "routes", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("order_id", id))
	})
	if err != nil {
		s.writeOrderServiceError(w, r, err)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.ErrorContext(ctx, "ws upgrade failed", slog.String("error", err.Error()))
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			slog.Warn("ws conn close error", slog.String("error", err.Error()))
		}
	}()

	orderID := id
	s.Hub.Register(id, conn)
	defer s.Hub.Unregister(orderID, conn)

	s.Hub.mu.RLock()
	_, trackerRunning := s.Hub.trackers[orderID]
	s.Hub.mu.RUnlock()
	if !trackerRunning && order.Status == models.OrderInTransit {
		go s.startRouteTracker(orderID)
	}

	coords, err := route.ParseCoordinates()
	if err == nil && route.CurrentIndex < len(coords) {
		if err := conn.WriteJSON(map[string]any{
			"current_index": route.CurrentIndex,
			"coordinate":    coords[route.CurrentIndex],
		}); err != nil {
			slog.ErrorContext(ctx, "error while writing date to clients", slog.String("error", err.Error()))
		}
	}

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (s *Server) startRouteTracker(orderID uuid.UUID) {
	s.Hub.StartTracker(orderID, func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
				order, err := storage.GetOne[models.Order](ctx, s.DB, "orders", func(sb *sqlbuilder.SelectBuilder) {
					sb.Where(sb.EQ("id", orderID))
				})
				if err != nil || order.Status != models.OrderInTransit {
					s.Hub.StopTracker(orderID)
					return
				}
				route, err := storage.GetOne[models.Route](ctx, s.DB, "routes", func(sb *sqlbuilder.SelectBuilder) {
					sb.Where(sb.EQ("order_id", orderID))
				})
				if err != nil {
					s.Hub.StopTracker(orderID)
					return
				}

				coords, err := route.ParseCoordinates()
				if err != nil || len(coords) == 0 {
					s.Hub.StopTracker(orderID)
					return
				}

				if route.CurrentIndex >= len(coords) {
					s.Hub.StopTracker(orderID)
					return
				}

				s.Hub.Broadcast(orderID, map[string]any{
					"current_index": route.CurrentIndex,
					"coordinate":    coords[route.CurrentIndex],
				})
				route.CurrentIndex++
				if err := storage.Update(ctx, "routes", *route, s.DB, func(sb *sqlbuilder.UpdateBuilder) {
					sb.Where(sb.EQ("order_id", orderID))
				}); err != nil {
					slog.ErrorContext(ctx, "error while updating route", slog.String("id", route.ID.String()), slog.String("error", err.Error()))
				}
			}
		}
	})
}
