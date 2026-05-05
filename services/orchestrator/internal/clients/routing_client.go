// internal/clients/routing_client.go
package clients

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type RoutingClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewRoutingClient(baseURL string) (*RoutingClient, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("routing service url is required")
	}

	return &RoutingClient{
		baseURL: normalizeBaseURL(baseURL),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

type RoutingRequest struct {
	CallID       string    `json:"call_id"`
	Segments     []Segment `json:"segments"`
	SkipSpamGate bool      `json:"skip_spam_gate,omitempty"`
}

type RoutingResponse struct {
	IntentID         string             `json:"intent_id"`
	IntentConfidence float64            `json:"intent_confidence"`
	Priority         string             `json:"priority"`
	SuggestedGroup   string             `json:"suggested_group,omitempty"`
	SpamCheck        *SpamCheckResponse `json:"spam_check,omitempty"`
}

type SpamCheckResponse struct {
	Status         string  `json:"status"`
	PredictedLabel string  `json:"predicted_label,omitempty"`
	Confidence     float64 `json:"confidence"`
	ThresholdLow   float64 `json:"threshold_low"`
	ThresholdHigh  float64 `json:"threshold_high"`
	Reason         string  `json:"reason,omitempty"`
	Skipped        bool    `json:"skipped,omitempty"`
	Backend        string  `json:"backend,omitempty"`
}

type routeHTTPResponse struct {
	Routing *RoutingResponse `json:"routing"`
	Error   string           `json:"error,omitempty"`
}

func (c *RoutingClient) Route(callID string, segments []Segment) (*RoutingResponse, error) {
	return c.route(callID, segments, false)
}

func (c *RoutingClient) RouteSkippingSpam(callID string, segments []Segment) (*RoutingResponse, error) {
	return c.route(callID, segments, true)
}

func (c *RoutingClient) route(callID string, segments []Segment, skipSpamGate bool) (*RoutingResponse, error) {
	requestBody := RoutingRequest{
		CallID:       callID,
		Segments:     segments,
		SkipSpamGate: skipSpamGate,
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal routing request: %w", err)
	}

	url := c.baseURL + "/api/route"
	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("routing http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read routing response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("routing service returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result routeHTTPResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode routing response: %w", err)
	}
	if result.Routing == nil {
		return nil, fmt.Errorf("routing service: empty response")
	}
	return result.Routing, nil
}

func (c *RoutingClient) Close() error {
	return nil
}
