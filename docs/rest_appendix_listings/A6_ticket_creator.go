// Листинг А.6. Формирование и создание тикета.
package services

import (
    "fmt"
    "log"
    "strings"

    "ticket_module/internal/adapters"
    "ticket_module/internal/clients"
    "ticket_module/internal/database"
    "ticket_module/internal/models"
)

type TicketCreatorService struct {
    pythonClient            *clients.PythonClient
    summarizer              TicketSummarizer
    ticketAdapter           adapters.TicketSystemAdapter
    repository              *database.TicketRepository
    includePIIInDescription bool
}

// CreateTicket основной метод создания тикета
func (s *TicketCreatorService) CreateTicket(req *models.CreateTicketRequest) (*models.TicketCreated, error) {
    log.Printf("Creating ticket for call_id: %s, intent: %s",
        req.Transcript.CallID, req.Routing.IntentID)

    // 1. Используем уже извлеченные сущности, если orchestrator их передал.
    entities := req.Entities
    if entities == nil {
        entities = &models.Entities{}
    }
    if req.Entities == nil && s.pythonClient != nil {
        extracted, err := s.pythonClient.ExtractEntities(req.Transcript.Segments)
        if err != nil {
            log.Printf("Warning: Entity extraction failed: %v", err)
        } else {
            entities = extracted
        }
    }

    // 2. Генерируем заголовок и описание через LLM
    summary, err := s.summarizer.GenerateSummary(
        req.Transcript.Segments,
        req.Routing.IntentID,
        req.Routing.Priority,
        entities,
    )
    if err != nil {
        return nil, fmt.Errorf("generate summary: %w", err)
    }

    // 3. Формируем черновик тикета
    draft := s.buildTicketDraft(req, summary, entities)
    payload := buildTicketSystemPayload(req, draft, summary, entities)

    // 4. Создаем тикет в внешней системе (Mock/Jira/Redmine)
    created, err := s.ticketAdapter.CreateTicket(payload)
    if err != nil {
        return nil, fmt.Errorf("create ticket in external system: %w", err)
    }
    return created, nil
}

// buildTicketDraft формирует черновик тикета из данных
func (s *TicketCreatorService) buildTicketDraft(
    req *models.CreateTicketRequest,
    summary *models.TicketSummary,
    entities *models.Entities,
) *models.TicketDraft {

    // Определяем assignee на основе routing
    assigneeType := "group"
    assigneeID := req.Routing.SuggestedGroup
    if assigneeID == "" {
        assigneeID = "support"
    }

    // Генерируем теги
    tags := []string{req.Routing.IntentID}
    if req.Routing.Priority == "high" || req.Routing.Priority == "critical" {
        tags = append(tags, "urgent")
    }

    // Добавляем извлеченные сущности в описание только если это явно разрешено.
    description := composeTicketDescription(summary)
    if s.includePIIInDescription {
        description = appendEntityDetails(description, entities)
    }

    return &models.TicketDraft{
        Title:            buildTicketTitle(req.Routing.IntentID),
        Description:      description,
        Priority:         req.Routing.Priority,
        AssigneeType:     assigneeType,
        AssigneeID:       assigneeID,
        Tags:             tags,
        CallID:           req.Transcript.CallID,
        AudioURL:         req.AudioURL,
        IntentID:         req.Routing.IntentID,
        IntentConfidence: req.Routing.IntentConfidence,
        Entities:         entities,
    }
}
