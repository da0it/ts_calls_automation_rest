// pkg/config/config.go
package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config — структура, в которую собираются все настройки orchestrator-сервиса.
type Config struct {
	// Порт HTTP-сервера orchestrator.
	HTTPPort string

	// Настройки TLS для HTTP-сервера.
	HTTPTLSEnabled  bool
	HTTPTLSCertFile string
	HTTPTLSKeyFile  string

	// Список разрешенных origin для браузерных запросов.
	CORSAllowedOrigins string

	// HTTP-адреса зависимых сервисов.
	TranscriptionServiceURL string
	RoutingServiceURL       string
	TicketServiceURL        string

	// Таймаут HTTP-запроса к ticket-сервису.
	TicketRequestTimeoutSeconds int

	// Адрес сервиса извлечения сущностей.
	EntityServiceURL string

	// Порог уверенности маршрутизации, ниже которого звонок уходит на review.
	RoutingReviewConfidenceThreshold float64

	// Пути к конфигурационным файлам маршрутизации.
	RoutingIntentsPath string
	RoutingGroupsPath  string

	// Путь к файлу обратной связи по маршрутизации.
	RoutingFeedbackPath string

	// Флаг автоматической передачи накопленного feedback в router.
	RoutingAutoLearn bool

	// Лимит количества накопленных записей feedback для auto-learn.
	RoutingAutoLearnLimit int

	// Настройки административного API router-сервиса.
	RouterAdminURL            string
	RouterAdminToken          string
	RouterAdminTimeoutSeconds int

	// Настройки аутентификации и базы данных.
	DatabaseURL    string
	JWTSecret      string
	JWTExpiryHours int
	AdminUsername  string
	AdminPassword  string
}

// Load загружает переменные окружения и возвращает заполненную конфигурацию orchestrator.
func Load() *Config {
	_ = godotenv.Load()

	cfg := &Config{
		HTTPPort:                         getEnv("HTTP_PORT", getEnv("SERVER_PORT", "8000")),
		HTTPTLSEnabled:                   getEnvBool("HTTP_TLS_ENABLED", false),
		HTTPTLSCertFile:                  getEnv("HTTP_TLS_CERT_FILE", ""),
		HTTPTLSKeyFile:                   getEnv("HTTP_TLS_KEY_FILE", ""),
		CORSAllowedOrigins:               getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:8000,http://localhost:3000"),
		TranscriptionServiceURL:          getEnv("TRANSCRIPTION_SERVICE_URL", "http://localhost:8083"),
		RoutingServiceURL:                getEnv("ROUTING_SERVICE_URL", "http://localhost:8081"),
		TicketServiceURL:                 getEnv("TICKET_SERVICE_URL", "http://localhost:8080"),
		TicketRequestTimeoutSeconds:      getEnvInt("TICKET_REQUEST_TIMEOUT_SECONDS", 300),
		EntityServiceURL:                 getEnv("ENTITY_SERVICE_URL", "http://localhost:5001"),
		RoutingReviewConfidenceThreshold: getEnvFloat("ROUTING_REVIEW_CONFIDENCE_THRESHOLD", getEnvFloat("ROUTER_MIN_CONFIDENCE", 0.8)),
		RoutingIntentsPath:               getEnv("ROUTING_INTENTS_PATH", "../router/configs/intents.json"),
		RoutingGroupsPath:                getEnv("ROUTING_GROUPS_PATH", "../router/configs/groups.json"),
		RoutingFeedbackPath:              getEnv("ROUTING_FEEDBACK_PATH", "./data/routing_feedback.jsonl"),
		RoutingAutoLearn:                 getEnv("ROUTING_AUTO_LEARN", "1") == "1",
		RoutingAutoLearnLimit:            getEnvInt("ROUTING_AUTO_LEARN_LIMIT", 50),
		RouterAdminURL:                   getEnv("ROUTER_ADMIN_URL", "http://localhost:8082"),
		RouterAdminToken:                 getEnv("ROUTER_ADMIN_TOKEN", ""),
		RouterAdminTimeoutSeconds:        getEnvInt("ROUTER_ADMIN_TIMEOUT_SECONDS", 600),
		DatabaseURL:                      getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/tickets?sslmode=disable"),
		JWTSecret:                        getEnv("JWT_SECRET", ""),
		JWTExpiryHours:                   getEnvInt("JWT_EXPIRY_HOURS", 24),
		AdminUsername:                    getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword:                    getEnv("ADMIN_PASSWORD", ""),
	}

	if cfg.JWTSecret == "" {
		log.Fatal("JWT_SECRET is required")
	}

	logConfig(cfg)
	return cfg
}

// Печатает основные параметры orchestrator при запуске приложения.
func logConfig(cfg *Config) {
	log.Println("Orchestrator config loaded:")
	items := []struct {
		name  string
		value interface{}
	}{
		{"HTTP port", cfg.HTTPPort},
		{"HTTP TLS enabled", cfg.HTTPTLSEnabled},
		{"CORS allowed origins", cfg.CORSAllowedOrigins},
		{"Transcription service URL", cfg.TranscriptionServiceURL},
		{"Routing service URL", cfg.RoutingServiceURL},
		{"Ticket service URL", cfg.TicketServiceURL},
		{"Ticket request timeout (sec)", cfg.TicketRequestTimeoutSeconds},
		{"Entity service URL", cfg.EntityServiceURL},
		{"Routing review confidence threshold", cfg.RoutingReviewConfidenceThreshold},
		{"Routing intents path", cfg.RoutingIntentsPath},
		{"Routing groups path", cfg.RoutingGroupsPath},
		{"Routing feedback path", cfg.RoutingFeedbackPath},
		{"Routing auto learn", cfg.RoutingAutoLearn},
		{"Routing auto learn limit", cfg.RoutingAutoLearnLimit},
		{"Router admin URL", cfg.RouterAdminURL},
		{"Router admin timeout (sec)", cfg.RouterAdminTimeoutSeconds},
		{"Database URL", cfg.DatabaseURL},
		{"JWT expiry hours", cfg.JWTExpiryHours},
		{"Admin username", cfg.AdminUsername},
	}
	for _, item := range items {
		log.Printf("  - %s: %v", item.name, item.value)
	}
}

// getEnv читает строковую переменную окружения и возвращает значение по умолчанию, если она пуста.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt читает переменную окружения как целое число.
func getEnvInt(key string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err == nil && parsed > 0 {
		return parsed
	}
	return defaultValue
}

// getEnvFloat читает переменную окружения как число с плавающей точкой.
func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err == nil {
			return parsed
		}
	}
	return defaultValue
}

// getEnvBool читает переменную окружения как bool.
func getEnvBool(key string, defaultValue bool) bool {
	value := strings.TrimSpace(strings.ToLower(getEnv(key, "")))
	if value == "" {
		return defaultValue
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultValue
	}
}
