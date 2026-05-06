// cmd/server/main.go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"ticket_module/internal/adapters"
	"ticket_module/internal/clients"
	"ticket_module/internal/database"
	"ticket_module/internal/handlers"
	"ticket_module/internal/services"
	"ticket_module/pkg/config"
)

// Основная функция запуска ticket-сервиса.
func main() {
	// Загрузка конфигурации.
	cfg := config.Load()

	// Подключение к БД.
	db, err := database.NewDatabase(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()
	log.Println("Connected to database")

	// Инициализация репозитория.
	ticketRepo := database.NewTicketRepository(db)

	// Инициализация клиентов к внешним сервисам.
	pythonClient := clients.NewPythonClient(cfg.PythonNERServiceURL)
	summarizer := services.NewLLMSummarizer(services.SummarizerConfig{
		OllamaBaseURL:     cfg.OllamaBaseURL,
		OllamaModel:       cfg.OllamaModel,
		OllamaTemperature: cfg.OllamaTemperature,
		OllamaNumPredict:  cfg.OllamaNumPredict,
		RequestTimeout:    time.Duration(cfg.LLMRequestTimeoutSeconds) * time.Second,
	})

	// Выбор адаптера тикет-системы.
	var ticketAdapter adapters.TicketSystemAdapter
	switch cfg.TicketSystem {
	case "mock":
		// Mock-адаптер позволяет проверять пайплайн без реальной внешней интеграции.
		ticketAdapter = adapters.NewMockAdapter()
		log.Println("Using Mock ticket adapter")
	case "simpleone":
		ticketAdapter, err = adapters.NewSimpleOneAdapter(adapters.SimpleOneAdapterConfig{
			EndpointURL: cfg.SimpleOneEndpointURL,
			BearerToken: cfg.SimpleOneBearerToken,
			Timeout:     time.Duration(cfg.SimpleOneTimeoutSecs) * time.Second,
		})
		if err != nil {
			log.Fatalf("Failed to configure SimpleOne adapter: %v", err)
		}
		log.Printf("Using SimpleOne ticket adapter: %s", cfg.SimpleOneEndpointURL)
	default:
		ticketAdapter = adapters.NewMockAdapter()
		log.Printf("Unknown ticket system '%s', using mock", cfg.TicketSystem)
	}

	// Инициализация прикладного сервиса создания тикетов.
	ticketService := services.NewTicketCreatorService(
		pythonClient,
		summarizer,
		ticketAdapter,
		ticketRepo,
		cfg.TicketIncludePIIInDescription,
	)

	// Инициализация HTTP-обработчиков.
	ticketHandler := handlers.NewTicketHandler(ticketService)
	router := setupRouter(ticketHandler, cfg)

	httpAddr := ":" + cfg.ServerPort
	httpSrv := &http.Server{
		Addr:              httpAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("Starting ticket HTTP service on %s", httpAddr)

	// HTTP-сервер запускается в отдельной goroutine.
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	// Блок graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down ticket service...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Fatalf("HTTP server forced to shutdown: %v", err)
	}
}

// setupRouter создает и настраивает HTTP router ticket-сервиса.
func setupRouter(h *handlers.TicketHandler, cfg *config.Config) *gin.Engine {
	// В production режим Gin обычно переключается в release mode через переменную окружения.
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.DebugMode)
	}

	router := gin.Default()

	// Базовые middleware восстановления и CORS.
	router.Use(gin.Recovery())
	router.Use(corsMiddleware(cfg.CORSAllowedOrigins))

	// Health check.
	router.GET("/health", h.Health)

	// Основные HTTP-маршруты ticket-сервиса.
	api := router.Group("/api")
	{
		api.POST("/tickets", h.CreateTicket)
		api.GET("/tickets/:id", h.GetTicket)
		api.GET("/tickets", h.ListTickets)
		api.PATCH("/tickets/:id/status", h.UpdateTicketStatus)
		api.GET("/tickets/stats", h.GetStats)
	}

	return router
}

// corsMiddleware создает middleware для CORS. Без CORS браузер может заблокировать запрос.
func corsMiddleware(allowedOriginsRaw string) gin.HandlerFunc {
	// Строка разрешенных origin преобразуется в lookup-таблицу.
	allowedOrigins := parseAllowedOrigins(allowedOriginsRaw)
	allowAny := allowedOrigins["*"]

	return func(c *gin.Context) {
		setSecurityHeaders(c)
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if allowAny {
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin != "" && allowedOrigins[origin] {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Vary", "Origin")
		}
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// parseAllowedOrigins преобразует строку с разрешенными origin в lookup-таблицу.
func parseAllowedOrigins(raw string) map[string]bool {
	out := make(map[string]bool)
	for _, item := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(item)
		if origin == "" {
			continue
		}
		out[origin] = true
	}
	return out
}

// Добавляет базовые security headers в HTTP-ответ.
func setSecurityHeaders(c *gin.Context) {
	c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
	c.Writer.Header().Set("X-Frame-Options", "DENY")
	c.Writer.Header().Set("Referrer-Policy", "no-referrer")
}
