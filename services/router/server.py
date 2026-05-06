from __future__ import annotations

import json
import logging
import os
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from threading import RLock
from typing import Any, Dict, List

from routing.ai_analyzer import CallIntentAnalyzer
from routing.models import CallInput, Segment

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logger = logging.getLogger("router-http")
SPAM_INTENT_IDS = {"spam.call", "spam"}


# Загружает словарь доступных интентов из JSON-конфига router.
def load_intents(intents_path: Path) -> Dict[str, Dict[str, Any]]:
    with intents_path.open("r", encoding="utf-8") as file:
        payload = json.load(file)
    if not isinstance(payload, dict):
        raise ValueError("intents payload must be a JSON object")
    return payload


# Приводит строковую переменную окружения к bool-значению.
def _env_bool(name: str, default: bool = False) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


# Нормализует дополнительный spam payload, если анализатор вернул его в raw-данных.
# В одноступенчатом контуре это поле обычно не требуется, но сохранено для совместимости UI и review-логики.
def _spam_check_payload(raw: Dict[str, Any]) -> Dict[str, Any] | None:
    payload = raw.get("spam_decision") if isinstance(raw, dict) else {}
    if not isinstance(payload, dict) or not payload:
        return None
    return {
        "status": str(payload.get("status") or ""),
        "predicted_label": str(payload.get("predicted_label") or ""),
        "confidence": float(payload.get("confidence") or 0.0),
        "threshold_low": float(payload.get("threshold_low") or 0.0),
        "threshold_high": float(payload.get("threshold_high") or 0.0),
        "reason": str(payload.get("reason") or ""),
        "skipped": bool(payload.get("skipped")),
        "backend": str(payload.get("backend") or ""),
    }


# Исключает спам из доступного набора, когда модуль управления просит повторную маршрутизацию после override.
def _filter_intents_for_request(
    intents: Dict[str, Dict[str, Any]],
    *,
    skip_spam_gate: bool,
) -> Dict[str, Dict[str, Any]]:
    if not skip_spam_gate:
        return intents
    filtered = {
        intent_id: meta
        for intent_id, meta in intents.items()
        if str(intent_id).strip().lower() not in SPAM_INTENT_IDS
    }
    return filtered or intents


# Сервис маршрутизации управляет доступным набором целей обращения и вызывает бизнес-анализатор звонка.
class RoutingService:
    def __init__(
        self,
        intents_path: Path,
        intents: Dict[str, Dict[str, Any]],
        analyzer: CallIntentAnalyzer,
    ) -> None:
        self.intents_path = intents_path
        self.intents = intents
        self._intents_mtime = intents_path.stat().st_mtime if intents_path.exists() else 0.0
        self._lock = RLock()
        self.analyzer = analyzer

    # Возвращает актуальный intents.json и при изменении файла перечитывает его на лету.
    def _get_intents(self) -> Dict[str, Dict[str, Any]]:
        try:
            current_mtime = self.intents_path.stat().st_mtime
        except OSError:
            current_mtime = 0.0

        with self._lock:
            if current_mtime <= self._intents_mtime:
                return self.intents

            try:
                loaded = load_intents(self.intents_path)
            except Exception as exc:
                logger.warning("failed to reload intents from %s: %s", self.intents_path, exc)
                return self.intents

            self.intents = loaded
            self._intents_mtime = current_mtime
            logger.info("reloaded intents config from %s (%d intents)", self.intents_path, len(self.intents))
            return self.intents

    # Преобразует HTTP payload в внутренние сегменты звонка, запускает анализатор и возвращает результат маршрутизации
    def route_segments(
        self,
        *,
        call_id: str,
        segments_payload: List[Dict[str, Any]],
        skip_spam_gate: bool,
    ) -> Dict[str, Any]:
        if not segments_payload:
            raise ValueError("segments are required")

        segments = [
            Segment(
                start=float(seg.get("start", 0.0)),
                end=float(seg.get("end", 0.0)),
                speaker=str(seg.get("speaker") or ""),
                role=(str(seg.get("role")).strip() or None) if seg.get("role") is not None else None,
                text=str(seg.get("text") or ""),
            )
            for seg in segments_payload
        ]

        call = CallInput(
            call_id=call_id or "unknown-call",
            segments=segments,
            meta={},
        )

        analysis = self.analyzer.analyze(
            call,
            _filter_intents_for_request(
                self._get_intents(),
                skip_spam_gate=skip_spam_gate,
            ),
        )

        suggested_group = ""
        for target in analysis.suggested_targets:
            if target.get("type") == "group":
                suggested_group = str(target.get("id", ""))
                break

        priority = str(analysis.priority)
        if priority == "normal":
            priority = "medium"

        payload: Dict[str, Any] = {
            "intent_id": analysis.intent.intent_id,
            "intent_confidence": float(analysis.intent.confidence),
            "priority": priority,
            "suggested_group": suggested_group,
        }
        spam_check = _spam_check_payload(analysis.raw)
        if spam_check is not None:
            payload["spam_check"] = spam_check
        return payload

    # Возвращает административный статус модели и текущего набора целей обращения
    def get_model_status(self) -> Dict[str, Any]:
        intents = self._get_intents()
        tuned_status = self.analyzer.get_training_status(intents)
        return {
            "service": "router-admin",
            "intents_count": len(intents),
            "tuned_model": tuned_status,
        }

    # Краткий health payload для проверки статуса сервиса.
    def health_status(self) -> Dict[str, Any]:
        return {
            "status": "healthy",
            "service": "router",
            "intents_count": len(self._get_intents()),
        }


# Отправляет JSON-ответ клиенту HTTP API.
def _json_response(handler: BaseHTTPRequestHandler, status: int, payload: Dict[str, Any]) -> None:
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json; charset=utf-8")
    handler.send_header("Content-Length", str(len(body)))
    handler.end_headers()
    handler.wfile.write(body)


# Читает JSON-тело запроса и гарантирует, что верхний уровень представлен объектом.
def _read_json_request(handler: BaseHTTPRequestHandler) -> Dict[str, Any]:
    content_length = int(handler.headers.get("Content-Length", "0") or "0")
    body = handler.rfile.read(content_length) if content_length > 0 else b"{}"
    if not body:
        return {}
    payload = json.loads(body.decode("utf-8"))
    if not isinstance(payload, dict):
        raise ValueError("request body must be a JSON object")
    return payload


# Фабрика HTTP-обработчика для пользовательского и административного API router-сервиса.
def make_router_handler(service: RoutingService, admin_token: str):
    class RouterHandler(BaseHTTPRequestHandler):
        # Обрабатывает health-check и административный статус модели.
        def do_GET(self) -> None:
            if self.path == "/health":
                _json_response(self, HTTPStatus.OK, service.health_status())
                return

            if self.path == "/admin/model/status":
                if not self._authorize():
                    return
                _json_response(self, HTTPStatus.OK, service.get_model_status())
                return

            _json_response(self, HTTPStatus.NOT_FOUND, {"error": "not found"})

        # Принимает HTTP-запрос на маршрутизацию звонка.
        def do_POST(self) -> None:
            if self.path != "/api/route":
                _json_response(self, HTTPStatus.NOT_FOUND, {"error": "not found"})
                return

            try:
                payload = _read_json_request(self)
                routing = service.route_segments(
                    call_id=str(payload.get("call_id") or "unknown-call"),
                    segments_payload=payload.get("segments") or [],
                    skip_spam_gate=bool(payload.get("skip_spam_gate")),
                )
                _json_response(self, HTTPStatus.OK, {"routing": routing})
            except ValueError as exc:
                _json_response(self, HTTPStatus.BAD_REQUEST, {"error": str(exc)})
            except Exception as exc:
                logger.exception("routing request failed")
                _json_response(self, HTTPStatus.INTERNAL_SERVER_ERROR, {"error": f"routing failed: {exc}"})

        # Отвечает на preflight-запросы браузера.
        def do_OPTIONS(self) -> None:
            self.send_response(HTTPStatus.NO_CONTENT)
            self.send_header("Access-Control-Allow-Origin", "*")
            self.send_header("Access-Control-Allow-Headers", "Content-Type, Authorization")
            self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
            self.end_headers()

        def log_message(self, fmt: str, *args) -> None:
            logger.info("http %s - %s", self.address_string(), fmt % args)

        # Проверяет Bearer token для административных запросов.
        def _authorize(self) -> bool:
            if not admin_token:
                return True
            auth_header = self.headers.get("Authorization", "")
            bearer = ""
            if auth_header.startswith("Bearer "):
                bearer = auth_header[len("Bearer ") :].strip()
            if bearer == admin_token:
                return True
            _json_response(self, HTTPStatus.UNAUTHORIZED, {"error": "unauthorized"})
            return False

    return RouterHandler


# Точка входа HTTP-сервера router: читает конфигурацию, собирает анализатор и запускает API
def serve() -> None:
    http_port = os.getenv("ROUTER_HTTP_PORT", "8081")
    model_name = os.getenv("ROUTER_MODEL_NAME", "ai-forever/ruBert-base")
    min_confidence = float(os.getenv("ROUTER_MIN_CONFIDENCE", "0.55"))
    intents_path = Path(
        os.getenv("ROUTER_INTENTS_PATH", str(Path(__file__).parent / "configs" / "intents.json"))
    )

    finetuned_enabled = os.getenv("ROUTER_FINETUNED_ENABLED", "1").strip().lower() in {"1", "true", "yes", "on"}
    default_finetuned_model_path = str(Path(__file__).parent / "configs" / "router_finetuned_model")
    finetuned_model_path = os.getenv("ROUTER_FINETUNED_MODEL_PATH", default_finetuned_model_path)
    tuned_model_path = os.getenv("ROUTER_TUNED_MODEL_PATH", str(Path(finetuned_model_path) / "router_tuned_head.pt"))
    finetuned_learning_rate = float(os.getenv("ROUTER_FINETUNED_LR", "2e-6"))
    finetuned_epochs = int(os.getenv("ROUTER_FINETUNED_EPOCHS", "100"))
    finetuned_batch_size = int(os.getenv("ROUTER_FINETUNED_BATCH_SIZE", "16"))
    finetuned_max_length = int(os.getenv("ROUTER_FINETUNED_MAX_LENGTH", "512"))
    finetuned_weight_decay = float(os.getenv("ROUTER_FINETUNED_WEIGHT_DECAY", "0.01"))
    base_dataset_path = os.getenv("ROUTER_BASE_DATASET_PATH", "").strip()
    include_intent_examples = _env_bool("ROUTER_INCLUDE_INTENT_EXAMPLES", True)
    nlp_text_mode = os.getenv("ROUTER_NLP_TEXT_MODE", "canonical").strip().lower() or "canonical"
    admin_token = os.getenv("ROUTER_ADMIN_TOKEN", "").strip()

    intents = load_intents(intents_path)
    logger.info("loaded intents config from %s (%d intents)", intents_path, len(intents))
    analyzer = CallIntentAnalyzer(
        model_name=model_name,
        min_confidence=min_confidence,
        tuned_model_path=tuned_model_path,
        finetuned_enabled=finetuned_enabled,
        finetuned_model_path=finetuned_model_path,
        finetuned_learning_rate=finetuned_learning_rate,
        finetuned_epochs=finetuned_epochs,
        finetuned_batch_size=finetuned_batch_size,
        finetuned_max_length=finetuned_max_length,
        finetuned_weight_decay=finetuned_weight_decay,
        base_dataset_path=base_dataset_path,
        include_intent_examples=include_intent_examples,
        nlp_text_mode=nlp_text_mode,
    )

    routing_service = RoutingService(
        intents_path=intents_path,
        intents=intents,
        analyzer=analyzer,
    )

    addr = ("0.0.0.0", int(http_port))
    server = ThreadingHTTPServer(addr, make_router_handler(routing_service, admin_token))
    logger.info("Routing HTTP server listening on http://%s:%s", addr[0], http_port)
    try:
        server.serve_forever()
    finally:
        server.server_close()


if __name__ == "__main__":
    serve()
