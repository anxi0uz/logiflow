package routing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOSRMPlannerWithWarehouseCoordinates(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/route/v1/driving/30.000000,60.000000;31.000000,61.000000" ||
			r.URL.Query().Get("geometries") != "geojson" || r.Header.Get("User-Agent") != "logiflow/1.0" {
			t.Errorf("unexpected OSRM request: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"routes":[{"geometry":{"coordinates":[[30,60],[31,61]]},"distance":25000,"duration":900}]}`))
	}))
	defer provider.Close()
	planner := OSRMPlanner{BaseURL: provider.URL, Client: provider.Client()}
	estimate, err := planner.Plan(context.Background(),
		Endpoint{Coordinates: &Coordinates{Latitude: 60, Longitude: 30}},
		Endpoint{Coordinates: &Coordinates{Latitude: 61, Longitude: 31}},
	)
	if err != nil || estimate.DistanceKm != 25 || estimate.DurationSec != 900 || len(estimate.Coordinates) != 2 {
		t.Fatalf("route estimate: %+v err=%v", estimate, err)
	}
}

func TestOSRMPlannerRejectsMissingRoute(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"routes":[]}`))
	}))
	defer provider.Close()
	planner := OSRMPlanner{BaseURL: provider.URL, Client: provider.Client()}
	_, err := planner.Plan(context.Background(),
		Endpoint{Coordinates: &Coordinates{Latitude: 60, Longitude: 30}},
		Endpoint{Coordinates: &Coordinates{Latitude: 61, Longitude: 31}},
	)
	if err == nil || !strings.Contains(err.Error(), "no route found") {
		t.Fatalf("missing route: %v", err)
	}
}
