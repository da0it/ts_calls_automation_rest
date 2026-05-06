// internal/clients/ticket_client.go
package clients

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// TicketClient инкапсулирует HTTP-вызов сервиса создания тикетов.
type TicketClient struct {
	// Базовый адрес ticket-service.
	baseURL string

	// HTTP-клиент с таймаутом, достаточным для LLM и внешней интеграции.
	httpClient *http.Client
}

// NewTicketClient создает HTTP-клиент ticket-service с заданным таймаутом.
func NewTicketClient(baseURL string, timeout time.Duration) (*TicketClient, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("ticket service url is required")
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	return &TicketClient{
		baseURL: normalizeBaseURL(baseURL),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// TranscriptData описывает transcript в формате, который ожидает ticket-service.
type TranscriptData struct {
	CallID      string                 `json:"call_id"`
	Segments    []Segment              `json:"segments"`
	RoleMapping map[string]string      `json:"role_mapping"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// RoutingData описывает routing-результат, передаваемый в ticket-service.
type RoutingData struct {
	IntentID         string  `json:"intent_id"`
	IntentConfidence float64 `json:"intent_confidence"`
	Priority         string  `json:"priority"`
	SuggestedGroup   string  `json:"suggested_group,omitempty"`
}

// CreateTicketRequest объединяет transcript, routing и извлеченные сущности в единый payload.
type CreateTicketRequest struct {
	Transcript TranscriptData `json:"transcript"`
	Routing    RoutingData    `json:"routing"`
	Entities   *Entities      `json:"entities,omitempty"`
	AudioURL   string         `json:"audio_url,omitempty"`
}

// TicketCreated описывает результат успешного создания тикета.
type TicketCreated struct {
	TicketID   string    `json:"ticket_id"`
	ExternalID string    `json:"external_id"`
	URL        string    `json:"url"`
	System     string    `json:"system"`
	CreatedAt  time.Time `json:"created_at"`
}

// CreateTicketResponse соответствует HTTP-ответу ticket-service.
type CreateTicketResponse struct {
	Success bool           `json:"success"`
	Ticket  *TicketCreated `json:"ticket,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// CreateTicket отправляет результаты предыдущих этапов pipeline в ticket-service и возвращает созданный тикет.
func (c *TicketClient) CreateTicket(transcript *TranscriptionResponse, routing *RoutingResponse, entities *Entities) (*TicketCreated, error) {
	body, err := json.Marshal(CreateTicketRequest{
		Transcript: TranscriptData{
			CallID:      transcript.CallID,
			Segments:    transcript.Segments,
			RoleMapping: transcript.RoleMapping,
			Metadata:    transcript.Metadata,
		},
		Routing: RoutingData{
			IntentID:         routing.IntentID,
			IntentConfidence: routing.IntentConfidence,
			Priority:         routing.Priority,
			SuggestedGroup:   routing.SuggestedGroup,
		},
		Entities: entities,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal ticket request: %w", err)
	}

	url := c.baseURL + "/api/tickets"
	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ticket http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read ticket response: %w", err)
	}

	var result CreateTicketResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode ticket response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if result.Error != "" {
			return nil, fmt.Errorf("ticket service returned %d: %s", resp.StatusCode, result.Error)
		}
		return nil, fmt.Errorf("ticket service returned %d: %s", resp.StatusCode, string(respBody))
	}
	if !result.Success {
		if result.Error == "" {
			result.Error = "ticket service returned unsuccessful response"
		}
		return nil, errors.New(result.Error)
	}
	if result.Ticket == nil {
		return nil, fmt.Errorf("ticket service: empty response")
	}
	return result.Ticket, nil
}

func (c *TicketClient) Close() error {
	return nil
}
