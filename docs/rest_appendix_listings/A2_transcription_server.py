# Листинг А.2. REST-сервис транскрибации аудиозаписи.

#!/usr/bin/env python

# HTTP-сервер сервиса транскрибации.
# Принимает сырые байты аудио в теле запроса, сохраняет их во временный файл,
# запускает WhisperX pipeline и возвращает результат в JSON-формате.

from __future__ import annotations

import json
import logging
import os
import tempfile
import time
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from transcribe_logic.pipeline import transcribe_with_roles

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger(__name__)


# Читает переменную окружения и преобразует ее к bool-значению.
def _env_bool(name: str, default: bool = False) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


# Сервис транскрибации управляет прогревом WhisperX и обработкой HTTP-запросов на распознавание аудио
class TranscriptionService:
    def __init__(self) -> None:
        logger.info("Initializing TranscriptionService")
        self._maybe_warmup_whisperx()

    # При включенном WHISPERX_PRELOAD заранее загружает модель, чтобы сократить задержку выполнения первого запроса
    def _maybe_warmup_whisperx(self) -> None:
        preload = _env_bool("WHISPERX_PRELOAD", False)
        if not preload:
            return

    # Выполняет транскрибацию одного аудиофайла и возвращает JSON-совместимую структуру transcript
    def transcribe_audio(self, *, audio: bytes, filename: str, call_id: str) -> dict:
        if not audio:
            raise ValueError("audio is required")

        start_time = time.time()
        file_ext = Path(filename).suffix.lower() or ".mp3"
        logger.info("Received request: call_id=%s ext=%s bytes=%d", call_id, file_ext, len(audio))

        temp_file = None
        try:
            # Для WhisperX байты запроса сначала сохраняются во временный файл.
            with tempfile.NamedTemporaryFile(delete=False, suffix=file_ext) as tmp:
                tmp.write(audio)
                temp_file = tmp.name

            result = transcribe_with_roles(audio_path=temp_file)
            transcript = {
                "call_id": call_id,
                "segments": [
                    {
                        "start": float(seg.get("start", 0.0)),
                        "end": float(seg.get("end", 0.0)),
                        "speaker": str(seg.get("speaker") or ""),
                        "role": str(seg.get("role") or ""),
                        "text": str(seg.get("text") or ""),
                    }
                    for seg in result.get("segments", [])
                ],
                "role_mapping": result.get("role_mapping", {}) or {},
                "metadata": {
                    "mode": result.get("mode", "whisperx"),
                    "input": result.get("input", ""),
                    "note": result.get("note", ""),
                    "processing_time_seconds": str(round(time.time() - start_time, 2)),
                },
            }
            return transcript
        finally:

            # Временный файл удаляется вне зависимости от результата транскрибации
            if temp_file and os.path.exists(temp_file):
                os.unlink(temp_file)


# Фабрика HTTP-обработчика transcription-сервиса.
def make_http_handler(service: TranscriptionService):
    class TranscriptionHTTPHandler(BaseHTTPRequestHandler):
        # Возвращает состояние сервиса.
        def do_GET(self) -> None:
            if self.path == "/health":
                _json_response(self, HTTPStatus.OK, service.health_status())
                return
            _json_response(self, HTTPStatus.NOT_FOUND, {"error": "not found"})

        # Принимает аудиоданные и запускает транскрибацию.
        def do_POST(self) -> None:
            if self.path != "/api/transcribe":
                _json_response(self, HTTPStatus.NOT_FOUND, {"error": "not found"})
                return
