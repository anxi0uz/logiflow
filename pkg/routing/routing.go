package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/anxi0uz/logiflow/pkg/geocode"
	"golang.org/x/sync/errgroup"
)

type Coordinates struct {
	Latitude  float64
	Longitude float64
}

type Endpoint struct {
	Address     string
	Coordinates *Coordinates
}

type Estimate struct {
	Coordinates [][]float64
	DistanceKm  float64
	DurationSec int
}

type Planner interface {
	Plan(ctx context.Context, origin, destination Endpoint) (Estimate, error)
}

// OSRMPlanner keeps the current synchronous Nominatim/OSRM integration in-process.
type OSRMPlanner struct {
	BaseURL string
	Client  *http.Client
}

func (p OSRMPlanner) Plan(ctx context.Context, origin, destination Endpoint) (Estimate, error) {
	var from, to Coordinates
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if origin.Coordinates != nil {
			from = *origin.Coordinates
			return nil
		}
		var err error
		from.Latitude, from.Longitude, err = geocode.Geocode(gctx, origin.Address)
		return err
	})
	g.Go(func() error {
		if destination.Coordinates != nil {
			to = *destination.Coordinates
			return nil
		}
		var err error
		to.Latitude, to.Longitude, err = geocode.Geocode(gctx, destination.Address)
		return err
	})
	if err := g.Wait(); err != nil {
		return Estimate{}, fmt.Errorf("get route coordinates: %w", err)
	}

	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = "http://router.project-osrm.org"
	}
	url := fmt.Sprintf(
		"%s/route/v1/driving/%f,%f;%f,%f?overview=full&geometries=geojson",
		strings.TrimRight(baseURL, "/"),
		from.Longitude, from.Latitude, to.Longitude, to.Latitude,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Estimate{}, fmt.Errorf("osrm request: %w", err)
	}
	req.Header.Set("User-Agent", "logiflow/1.0")
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Estimate{}, fmt.Errorf("osrm request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("failed to close osrm response body", slog.String("error", err.Error()))
		}
	}()
	var result struct {
		Routes []struct {
			Geometry struct {
				Coordinates [][]float64 `json:"coordinates"`
			} `json:"geometry"`
			Distance float64 `json:"distance"`
			Duration float64 `json:"duration"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Routes) == 0 {
		return Estimate{}, fmt.Errorf("osrm: no route found")
	}
	route := result.Routes[0]
	return Estimate{
		Coordinates: route.Geometry.Coordinates,
		DistanceKm:  route.Distance / 1000,
		DurationSec: int(route.Duration),
	}, nil
}
