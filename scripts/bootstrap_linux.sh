#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PYTHON_BIN="${PYTHON_BIN:-python3}"
WHISPERX_VENV_DIR="${WHISPERX_VENV_DIR:-$HOME/whisperx_venv}"
ENTITY_VENV_DIR="${ENTITY_VENV_DIR:-$ROOT_DIR/.venv}"
ROUTER_VENV_DIR="${ROUTER_VENV_DIR:-$ROOT_DIR/services/router/venv}"
CONFIGS_DIR="${CONFIGS_DIR:-$ROOT_DIR/configs}"

INSTALL_SYSTEM_DEPS="${INSTALL_SYSTEM_DEPS:-0}"
PREPARE_ONLINE_MODE="${PREPARE_ONLINE_MODE:-1}"
PREPARE_ENTITY_NER_STARTUP="${PREPARE_ENTITY_NER_STARTUP:-1}"
CHECK_LOCAL_SERVICES="${CHECK_LOCAL_SERVICES:-1}"

log() {
  echo "[$(date '+%H:%M:%S')] $*"
}

warn() {
  echo "[WARN] $*" >&2
}

fail() {
  echo "[ERROR] $*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    fail "Missing command: $cmd"
  fi
}

run_privileged() {
  if [[ "$(id -u)" -eq 0 ]]; then
    "$@"
    return
  fi
  if command -v sudo >/dev/null 2>&1; then
    sudo "$@"
    return
  fi
  fail "Need root privileges for: $*"
}

install_system_deps() {
  if [[ "$INSTALL_SYSTEM_DEPS" != "1" ]]; then
    return
  fi

  if ! command -v apt-get >/dev/null 2>&1; then
    fail "INSTALL_SYSTEM_DEPS=1 is only supported for apt-based Linux distributions"
  fi

  log "Installing system packages via apt..."
  run_privileged apt-get update
  run_privileged apt-get install -y \
    git curl ffmpeg postgresql postgresql-contrib \
    python3 python3-venv python3-pip \
    build-essential pkg-config libsndfile1 golang
}

ensure_venv() {
  local venv_path="$1"
  "$PYTHON_BIN" -m venv "$venv_path"
}

pip_install() {
  local pip_bin="$1"
  shift
  "$pip_bin" install --upgrade pip setuptools wheel >/dev/null
  "$pip_bin" install "$@"
}

write_if_missing() {
  local path="$1"
  local content="$2"
  if [[ -f "$path" ]]; then
    return
  fi
  printf '%s' "$content" >"$path"
}

set_env_value() {
  local env_file="$1"
  local key="$2"
  local value="$3"

  if [[ ! -f "$env_file" ]]; then
    fail "Env file not found: $env_file"
  fi

  if grep -q "^${key}=" "$env_file"; then
    sed -i.bak "s|^${key}=.*|${key}=${value}|" "$env_file"
  else
    printf '\n%s=%s\n' "$key" "$value" >>"$env_file"
  fi
}

ensure_configs() {
  log "Ensuring local configs exist..."
  mkdir -p "$CONFIGS_DIR"

  write_if_missing "$CONFIGS_DIR/transcription.env" 'HTTP_HOST=0.0.0.0
TRANSCRIPTION_HTTP_PORT=8083
WHISPERX_MODEL=large-v3
WHISPERX_LANGUAGE=ru
WHISPERX_DEVICE=auto
WHISPERX_COMPUTE_TYPE=int8
WHISPERX_BATCH_SIZE=1
WHISPERX_VAD_METHOD=silero
WHISPERX_PRELOAD=1
HF_HUB_OFFLINE=1
TRANSFORMERS_OFFLINE=1
HF_TOKEN=
FFMPEG_BIN=/usr/bin/ffmpeg
FFPROBE_BIN=/usr/bin/ffprobe
'

  write_if_missing "$CONFIGS_DIR/routing.env" 'ROUTER_HTTP_PORT=8081
ROUTER_MODEL_NAME=ai-forever/ruRoberta-large
ROUTER_MIN_CONFIDENCE=0.50
ROUTER_INTENTS_PATH=./configs/routing_intents.json
ROUTER_FEEDBACK_PATH=./configs/routing_feedback.jsonl
ROUTER_BASE_DATASET_PATH=
ROUTER_INCLUDE_INTENT_EXAMPLES=1
ROUTER_TUNED_MODEL_PATH=./configs/router_tuned_head.pt
ROUTER_ADMIN_TOKEN=
ROUTER_FINETUNED_ENABLED=1
ROUTER_FINETUNED_MODEL_PATH=./configs/router_finetuned_model
ROUTER_FINETUNED_LR=2e-5
ROUTER_FINETUNED_EPOCHS=50
ROUTER_FINETUNED_BATCH_SIZE=8
ROUTER_FINETUNED_MAX_LENGTH=512
ROUTER_FINETUNED_WEIGHT_DECAY=0.01
ROUTER_NLP_TEXT_MODE=tokens
ROUTER_TRAIN_EPOCHS=50
ROUTER_TRAIN_BATCH_SIZE=16
ROUTER_TRAIN_LR=2e-5
ROUTER_TRAIN_VAL_RATIO=0.2
ROUTER_TRAIN_SEED=42
HF_HUB_OFFLINE=1
TRANSFORMERS_OFFLINE=1
'

  write_if_missing "$CONFIGS_DIR/entity.env" 'HF_HUB_OFFLINE=1
TRANSFORMERS_OFFLINE=1
ENTITY_USE_NER=1
ENTITY_NER_DOWNLOAD_ON_STARTUP=0
ENTITY_NER_INSTALL_ON_STARTUP=0
'

  write_if_missing "$CONFIGS_DIR/ticket.env" 'SERVER_PORT=8080
CORS_ALLOWED_ORIGINS=http://localhost:8000,http://localhost:3000

DATABASE_URL=postgres://postgres:postgres@localhost:5432/tickets?sslmode=disable
PYTHON_NER_SERVICE_URL=http://localhost:5001
LLM_REQUEST_TIMEOUT_SECONDS=180
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=gemma3:4b
OLLAMA_TEMPERATURE=0
OLLAMA_NUM_PREDICT=48

TICKET_SYSTEM=mock

SIMPLEONE_ENDPOINT_URL=https://test-arenadata.simpleone.ru/v1/api/itsm_itsm/integrations/v1/ticket_ingest
SIMPLEONE_BEARER_TOKEN=
SIMPLEONE_TIMEOUT_SECONDS=30
TICKET_INCLUDE_PII_IN_DESCRIPTION=1
'

  write_if_missing "$CONFIGS_DIR/orchestrator.env" 'HTTP_PORT=8000
HTTP_TLS_ENABLED=0
HTTP_TLS_CERT_FILE=
HTTP_TLS_KEY_FILE=
CORS_ALLOWED_ORIGINS=http://localhost:8000,http://localhost:3000

TRANSCRIPTION_SERVICE_URL=http://localhost:8083
ROUTING_SERVICE_URL=http://localhost:8081
TICKET_SERVICE_URL=http://localhost:8080
TICKET_REQUEST_TIMEOUT_SECONDS=300
ENTITY_SERVICE_URL=http://localhost:5001
TRANSCRIPTION_HTTP_TIMEOUT_SECONDS=2400
ROUTING_REVIEW_CONFIDENCE_THRESHOLD=0.80
ROUTING_INTENTS_PATH=../../configs/routing_intents.json
ROUTING_GROUPS_PATH=../../configs/routing_groups.json
ROUTING_FEEDBACK_PATH=../../configs/routing_feedback.jsonl
ROUTING_AUTO_LEARN=1
ROUTING_AUTO_LEARN_LIMIT=50
ROUTER_ADMIN_URL=http://localhost:8081
ROUTER_ADMIN_TIMEOUT_SECONDS=600
ORCH_DELETE_UPLOADED_AUDIO_AFTER_PROCESS=1
DATABASE_URL=postgres://postgres:postgres@localhost:5432/tickets?sslmode=disable
JWT_SECRET=change-me-in-production-32chars
JWT_EXPIRY_HOURS=24
ADMIN_USERNAME=admin
ADMIN_PASSWORD=admin123
'

  if [[ ! -f "$CONFIGS_DIR/routing_intents.json" ]]; then
    cp "$ROOT_DIR/services/router/configs/intents.json" "$CONFIGS_DIR/routing_intents.json"
  fi
  if [[ ! -f "$CONFIGS_DIR/routing_groups.json" ]]; then
    cp "$ROOT_DIR/services/router/configs/groups.json" "$CONFIGS_DIR/routing_groups.json"
  fi
  touch "$CONFIGS_DIR/routing_feedback.jsonl"
}

prepare_linux_env_files() {
  local ffmpeg_bin
  local ffprobe_bin

  ffmpeg_bin="$(command -v ffmpeg)"
  ffprobe_bin="$(command -v ffprobe)"

  log "Normalizing Linux env paths..."
  set_env_value "$CONFIGS_DIR/transcription.env" "FFMPEG_BIN" "$ffmpeg_bin"
  set_env_value "$CONFIGS_DIR/transcription.env" "FFPROBE_BIN" "$ffprobe_bin"

  if [[ "$PREPARE_ONLINE_MODE" == "1" ]]; then
    log "Enabling online mode for the first model download..."
    set_env_value "$CONFIGS_DIR/transcription.env" "HF_HUB_OFFLINE" "0"
    set_env_value "$CONFIGS_DIR/transcription.env" "TRANSFORMERS_OFFLINE" "0"
    set_env_value "$CONFIGS_DIR/routing.env" "HF_HUB_OFFLINE" "0"
    set_env_value "$CONFIGS_DIR/routing.env" "TRANSFORMERS_OFFLINE" "0"
    set_env_value "$CONFIGS_DIR/entity.env" "HF_HUB_OFFLINE" "0"
    set_env_value "$CONFIGS_DIR/entity.env" "TRANSFORMERS_OFFLINE" "0"
  fi

  if [[ "$PREPARE_ENTITY_NER_STARTUP" == "1" ]]; then
    log "Allowing entity_extraction to install and download NER assets on startup..."
    set_env_value "$CONFIGS_DIR/entity.env" "ENTITY_NER_DOWNLOAD_ON_STARTUP" "1"
    set_env_value "$CONFIGS_DIR/entity.env" "ENTITY_NER_INSTALL_ON_STARTUP" "1"
  fi
}

check_postgres() {
  if [[ "$CHECK_LOCAL_SERVICES" != "1" ]]; then
    return
  fi

  if ! command -v psql >/dev/null 2>&1; then
    warn "psql not found. PostgreSQL is required before local launch"
    return
  fi

  local db_url
  db_url="postgres://postgres:postgres@localhost:5432/tickets?sslmode=disable"
  if psql "$db_url" -c 'select 1;' >/dev/null 2>&1; then
    log "PostgreSQL check passed."
  else
    warn "PostgreSQL is not reachable at $db_url"
    warn "Create the database before launch, for example:"
    warn "  sudo systemctl enable --now postgresql"
    warn "  sudo -u postgres psql -c \"ALTER USER postgres PASSWORD 'postgres';\""
    warn "  sudo -u postgres createdb tickets"
  fi
}

check_ollama() {
  if [[ "$CHECK_LOCAL_SERVICES" != "1" ]]; then
    return
  fi

  if ! command -v curl >/dev/null 2>&1; then
    warn "curl not found. Skipping Ollama health check."
    return
  fi

  if curl -fsS http://localhost:11434/api/tags >/dev/null 2>&1; then
    log "Ollama check passed."
  else
    warn "Ollama is not reachable at http://localhost:11434"
    warn "Start it before launch and pull the model from configs/ticket.env:"
    warn "  ollama serve"
    warn "  ollama pull gemma3:4b"
  fi
}

main() {
  install_system_deps

  require_cmd "$PYTHON_BIN"
  require_cmd git
  require_cmd go
  require_cmd ffmpeg
  require_cmd ffprobe

  ensure_configs
  prepare_linux_env_files

  log "Preparing Python venv for entity_extraction..."
  ensure_venv "$ENTITY_VENV_DIR"
  pip_install "$ENTITY_VENV_DIR/bin/pip" -r "$ROOT_DIR/services/entity_extraction/requirements.txt"

  log "Preparing Python venv for router..."
  ensure_venv "$ROUTER_VENV_DIR"
  pip_install "$ROUTER_VENV_DIR/bin/pip" -r "$ROOT_DIR/services/router/requirements.txt"

  log "Preparing Python venv for transcription (WhisperX)..."
  ensure_venv "$WHISPERX_VENV_DIR"
  pip_install "$WHISPERX_VENV_DIR/bin/pip" whisperx fastapi uvicorn grpcio protobuf python-dotenv

  log "Downloading Go modules..."
  (
    cd "$ROOT_DIR/services/orchestrator"
    go mod download
  )
  (
    cd "$ROOT_DIR/services/ticket_creation"
    go mod download
  )

  check_postgres
  check_ollama

  log "Bootstrap complete."
  log "Next: run bash $ROOT_DIR/scripts/run_all.sh"
}

main "$@"
