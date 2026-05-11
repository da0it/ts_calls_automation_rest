// Листинг А.7. Адаптер интеграции с тикет-системой.
package adapters

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/da0it/ts_calls_automation_rest/services/ticket_creation/internal/models"
)

type SimpleOneAdapter struct {
    baseURL string
    client  *http.Client
    token   string
}

func NewSimpleOneAdapter(baseURL, token string) *SimpleOneAdapter {
    return &SimpleOneAdapter{
        baseURL: baseURL,
        token:   token,
        client:  &http.Client{Timeout: 30 * time.Second},
    }
}

func (a *SimpleOneAdapter) CreateTicket(ctx context.Context, payload *models.TicketSystemPayload) (*models.ExternalTicket, error) {
    body, err := json.Marshal(a.buildSimpleOnePayload(payload))
    if err != nil {
        return nil, err
    }

    req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/api/v1/tickets", bytes.NewReader(body))
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+a.token)
    req.Header.Set("Content-Type", "application/json")

    resp, err := a.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 300 {
        return nil, fmt.Errorf("simpleone returned status %d", resp.StatusCode)
    }

    var created models.ExternalTicket
    if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
        return nil, err
    }
    return &created, nil
}
