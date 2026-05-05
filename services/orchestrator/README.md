# Orchestrator Service

Главный сервис, который оркестрирует цепочку:

`Аудио -> Транскрибация+диаризация -> Маршрутизация -> Извлечение сущностей -> Формирование тикета`

## Транспорт

- HTTP API: `POST /api/v1/process-call`
- Внутренние вызовы между сервисами тоже выполняются по HTTP

## Внутренние зависимости

Orchestrator вызывает по HTTP:

- `transcription` (`TRANSCRIPTION_SERVICE_URL`, default `http://localhost:8083`)
- `router` (`ROUTING_SERVICE_URL`, default `http://localhost:8081`)
- `ticket_creation` (`TICKET_SERVICE_URL`, default `http://localhost:8080`)
- `entity_extraction` (`ENTITY_SERVICE_URL`, default `http://localhost:5001`)

## Локальный запуск

```bash
cd services/orchestrator
go mod download
go run cmd/server/main.go
```

Порт:

- HTTP: `HTTP_PORT` (default `8000`)
