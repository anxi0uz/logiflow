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

func (s *Server) RouteWebSocket(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	ctx := r.Context()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.ErrorContext(ctx, "ws upgrade failed", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	defer conn.Close()

	orderID := id
	s.Hub.Register(id, conn)
	defer s.Hub.Unregister(orderID, conn)

	s.Hub.mu.RLock()
	_, trackerRunning := s.Hub.trackers[orderID]
	s.Hub.mu.RUnlock()
	if !trackerRunning {
		go s.startRouteTracker(orderID)
	}

	route, err := storage.GetOne[models.Route](r.Context(), s.DB, "routes", func(sb *sqlbuilder.SelectBuilder) {
		sb.Where(sb.EQ("order_id", orderID))
	})
	if err == nil {
		coords, err := route.ParseCoordinates()
		if err == nil && route.CurrentIndex < len(coords) {
			conn.WriteJSON(map[string]any{
				"current_index": route.CurrentIndex,
				"coordinate":    coords[route.CurrentIndex],
			})
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
				route, err := storage.GetOne[models.Route](ctx, s.DB, "routes", func(sb *sqlbuilder.SelectBuilder) {
					sb.Where(sb.EQ("order_id", orderID))
				})
				if err != nil {
					return
				}

				coords, err := route.ParseCoordinates()
				if err != nil || len(coords) == 0 {
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
				storage.Update(ctx, "routes", *route, s.DB, func(sb *sqlbuilder.UpdateBuilder) {
					sb.Where(sb.EQ("order_id", orderID))
				})
			}
		}
	})
}
