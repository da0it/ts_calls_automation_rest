// cmd/server/main.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"orchestrator/internal/clients"
	"orchestrator/internal/handlers"
	"orchestrator/internal/middleware"
	"orchestrator/internal/models"
	"orchestrator/internal/services"
	"orchestrator/pkg/config"
)

func main() {
	cfg := config.Load()

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("✓ Database connected")

	userService := services.NewUserService(db)
	if err := userService.Migrate(); err != nil {
		log.Fatalf("Failed to run users migration: %v", err)
	}
	auditService := services.NewAuditService(db)
	if err := auditService.Migrate(); err != nil {
		log.Fatalf("Failed to run audit migration: %v", err)
	}
	appSettingsService := services.NewAppSettingsService(db)
	if err := appSettingsService.Migrate(); err != nil {
		log.Fatalf("Failed to run app settings migration: %v", err)
	}
	callQueueService := services.NewCallQueueService(db)
	if err := callQueueService.Migrate(); err != nil {
		log.Fatalf("Failed to run call queue migration: %v", err)
	}
	if err := userService.SeedAdmin(cfg.AdminUsername, cfg.AdminPassword); err != nil {
		log.Fatalf("Failed to seed admin: %v", err)
	}

	transcriptionClient, err := clients.NewTranscriptionClient(cfg.TranscriptionServiceURL)
	if err != nil {
		log.Fatalf("Failed to initialize transcription client: %v", err)
	}
	defer transcriptionClient.Close()

	routingClient, err := clients.NewRoutingClient(cfg.RoutingServiceURL)
	if err != nil {
		log.Fatalf("Failed to initialize routing client: %v", err)
	}
	defer routingClient.Close()

	ticketClient, err := clients.NewTicketClient(
		cfg.TicketServiceURL,
		time.Duration(cfg.TicketRequestTimeoutSeconds)*time.Second,
	)
	if err != nil {
		log.Fatalf("Failed to initialize ticket client: %v", err)
	}
	defer ticketClient.Close()

	entityClient := clients.NewEntityClient(cfg.EntityServiceURL)
	log.Println("✓ All clients initialized")

	orchestrator := services.NewOrchestratorService(
		transcriptionClient,
		routingClient,
		ticketClient,
		entityClient,
		cfg.RoutingReviewConfidenceThreshold,
	)
	log.Println("✓ Orchestrator service initialized")

	routingConfigService := services.NewRoutingConfigService(
		cfg.RoutingIntentsPath,
		cfg.RoutingGroupsPath,
	)
	routingFeedbackService := services.NewRoutingFeedbackService(
		cfg.RoutingFeedbackPath,
		cfg.RoutingAutoLearn,
		cfg.RoutingAutoLearnLimit,
		routingConfigService,
	)
	routingModelService := services.NewRoutingModelService(
		cfg.RouterAdminURL,
		cfg.RouterAdminToken,
		time.Duration(cfg.RouterAdminTimeoutSeconds)*time.Second,
		filepath.Dir(cfg.RoutingFeedbackPath),
	)

	processHandler := handlers.NewProcessHandler(
		orchestrator,
		callQueueService,
		appSettingsService,
		routingConfigService,
		routingFeedbackService,
		routingModelService,
		auditService,
	)
	authHandler := handlers.NewAuthHandler(userService, cfg.JWTSecret, cfg.JWTExpiryHours, auditService)

	authMw := middleware.AuthRequired(cfg.JWTSecret, userService)
	adminMw := middleware.RequireRole(models.RoleAdmin)

	router := setupRouter(processHandler, authHandler, authMw, adminMw, cfg)

	httpAddr := ":" + cfg.HTTPPort
	httpSrv := &http.Server{
		Addr:              httpAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	httpScheme := "http"
	if cfg.HTTPTLSEnabled {
		httpScheme = "https"
	}
	log.Printf("Starting Orchestrator HTTP on %s://0.0.0.0%s", httpScheme, httpAddr)
	log.Printf("Ready to process calls")

	go func() {
		var serveErr error
		if cfg.HTTPTLSEnabled {
			serveErr = httpSrv.ListenAndServeTLS(cfg.HTTPTLSCertFile, cfg.HTTPTLSKeyFile)
		} else {
			serveErr = httpSrv.ListenAndServe()
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			log.Fatalf("Failed to start HTTP server: %v", serveErr)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Fatal("HTTP server forced to shutdown:", err)
	}

	log.Println("Orchestrator exited")
}

func setupRouter(
	h *handlers.ProcessHandler,
	auth *handlers.AuthHandler,
	authMw gin.HandlerFunc,
	adminMw gin.HandlerFunc,
	cfg *config.Config,
) *gin.Engine {
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.DebugMode)
	}

	router := gin.Default()
	router.Use(gin.Recovery())
	router.Use(corsMiddleware(cfg.CORSAllowedOrigins))
	router.Use(requestIDMiddleware())
	router.MaxMultipartMemory = 100 << 20

	router.GET("/", func(c *gin.Context) {
		c.File("./web/index.html")
	})
	router.GET("/api/info", h.Root)
	router.GET("/health", h.Health)

	router.POST("/api/v1/auth/login", auth.Login)
	router.POST("/api/v1/auth/register", auth.Register)

	api := router.Group("/api/v1")
	api.Use(authMw)
	{
		api.GET("/auth/me", auth.Me)
		api.POST("/process-call", h.ProcessCall)
		api.GET("/calls", h.ListCalls)
		api.POST("/routing-review", h.ResolveRoutingReview)
		api.GET("/app-settings", h.GetAppSettings)
		api.GET("/routing-config", h.GetRoutingConfig)
		api.POST("/routing-feedback", h.SaveRoutingFeedback)
		api.GET("/routing-model/status", h.GetRoutingModelStatus)

		admin := api.Group("")
		admin.Use(adminMw)
		{
			admin.DELETE("/calls", h.ClearCalls)
			admin.DELETE("/calls/:id", h.DeleteCall)
			admin.PUT("/app-settings", h.UpdateAppSettings)
			admin.GET("/audit/events", h.ListAuditEvents)
			admin.GET("/users", auth.ListUsers)
			admin.POST("/users", auth.CreateUser)
			admin.POST("/users/:id/approve", auth.ApproveUser)
			admin.POST("/users/:id/deactivate", auth.DeactivateUser)
			admin.DELETE("/users/:id", auth.DeleteUser)
		}
	}

	return router
}

func corsMiddleware(allowedOriginsRaw string) gin.HandlerFunc {
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

func setSecurityHeaders(c *gin.Context) {
	c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
	c.Writer.Header().Set("X-Frame-Options", "DENY")
	c.Writer.Header().Set("Referrer-Policy", "no-referrer")
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID()
		}

		c.Set("request_id", requestID)
		c.Writer.Header().Set("X-Request-ID", requestID)
		c.Next()
	}
}

func generateRequestID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
