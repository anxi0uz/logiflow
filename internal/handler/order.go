package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/anxi0uz/logiflow/internal/api"
	"github.com/anxi0uz/logiflow/internal/models"
	storage "github.com/anxi0uz/logiflow/pkg"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"golang.org/x/sync/errgroup"
)

func (s *Server) ListOrders(w http.ResponseWriter, r *http.Request, params api.ListOrdersParams) {}

func (s *Server) CreateOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := ctx.Value("user").(*Claims)
	if !ok {
		slog.ErrorContext(ctx, "error while casting claims")
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	var req api.OrderCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.JSON(w, r, http.StatusBadRequest, MsgInvalidBody, RespError)
		return
	}

	var (
		originLat, originLon float64
		destLat, destLon     float64
	)
	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error
		originLat, originLon, err = s.geocode(gctx, *req.OriginAddress)
		return err
	})

	g.Go(func() error {
		var err error
		destLat, destLon, err = s.geocode(gctx, req.DestinationAddress)
		return err
	})

	if err := g.Wait(); err != nil {
		s.JSON(w, r, http.StatusBadRequest, "Не удалось определить координаты", RespError)
		return
	}

	osrmURL := fmt.Sprintf(
		"http://router.project-osrm.org/route/v1/driving/%f,%f;%f,%f?overview=full&geometries=geojson",
		originLon, originLat, destLon, destLat,
	)
	osrmReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, osrmURL, nil)
	osrmReq.Header.Set("User-Agent", "logiflow/1.0")

	osrmResp, err := http.DefaultClient.Do(osrmReq)
	if err != nil {
		slog.ErrorContext(ctx, "OSRM request failed", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	defer osrmResp.Body.Close()

	var osrmResult struct {
		Routes []struct {
			Geometry struct {
				Coordinates [][]float64 `json:"coordinates"`
			} `json:"geometry"`
			Distance float64 `json:"distance"`
			Duration float64 `json:"duration"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(osrmResp.Body).Decode(&osrmResult); err != nil || len(osrmResult.Routes) == 0 {
		s.JSON(w, r, http.StatusBadRequest, "Не удалось построить маршрут", RespError)
		return
	}
	route := osrmResult.Routes[0]
	distanceKm := route.Distance / 1000

	var weightKg, volumeM3 float64
	if req.WeightKg != nil {
		weightKg = float64(*req.WeightKg)
	}
	if req.VolumeM3 != nil {
		volumeM3 = float64(*req.VolumeM3)
	}
	price := s.Config.Pricing.BaseFee + distanceKm*s.Config.Pricing.PerKm + weightKg*s.Config.Pricing.PerKg + volumeM3*s.Config.Pricing.PerM3

	now := time.Now
	orderID := uuid.New()
	order := models.Order{
		ID:                 orderID,
		CreatedByID:        &claims.ID,
		DestinationAddress: req.DestinationAddress,
		Status:             "pending",
		TotalPrice:         price,
		CreatedAt:          now(),
	}
	if *req.OriginAddress != "" {
		order.OriginAddress = *req.OriginAddress
	}
	if req.CargoDescription != nil {
		order.CargoDescription = *req.CargoDescription
	}
	if req.WeightKg != nil {
		order.WeightKg = float64(*req.WeightKg)
	}
	if req.VolumeM3 != nil {
		order.VolumeM3 = volumeM3
	}
	coordsJSON, err := json.Marshal(route.Geometry.Coordinates)
	if err != nil {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	routeModel := models.Route{
		ID:          uuid.New(),
		OrderID:     orderID,
		Coordinates: coordsJSON,
		DurationSec: int(route.Duration),
		Status:      "pending",
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	defer tx.Rollback(ctx)

	if err := storage.Create(ctx, "orders", order, tx); err != nil {
		slog.ErrorContext(ctx, "Failed to create order", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}
	if err := storage.Create(ctx, "routes", routeModel, tx); err != nil {
		slog.ErrorContext(ctx, "Failed to create route", slog.String("error", err.Error()))
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		s.JSON(w, r, http.StatusInternalServerError, MsgInternalError, RespError)
		return
	}

	s.JSON(w, r, http.StatusCreated, map[string]any{
		"order": order,
		"route": routeModel,
	}, "order")
}

func (s *Server) GetOrder(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {}

func (s *Server) CancelOrder(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {}

func (s *Server) UpdateOrderStatus(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {}

func (s *Server) GetOrdersReport(w http.ResponseWriter, r *http.Request, params api.GetOrdersReportParams) {
}

func (s *Server) GetDashboard(w http.ResponseWriter, r *http.Request) {}
