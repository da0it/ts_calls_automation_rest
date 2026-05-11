# Листинг А.4. Классификация и маршрутизация обращения.

from __future__ import annotations

from typing import Any, Dict, Optional
import logging
import time

import torch

from .finetuned_router import FinetunedRouterRuntime
from .models import AIAnalysis, CallInput, IntentResult, Priority
from .nlp_preprocess import PreprocessConfig, build_canonical

logger = logging.getLogger(__name__)
RESERVED_FALLBACK_INTENT_ID = "misc.triage"

# Родительский класс. Задает общий контракт: любой анализатор должен иметь метод Analyze, чтобы можно было изменить метод
# классификации. Например, на классический ML алгоритм, ансамбль моделей
class AIAnalyzer:
    def analyze(
        self,
        call: CallInput,
        allowed_intents: Dict[str, Dict],
        groups: Optional[Dict[str, Dict]] = None,
    ) -> AIAnalysis:
        raise NotImplementedError

# Анализатор целей звонка. Инкапсулирует прикладную логику анализа:
# препроцессинг сегментов, вызов среды выполнения модели и интерпретацию
# результата в терминах intent/priority/targets.
class CallIntentAnalyzer(AIAnalyzer):
    def __init__(
        self,
        model_name: str = "ai-forever/ruBert-base",
        device: Optional[str] = None,
        min_confidence: float = 0.55,
        max_text_chars: int = 4000,
        preprocess_cfg: Optional[PreprocessConfig] = None,
        tuned_model_path: Optional[str] = None,
        finetuned_enabled: bool = False,
        finetuned_model_path: Optional[str] = None,
        finetuned_max_length: int = 512,
        nlp_text_mode: str = "canonical",
        **_: Any,
    ):
        self.model_name = str(model_name).strip() or "ai-forever/ruBert-base"
        self.device = device or ("cuda" if torch.cuda.is_available() else "cpu")
        self.min_confidence = float(max(0.0, min(1.0, min_confidence)))
        self.max_text_chars = int(max(200, min(20000, max_text_chars)))
        self.preprocess_cfg = preprocess_cfg or PreprocessConfig(
            model_text_mode=str(nlp_text_mode or "canonical").strip().lower() or "canonical",
            drop_fillers=True,
            dedupe=True,
            keep_timestamps=True,
            drop_stopwords=False,
            max_chars=self.max_text_chars,
        )
        self._finetuned_router = FinetunedRouterRuntime(
            model_name=self.model_name,
            device=self.device,
            tuned_model_path=str(tuned_model_path or "").strip(),
            finetuned_enabled=bool(finetuned_enabled),
            finetuned_model_path=str(finetuned_model_path or "").strip(),
            finetuned_max_length=int(max(64, min(512, finetuned_max_length))),
        )

    # Главный метод класса CallIntentAnalyzer, в функции собрана основная логика
    # анализа звонка и интерпретации результата локальной модели.
    def analyze(
        self,
        call: CallInput,
        allowed_intents: Dict[str, Dict],
        groups: Optional[Dict[str, Dict]] = None,
    ) -> AIAnalysis:
        started = time.time()
        prep = build_canonical([(s.start, s.text, s.role) for s in call.segments], self.preprocess_cfg)
        text = prep.model_text
        runtime_intent_ids = self._runtime_intent_ids(allowed_intents)

        probs, meta = self._finetuned_router.predict(text, runtime_intent_ids)
        if probs is None:
            return self._triage_result(
                reason=f"finetuned_unavailable:{meta.get('reason', 'unknown')}",
                processing_time_ms=(time.time() - started) * 1000.0,
                text_len=len(text),
                prep_meta=prep.meta,
                model_meta=meta,
            )

        intent_ids = list(meta.get("intent_ids") or runtime_intent_ids)
        best_idx = int(torch.argmax(probs).item())
        best_intent_id = intent_ids[best_idx]
        confidence = float(probs[best_idx].item())

        if confidence < self.min_confidence:
            return self._low_confidence_result(
                intent_id=best_intent_id,
                confidence=confidence,
                priority="medium",
                suggested_targets=[],
                processing_time_ms=(time.time() - started) * 1000.0,
                text_len=len(text),
                prep_meta=prep.meta,
                model_meta={
                    "finetuned_model": meta,
                    "review_required": True,
                    "review_reason": f"low_confidence:{confidence:.3f}",
                },
            )

        return AIAnalysis(
            intent=IntentResult(
                intent_id=best_intent_id,
                confidence=confidence,
                evidence=[],
                notes=f"finetuned confidence={confidence:.3f}",
            ),
            priority="medium",
            suggested_targets=[],
            raw={
                "mode": "finetuned_only",
                "model_version": self.model_name,
                "device": self.device,
                "processing_time_ms": round((time.time() - started) * 1000.0, 2),
                "text_length": len(text),
                "prep_meta": prep.meta,
                "finetuned_model": meta,
            },
        )
