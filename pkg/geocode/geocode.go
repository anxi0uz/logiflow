package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"strconv"
)

type OsrmResult struct {
	Routes []struct {
		Geometry struct {
			Coordinates [][]float64 `json:"coordinates"`
		} `json:"geometry"`
		Distance float64 `json:"distance"`
		Duration float64 `json:"duration"`
	} `json:"routes"`
}

func Geocode(ctx context.Context, address string) (lat, lon float64, err error) {
	url := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/search?q=%s&format=json&limit=1",
		neturl.QueryEscape(address),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("User-Agent", "logiflow/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	var results []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return 0, 0, err
	}
	if len(results) == 0 {
		return 0, 0, fmt.Errorf("address not found: %s", address)
	}

	lat, _ = strconv.ParseFloat(results[0].Lat, 64)
	lon, _ = strconv.ParseFloat(results[0].Lon, 64)
	return lat, lon, nil
}
