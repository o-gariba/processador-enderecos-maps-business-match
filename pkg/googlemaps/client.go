package googlemaps

import (
	"context"
	"encoding/json"
	"net/http"
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

// FindPlaceResult represents the result of a Find Place API call.
type FindPlaceResult struct {
	Candidates []struct {
		Name    string `json:"name"`
		PlaceID string `json:"place_id"`
	} `json:"candidates"`
	Status string `json:"status"`
}

// FindPlace finds a place using the Google Maps Find Place API.
func (c *Client) FindPlace(ctx context.Context, address string) (*FindPlaceResult, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://maps.googleapis.com/maps/api/place/findplacefromtext/json", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("input", address)
	q.Add("inputtype", "textquery")
	q.Add("fields", "name,place_id")
	q.Add("key", c.apiKey)
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result FindPlaceResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}
