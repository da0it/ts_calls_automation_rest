package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"orchestrator/internal/clients"
	"orchestrator/internal/services"
)

type transcriptionHTTPCall struct {
	CallID   string
	Filename string
	Body     []byte
}

type fakeTranscriptionServer struct {
	mu       sync.Mutex
	requests []transcriptionHTTPCall
	response *clients.TranscriptionResponse
}

func (s *fakeTranscriptionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/transcribe" {
		http.NotFound(w, r)
		return
	}
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.requests = append(s.requests, transcriptionHTTPCall{
		CallID:   r.Header.Get("X-Call-ID"),
		Filename: r.Header.Get("X-Filename"),
		Body:     body,
	})
	response := s.response
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"transcript": response})
}

func (s *fakeTranscriptionServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

type fakeRoutingServer struct {
	mu       sync.Mutex
	requests []clients.RoutingRequest
	response *clients.RoutingResponse
}

func (s *fakeRoutingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/route" {
		http.NotFound(w, r)
		return
	}
	var req clients.RoutingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	response := s.response
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"routing": response})
}

func (s *fakeRoutingServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

type fakeTicketServer struct {
	mu       sync.Mutex
	requests []clients.CreateTicketRequest
	response *clients.TicketCreated
}

func (s *fakeTicketServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/tickets" {
		http.NotFound(w, r)
		return
	}
	var req clients.CreateTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	response := s.response
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(clients.CreateTicketResponse{
		Success: true,
		Ticket:  response,
	})
}

func (s *fakeTicketServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *fakeTicketServer) lastRequest() *clients.CreateTicketRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		return nil
	}
	last := s.requests[len(s.requests)-1]
	return &last
}

type testEnv struct {
	service            *services.OrchestratorService
	transcriptionCalls *fakeTranscriptionServer
	routingCalls       *fakeRoutingServer
	ticketCalls        *fakeTicketServer
	entityCalls        *int
}

func writeAudioFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "call.wav")
	if err := os.WriteFile(path, []byte("fake audio bytes"), 0o644); err != nil {
		t.Fatalf("write audio file: %v", err)
	}
	return path
}

func buildServiceForTest(
	t *testing.T,
	transcript *clients.TranscriptionResponse,
	routing *clients.RoutingResponse,
	entityResponse string,
	threshold float64,
) testEnv {
	t.Helper()

	transcriptionServer := &fakeTranscriptionServer{response: transcript}
	routingServer := &fakeRoutingServer{response: routing}
	ticketServer := &fakeTicketServer{
		response: &clients.TicketCreated{
			TicketID:   "ticket-001",
			ExternalID: "EXT-001",
			URL:        "http://ticket.local/ticket-001",
			System:     "simpleone",
			CreatedAt:  time.Now(),
		},
	}

	entityCalls := 0
	entityServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entityCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(entityResponse))
	}))
	t.Cleanup(entityServer.Close)

	transcriptionHTTP := httptest.NewServer(transcriptionServer)
	t.Cleanup(transcriptionHTTP.Close)
	routingHTTP := httptest.NewServer(routingServer)
	t.Cleanup(routingHTTP.Close)
	ticketHTTP := httptest.NewServer(ticketServer)
	t.Cleanup(ticketHTTP.Close)

	transcriptionClient, err := clients.NewTranscriptionClient(transcriptionHTTP.URL)
	if err != nil {
		t.Fatalf("new transcription client: %v", err)
	}
	t.Cleanup(func() { _ = transcriptionClient.Close() })

	routingClient, err := clients.NewRoutingClient(routingHTTP.URL)
	if err != nil {
		t.Fatalf("new routing client: %v", err)
	}
	t.Cleanup(func() { _ = routingClient.Close() })

	ticketClient, err := clients.NewTicketClient(ticketHTTP.URL, 10*time.Second)
	if err != nil {
		t.Fatalf("new ticket client: %v", err)
	}
	t.Cleanup(func() { _ = ticketClient.Close() })

	return testEnv{
		service: services.NewOrchestratorService(
			transcriptionClient,
			routingClient,
			ticketClient,
			clients.NewEntityClient(entityServer.URL),
			threshold,
		),
		transcriptionCalls: transcriptionServer,
		routingCalls:       routingServer,
		ticketCalls:        ticketServer,
		entityCalls:        &entityCalls,
	}
}

func TestProcessCallIntegrationFullPipeline(t *testing.T) {
	env := buildServiceForTest(
		t,
		&clients.TranscriptionResponse{
			CallID: "call-001",
			Segments: []clients.Segment{
				{Start: 0, End: 2, Speaker: "spk_0", Text: "Здравствуйте, чем могу помочь?"},
				{Start: 2, End: 5, Speaker: "spk_1", Text: "У меня проблема с заказом 12345."},
			},
			Metadata: map[string]interface{}{},
		},
		&clients.RoutingResponse{
			IntentID:         "orders.problem",
			IntentConfidence: 0.91,
			Priority:         "high",
			SuggestedGroup:   "support",
			SpamCheck: &clients.SpamCheckResponse{
				Status:         "allow",
				PredictedLabel: "not_spam",
				Confidence:     0.99,
			},
		},
		`{"entities":{"persons":[],"phones":[],"emails":[],"order_ids":[{"type":"order_id","value":"12345","confidence":0.97,"context":"проблема с заказом 12345"}],"account_ids":[],"money_amounts":[],"dates":[]}}`,
		0.5,
	)

	result, err := env.service.ProcessCall(writeAudioFile(t))
	if err != nil {
		t.Fatalf("process call: %v", err)
	}

	if result.Status != services.ProcessStatusCompleted {
		t.Fatalf("expected status %q, got %q", services.ProcessStatusCompleted, result.Status)
	}
	if result.CallID != "call-001" {
		t.Fatalf("unexpected call id: %s", result.CallID)
	}
	if env.transcriptionCalls.count() != 1 {
		t.Fatalf("expected 1 transcription call, got %d", env.transcriptionCalls.count())
	}
	if env.routingCalls.count() != 1 {
		t.Fatalf("expected 1 routing call, got %d", env.routingCalls.count())
	}
	if env.ticketCalls.count() != 1 {
		t.Fatalf("expected 1 ticket call, got %d", env.ticketCalls.count())
	}
	if *env.entityCalls != 1 {
		t.Fatalf("expected 1 entity call, got %d", *env.entityCalls)
	}
	if result.Ticket == nil || result.Ticket.TicketID != "ticket-001" {
		t.Fatalf("unexpected ticket in result: %#v", result.Ticket)
	}
	if result.Entities == nil || len(result.Entities.OrderIDs) != 1 {
		t.Fatalf("unexpected entities in result: %#v", result.Entities)
	}
	if result.TotalTime <= 0 {
		t.Fatalf("total time must be > 0")
	}

	ticketReq := env.ticketCalls.lastRequest()
	if ticketReq == nil {
		t.Fatalf("ticket request was not captured")
	}
	if ticketReq.Transcript.CallID != "call-001" {
		t.Fatalf("unexpected call id in ticket request: %s", ticketReq.Transcript.CallID)
	}
	if ticketReq.Routing.IntentID != "orders.problem" {
		t.Fatalf("unexpected intent in ticket request: %s", ticketReq.Routing.IntentID)
	}
	if ticketReq.Entities == nil || len(ticketReq.Entities.OrderIDs) != 1 {
		t.Fatalf("unexpected order ids in ticket request: %#v", ticketReq.Entities)
	}
}

func TestProcessCallIntegrationStopsOnLowConfidenceRouting(t *testing.T) {
	env := buildServiceForTest(
		t,
		&clients.TranscriptionResponse{
			CallID: "call-002",
			Segments: []clients.Segment{
				{Start: 0, End: 1, Speaker: "spk_0", Text: "Мне нужна помощь"},
			},
		},
		&clients.RoutingResponse{
			IntentID:         "misc.triage",
			IntentConfidence: 0.22,
			Priority:         "medium",
			SuggestedGroup:   "support",
			SpamCheck: &clients.SpamCheckResponse{
				Status:         "allow",
				PredictedLabel: "not_spam",
				Confidence:     0.9,
			},
		},
		`{"entities":{}}`,
		0.5,
	)

	result, err := env.service.ProcessCall(writeAudioFile(t))
	if err != nil {
		t.Fatalf("process call: %v", err)
	}

	if result.Status != services.ProcessStatusAwaitingRoutingReview {
		t.Fatalf("expected status %q, got %q", services.ProcessStatusAwaitingRoutingReview, result.Status)
	}
	if env.ticketCalls.count() != 0 {
		t.Fatalf("ticket service must not be called, got %d calls", env.ticketCalls.count())
	}
	if *env.entityCalls != 0 {
		t.Fatalf("entity service must not be called, got %d calls", *env.entityCalls)
	}
	if result.Ticket != nil {
		t.Fatalf("ticket must be nil when routing review is required")
	}
}

func TestProcessCallIntegrationBlocksSingleStageSpamIntent(t *testing.T) {
	env := buildServiceForTest(
		t,
		&clients.TranscriptionResponse{
			CallID: "call-spam",
			Segments: []clients.Segment{
				{Start: 0, End: 1, Speaker: "spk_0", Text: "Предлагаем услуги продвижения сайта."},
			},
		},
		&clients.RoutingResponse{
			IntentID:         "spam",
			IntentConfidence: 0.96,
			Priority:         "high",
			SuggestedGroup:   "support",
		},
		`{"entities":{}}`,
		0.5,
	)

	result, err := env.service.ProcessCall(writeAudioFile(t))
	if err != nil {
		t.Fatalf("process call: %v", err)
	}

	if result.Status != services.ProcessStatusSpamBlocked {
		t.Fatalf("expected status %q, got %q", services.ProcessStatusSpamBlocked, result.Status)
	}
	if env.ticketCalls.count() != 0 {
		t.Fatalf("ticket service must not be called for spam, got %d calls", env.ticketCalls.count())
	}
	if *env.entityCalls != 0 {
		t.Fatalf("entity service must not be called for spam, got %d calls", *env.entityCalls)
	}
}

func TestProcessCallIntegrationReturnsNoSpeechWithoutRouting(t *testing.T) {
	env := buildServiceForTest(
		t,
		&clients.TranscriptionResponse{
			CallID:   "call-empty",
			Segments: []clients.Segment{},
		},
		&clients.RoutingResponse{
			IntentID:         "misc.triage",
			IntentConfidence: 0.1,
			Priority:         "medium",
			SuggestedGroup:   "support",
		},
		`{"entities":{}}`,
		0.5,
	)

	result, err := env.service.ProcessCall(writeAudioFile(t))
	if err != nil {
		t.Fatalf("process call: %v", err)
	}

	if result.Status != services.ProcessStatusNoSpeech {
		t.Fatalf("expected status %q, got %q", services.ProcessStatusNoSpeech, result.Status)
	}
	if env.routingCalls.count() != 0 {
		t.Fatalf("routing service must not be called, got %d calls", env.routingCalls.count())
	}
	if env.ticketCalls.count() != 0 {
		t.Fatalf("ticket service must not be called, got %d calls", env.ticketCalls.count())
	}
	if *env.entityCalls != 0 {
		t.Fatalf("entity service must not be called, got %d calls", *env.entityCalls)
	}
	if result.Routing != nil {
		t.Fatalf("routing must be nil for no_speech, got %#v", result.Routing)
	}
	if result.Ticket != nil {
		t.Fatalf("ticket must be nil for no_speech, got %#v", result.Ticket)
	}
	if result.Entities == nil {
		t.Fatal("entities must not be nil for no_speech")
	}
}

func TestProcessCallIntegrationContinuesWhenEntityExtractionFails(t *testing.T) {
	transcriptionServer := &fakeTranscriptionServer{
		response: &clients.TranscriptionResponse{
			CallID: "call-003",
			Segments: []clients.Segment{
				{Start: 0, End: 2, Speaker: "spk_0", Text: "Не могу войти в систему."},
			},
		},
	}
	routingServer := &fakeRoutingServer{
		response: &clients.RoutingResponse{
			IntentID:         "portal_access",
			IntentConfidence: 0.96,
			Priority:         "high",
			SuggestedGroup:   "support",
			SpamCheck: &clients.SpamCheckResponse{
				Status:         "allow",
				PredictedLabel: "not_spam",
				Confidence:     0.99,
			},
		},
	}
	ticketServer := &fakeTicketServer{
		response: &clients.TicketCreated{
			TicketID:   "ticket-003",
			ExternalID: "EXT-003",
			URL:        "http://ticket.local/ticket-003",
			System:     "simpleone",
			CreatedAt:  time.Now(),
		},
	}

	entityCalls := 0
	entityServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entityCalls++
		http.Error(w, "ner failed", http.StatusInternalServerError)
	}))
	defer entityServer.Close()

	transcriptionHTTP := httptest.NewServer(transcriptionServer)
	defer transcriptionHTTP.Close()
	routingHTTP := httptest.NewServer(routingServer)
	defer routingHTTP.Close()
	ticketHTTP := httptest.NewServer(ticketServer)
	defer ticketHTTP.Close()

	transcriptionClient, err := clients.NewTranscriptionClient(transcriptionHTTP.URL)
	if err != nil {
		t.Fatalf("new transcription client: %v", err)
	}
	defer transcriptionClient.Close()

	routingClient, err := clients.NewRoutingClient(routingHTTP.URL)
	if err != nil {
		t.Fatalf("new routing client: %v", err)
	}
	defer routingClient.Close()

	ticketClient, err := clients.NewTicketClient(ticketHTTP.URL, 10*time.Second)
	if err != nil {
		t.Fatalf("new ticket client: %v", err)
	}
	defer ticketClient.Close()

	service := services.NewOrchestratorService(
		transcriptionClient,
		routingClient,
		ticketClient,
		clients.NewEntityClient(entityServer.URL),
		0.5,
	)

	result, err := service.ProcessCall(writeAudioFile(t))
	if err != nil {
		t.Fatalf("process call: %v", err)
	}
	if result.Status != services.ProcessStatusCompleted {
		t.Fatalf("expected status %q, got %q", services.ProcessStatusCompleted, result.Status)
	}
	if entityCalls != 1 {
		t.Fatalf("expected 1 entity extraction call, got %d", entityCalls)
	}
	if ticketServer.count() != 1 {
		t.Fatalf("expected 1 ticket call, got %d", ticketServer.count())
	}
	if result.Entities == nil {
		t.Fatal("entities must not be nil on non-fatal extraction failure")
	}
	if len(result.Entities.Phones) != 0 || len(result.Entities.OrderIDs) != 0 {
		t.Fatalf("entities should fall back to empty lists, got %#v", result.Entities)
	}
	if result.Ticket == nil || result.Ticket.TicketID == "" {
		t.Fatalf("ticket should still be created, got %#v", result.Ticket)
	}
}
