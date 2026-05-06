#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PYTHON_BIN="${PYTHON_BIN:-python3}"
WHISPERX_VENV_DIR="${WHISPERX_VENV_DIR:-$HOME/whisperx_venv}"
ENTITY_VENV_DIR="${ENTITY_VENV_DIR:-$ROOT_DIR/.venv}"
ROUTER_VENV_DIR="${ROUTER_VENV_DIR:-$ROOT_DIR/services/router/venv}"

# INSTALL_SYSTEM_DEPS=1 пытается установить системные пакеты через apt.
INSTALL_SYSTEM_DEPS="${INSTALL_SYSTEM_DEPS:-0}"

# PREPARE_ONLINE_MODE=1 временно включает онлайн-режим для первой загрузки моделей.
PREPARE_ONLINE_MODE="${PREPARE_ONLINE_MODE:-1}"

# PREPARE_ENTITY_NER_STARTUP=1 разрешает entity_extraction скачать и установить NER-модель при первом старте.
PREPARE_ENTITY_NER_STARTUP="${PREPARE_ENTITY_NER_STARTUP:-1}"

# CHECK_LOCAL_SERVICES=1 проверяет доступность PostgreSQL и Ollama.
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

prepare_linux_env_files() {
  local ffmpeg_bin
  local ffprobe_bin

  ffmpeg_bin="$(command -v ffmpeg)"
  ffprobe_bin="$(command -v ffprobe)"

  log "Normalizing Linux env paths..."
  set_env_value "$ROOT_DIR/configs/transcription.env" "FFMPEG_BIN" "$ffmpeg_bin"
  set_env_value "$ROOT_DIR/configs/transcription.env" "FFPROBE_BIN" "$ffprobe_bin"

  if [[ "$PREPARE_ONLINE_MODE" == "1" ]]; then
    log "Enabling online mode for the first model download..."
    set_env_value "$ROOT_DIR/configs/transcription.env" "HF_HUB_OFFLINE" "0"
    set_env_value "$ROOT_DIR/configs/transcription.env" "TRANSFORMERS_OFFLINE" "0"
    set_env_value "$ROOT_DIR/configs/routing.env" "HF_HUB_OFFLINE" "0"
    set_env_value "$ROOT_DIR/configs/routing.env" "TRANSFORMERS_OFFLINE" "0"
    set_env_value "$ROOT_DIR/configs/entity.env" "HF_HUB_OFFLINE" "0"
    set_env_value "$ROOT_DIR/configs/entity.env" "TRANSFORMERS_OFFLINE" "0"
  fi

  if [[ "$PREPARE_ENTITY_NER_STARTUP" == "1" ]]; then
    log "Allowing entity_extraction to install and download NER assets on startup..."
    set_env_value "$ROOT_DIR/configs/entity.env" "ENTITY_NER_DOWNLOAD_ON_STARTUP" "1"
    set_env_value "$ROOT_DIR/configs/entity.env" "ENTITY_NER_INSTALL_ON_STARTUP" "1"
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
  log "Next: run the local stack launcher for the REST repo."
}

main "$@"
