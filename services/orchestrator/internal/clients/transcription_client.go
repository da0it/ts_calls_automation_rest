// internal/clients/transcription_client.go
package clients

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// TranscriptionClient инкапсулирует HTTP-вызов сервиса транскрибации.
type TranscriptionClient struct {
	// Базовый адрес transcription-service.
	baseURL string

	// HTTP-клиент, через который orchestrator отправляет запросы на транскрибацию.
	httpClient *http.Client
}

// NewTranscriptionClient создает HTTP-клиент transcription-service и настраивает таймаут запроса.
func NewTranscriptionClient(baseURL string) (*TranscriptionClient, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("transcription service url is required")
	}

	timeoutSec := 9999
	if raw := os.Getenv("TRANSCRIPTION_HTTP_TIMEOUT_SECONDS"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			timeoutSec = parsed
		}
	} else if raw := os.Getenv("TRANSCRIPTION_RPC_TIMEOUT_SECONDS"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			timeoutSec = parsed
		}
	}

	return &TranscriptionClient{
		baseURL: normalizeBaseURL(baseURL),
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
	}, nil
}

// Segment описывает один сегмент распознанного диалога.
type Segment struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker string  `json:"speaker"`
	Role    string  `json:"role"`
	Text    string  `json:"text"`
}

// TranscriptionResponse представляет transcript в формате, который использует orchestrator.
type TranscriptionResponse struct {
	CallID      string                 `json:"call_id"`
	Segments    []Segment              `json:"segments"`
	RoleMapping map[string]string      `json:"role_mapping"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// Внутренняя структура HTTP-ответа transcription-сервиса.
type transcribeHTTPResponse struct {
	Transcript *TranscriptionResponse `json:"transcript"`
	Error      string                 `json:"error,omitempty"`
}

// Transcribe читает локальный аудиофайл, отправляет его в transcription-service и возвращает transcript.
func (c *TranscriptionClient) Transcribe(audioPath string) (*TranscriptionResponse, error) {
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return nil, fmt.Errorf("read audio file: %w", err)
	}

	callID := filepath.Base(audioPath)
	if ext := filepath.Ext(callID); ext != "" {
		callID = callID[:len(callID)-len(ext)]
	}
	if callID == "" {
		callID = "unknown-call"
	}

	url := c.baseURL + "/api/transcribe"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(audioData))
	if err != nil {
		return nil, fmt.Errorf("build transcription request: %w", err)
	}

	// Для HTTP API имя файла и call_id передаются через заголовки.
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Call-ID", callID)
	req.Header.Set("X-Filename", filepath.Base(audioPath))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("transcription http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read transcription response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("transcription service returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result transcribeHTTPResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode transcription response: %w", err)
	}
	if result.Transcript == nil {
		return nil, fmt.Errorf("transcription service: empty response")
	}
	if result.Transcript.CallID == "" {
		result.Transcript.CallID = callID
	}
	if result.Transcript.Metadata == nil {
		result.Transcript.Metadata = map[string]interface{}{}
	}
	if result.Transcript.RoleMapping == nil {
		result.Transcript.RoleMapping = map[string]string{}
	}
	return result.Transcript, nil
}

func (c *TranscriptionClient) Close() error {
	return nil
}
