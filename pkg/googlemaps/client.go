package googlemaps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// Client holds the necessary components for interacting with the Google Maps API.
type Client struct {
	apiKey     string
	httpClient *http.Client
	limiter    *rate.Limiter
}

// NewClient creates a new Google Maps client.
func NewClient(apiKey string, limiter *rate.Limiter) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		limiter: limiter,
	}
}

// --- Geocoding API Structures ---

// GeocodeResult represents a single result from the Geocoding API.
type GeocodeResult struct {
	PlaceID  string   `json:"place_id"`
	Types    []string `json:"types"`
	Geometry Geometry `json:"geometry"`
}

// Geometry holds the location data.
type Geometry struct {
	Location Location `json:"location"`
}

// Location holds the latitude and longitude.
type Location struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// GeocodeResponse represents the full response from the Geocoding API.
type GeocodeResponse struct {
	Results []GeocodeResult `json:"results"`
	Status  string          `json:"status"`
}

// --- Nearby Search API Structures ---

// NearbySearchResponse represents the response from the Nearby Search API.
type NearbySearchResponse struct {
	Results []Place `json:"results"`
	Status  string  `json:"status"`
}

// Place represents a single place found by Nearby Search.
type Place struct {
	PlaceID string   `json:"place_id"`
	Name    string   `json:"name"`
	Types   []string `json:"types"`
}

// --- Place Details API Structures ---

// PlaceDetails contains the detailed information for a specific place.
type PlaceDetails struct {
	Name                     string `json:"name"`
	FormattedAddress         string `json:"formatted_address"`
	InternationalPhoneNumber string `json:"international_phone_number"`
	Website                  string `json:"website"`
}

// PlaceDetailsResult represents the result of a Place Details API call.
type PlaceDetailsResult struct {
	Result PlaceDetails `json:"result"`
	Status string       `json:"status"`
}

// --- API Methods ---

// Geocode converts an address into a Place ID and metadata using the Geocoding API.
func (c *Client) Geocode(ctx context.Context, address string) (*GeocodeResponse, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://maps.googleapis.com/maps/api/geocode/json", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("address", address)
	q.Add("key", c.apiKey)
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result GeocodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.Status != "OK" && result.Status != "ZERO_RESULTS" {
		return nil, fmt.Errorf("geocoding API error: %s", result.Status)
	}

	return &result, nil
}

// NearbySearch finds places within a specified area.
func (c *Client) NearbySearch(ctx context.Context, lat, lng float64, radius uint) (*NearbySearchResponse, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://maps.googleapis.com/maps/api/place/nearbysearch/json", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("location", fmt.Sprintf("%f,%f", lat, lng))
	q.Add("radius", strconv.FormatUint(uint64(radius), 10))
	q.Add("key", c.apiKey)
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result NearbySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.Status != "OK" && result.Status != "ZERO_RESULTS" {
		return nil, fmt.Errorf("nearby search API error: %s", result.Status)
	}

	return &result, nil
}


// GetPlaceDetails gets detailed information about a place using its Place ID.
func (c *Client) GetPlaceDetails(ctx context.Context, placeID string) (*PlaceDetailsResult, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://maps.googleapis.com/maps/api/place/details/json", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("place_id", placeID)
	q.Add("fields", "name,formatted_address,website,international_phone_number")
	q.Add("key", c.apiKey)
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result PlaceDetailsResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.Status != "OK" {
		return nil, fmt.Errorf("place Details API error: %s", result.Status)
	}

	return &result, nil
}
