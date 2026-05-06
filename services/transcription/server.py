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

try:
    from transcribe_logic.whisperx_runtime import warmup_whisperx_runtime
except ImportError:
    warmup_whisperx_runtime = None

try:
    from transcribe_logic.config import get_whisperx_device_from_env
except ImportError:
    get_whisperx_device_from_env = None

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


# Сервис транскрибации управляет прогревом WhisperX и обработкой HTTP-запросов на распознавание аудио.
class TranscriptionService:
    def __init__(self) -> None:
        logger.info("Initializing TranscriptionService")
        self._maybe_warmup_whisperx()

    # При включенном WHISPERX_PRELOAD заранее загружает модель, чтобы сократить задержку первого запроса.
    def _maybe_warmup_whisperx(self) -> None:
        preload = _env_bool("WHISPERX_PRELOAD", False)
        if not preload:
            return
        if warmup_whisperx_runtime is None:
            logger.warning("WHISPERX_PRELOAD=1 but warmup_whisperx_runtime import failed.")
            return

        try:
            device = (
                get_whisperx_device_from_env()
                if get_whisperx_device_from_env is not None
                else os.getenv("WHISPERX_DEVICE", "auto")
            )
            logger.info("WhisperX preload enabled: warming up runtime on device=%s...", device)
            warmup_whisperx_runtime(
                model=os.getenv("WHISPERX_MODEL", "large-v3"),
                language=os.getenv("WHISPERX_LANGUAGE", "ru"),
                device=device,
                compute_type=os.getenv("WHISPERX_COMPUTE_TYPE", "int8"),
                vad_method=os.getenv("WHISPERX_VAD_METHOD", "silero").strip().lower(),
            )
            logger.info("WhisperX preload completed.")
        except Exception as exc:
            logger.warning("WhisperX preload failed, continuing without warmup: %s", exc)

    # Выполняет транскрибацию одного аудиофайла и возвращает JSON-совместимую структуру transcript.
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
            logger.info("Temporary audio file created for call_id=%s", call_id)

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
            logger.info(
                "Transcription completed in %.2fs, segments: %d",
                time.time() - start_time,
                len(transcript["segments"]),
            )
            return transcript
        finally:
            # Временный файл удаляется вне зависимости от результата транскрибации.
            if temp_file and os.path.exists(temp_file):
                os.unlink(temp_file)
                logger.debug("Temporary audio file deleted for call_id=%s", call_id)

    # Возвращает краткий статус сервиса для health-check.
    def health_status(self) -> dict:
        return {"status": "healthy", "service": "transcription"}


# Формирует и отправляет HTTP-ответ в формате JSON.
def _json_response(handler: BaseHTTPRequestHandler, status: int, payload: dict) -> None:
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json; charset=utf-8")
    handler.send_header("Content-Length", str(len(body)))
    handler.end_headers()
    handler.wfile.write(body)


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

            content_length = int(self.headers.get("Content-Length", "0") or "0")
            audio = self.rfile.read(content_length) if content_length > 0 else b""
            filename = self.headers.get("X-Filename", "audio.mp3")
            call_id = self.headers.get("X-Call-ID", "unknown-call")

            try:
                transcript = service.transcribe_audio(audio=audio, filename=filename, call_id=call_id)
                _json_response(self, HTTPStatus.OK, {"transcript": transcript})
            except ValueError as exc:
                _json_response(self, HTTPStatus.BAD_REQUEST, {"error": str(exc)})
            except Exception as exc:
                logger.error("HTTP transcription failed: %s", exc, exc_info=True)
                _json_response(self, HTTPStatus.INTERNAL_SERVER_ERROR, {"error": f"Transcription failed: {exc}"})

        # Отвечает на CORS preflight-запросы.
        def do_OPTIONS(self) -> None:
            self.send_response(HTTPStatus.NO_CONTENT)
            self.send_header("Access-Control-Allow-Origin", "*")
            self.send_header("Access-Control-Allow-Headers", "Content-Type, X-Filename, X-Call-ID")
            self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
            self.end_headers()

        def log_message(self, fmt: str, *args) -> None:
            logger.info("http %s - %s", self.address_string(), fmt % args)

    return TranscriptionHTTPHandler


# Точка входа HTTP-сервера транскрибации.
def serve() -> None:
    host = os.getenv("HTTP_HOST", "0.0.0.0")
    http_port = int(os.getenv("TRANSCRIPTION_HTTP_PORT", "8083"))

    service = TranscriptionService()
    server = ThreadingHTTPServer((host, http_port), make_http_handler(service))
    logger.info("Starting transcription HTTP server on http://%s:%s", host, http_port)
    try:
        server.serve_forever()
    finally:
        server.server_close()


if __name__ == "__main__":
    serve()
