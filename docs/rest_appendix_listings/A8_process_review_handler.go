// Листинг А.8. Ручная валидация маршрутизации обращения.
package handlers

import (
    "log"
    "net/http"
    "strings"
    "time"

    "orchestrator/internal/clients"
    "orchestrator/internal/services"

    "github.com/gin-gonic/gin"
)

// Константы spam-класса обращений
const (
    reviewSpamIntentID       = "spam"
    reviewLegacySpamIntentID = "spam.call"
)

// Основной JSON-запрос для ручной проверки.
type routingReviewRequest struct {
    QueueID        string                      `json:"queue_id"`
    CallID         string                      `json:"call_id"`
    SourceFilename string                      `json:"source_filename"`
    Decision       string                      `json:"decision"`
    Transcript     spamReviewTranscriptPayload `json:"transcript"`
    Routing        reviewRoutingPayload        `json:"routing"`
    SpamCheck      reviewSpamCheckPayload      `json:"spam_check"`
}

// Функция проверяет, является ли цель обращения спамом
func isReviewSpamIntent(intentID string) bool {
    raw := strings.ToLower(strings.TrimSpace(intentID))
    return raw == reviewSpamIntentID || raw == reviewLegacySpamIntentID
}

// HTTP endpoint, который принимает решение ручной проверки.
func (h *ProcessHandler) ResolveRoutingReview(c *gin.Context) {

    // Чтение json
    var payload routingReviewRequest
    if err := c.ShouldBindJSON(&payload); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
        return
    }

    // JSON-запрос приводится к виду внутренни структур, которые понимает оркестратор.
    transcript := buildTranscript(payload.CallID, payload.Transcript)
    routing := buildRouting(payload)

    // Эта ветка срабатывает, если: оператор принял решение accepted; выбранный класс является спамом
    // То есть ручная проверка подтверждает, что звонок действительно спам.
    if strings.ToLower(strings.TrimSpace(payload.Decision)) == "accepted" && isReviewSpamIntent(routing.IntentID) {
        result, err := h.orchestrator.ContinueAfterSpamOverride(services.ContinueAfterSpamOverrideInput{
            CallID:         payload.CallID,
            SourceFilename: payload.SourceFilename,
            Decision:       payload.Decision,
            Transcript:     transcript,
            Routing:        routing,
        })
        if err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
            return
        }

        existing := h.loadExistingQueueRecord(payload.QueueID)
        review := buildCompletedReview(payload)
        if result.Routing != nil {
            review["intentId"] = strings.TrimSpace(result.Routing.IntentID)
            review["priority"] = normalizePriority(result.Routing.Priority)
            review["group"] = strings.TrimSpace(result.Routing.SuggestedGroup)
        }
        h.finalizeReviewResult(c, result, payload.SourceFilename, payload.QueueID, existing, review, "spam override")
        return
    }

    result, err := h.orchestrator.ContinueAfterRoutingReview(services.ContinueAfterRoutingReviewInput{
        CallID:         payload.CallID,
        SourceFilename: payload.SourceFilename,
        Decision:       payload.Decision,
        Transcript:     transcript,
        Routing:        routing,
    })
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    existing := h.loadExistingQueueRecord(payload.QueueID)
    review := buildCompletedReview(payload)
    h.finalizeReviewResult(c, result, payload.SourceFilename, payload.QueueID, existing, review, "routing review")
}
