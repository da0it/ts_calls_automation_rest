# Листинг А.3. NLP-предобработка текста обращения перед классификацией.

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import List, Optional, Tuple, Dict, Any

# Список частых служебных слов, они могут удаляться при включенном параметре drop_stopwords=True в конфигурационном файле
STOP_WORDS = {
    "и","а","но","или","да","нет","это","в","на","к","ко","по","за","для","из","у","мы","вы","он","она","они",
    "я","ты","же","бы","ли","то","вот","там","тут","еще","ещё","уже","ну","ок","ладно","понятно","спасибо"
}

# Регулярные выражения для удаления коротких бессодержательных реплик
FILLER_PATTERNS = [
    r"^\s*(ал(е|ё)|алло)\s*[.!?]?\s*$",
    r"^\s*(да|да-да|угу|ага|мм+|мгм)\s*[.!?]?\s*$",
    r"^\s*(понятно|ясно|окей|хорошо)\s*[.!?]?\s*$",
    r"^\s*(спасибо)\s*[.!?]?\s*$",
]

# Класс данных, отвечающий за настройки предобработки текста
@dataclass
class PreprocessConfig:
    model_text_mode: str = "canonical"
    drop_fillers: bool = True
    drop_stopwords: bool = False
    dedupe: bool = True
    dedupe_window: int = 2
    max_chars: int = 4000
    keep_timestamps: bool = True
    do_tokenize: bool = True
    keep_special_tokens: bool = True


# Функция предназначена для базовой очистки текста: убирает пробелы по краям, заменяет ё на е, приводит
# текст к нижнему регистру и т.д.
def normalize_text(text: str) -> str:
    t = text.strip()
    t = t.replace("Ё", "Е").replace("ё", "е")
    t = t.lower()
    t = re.sub(r"[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}", " <email> ", t, flags=re.IGNORECASE)
    t = re.sub(r"(\+?\d[\d\s\-\(\)]{8,}\d)", " <phone> ", t)
    t = re.sub(r"\d+", " <num> ", t)
    t = re.sub(r"\s+", " ", t)
    return t.strip()

# Функция предназначена для проверки содержательности реплики. Проверка идет по предварительно определенному
# массиву FILLER_PATTERNS
def is_filler(text: str) -> bool:
    t = text.strip().lower()
    for pat in FILLER_PATTERNS:
        if re.match(pat, t, flags=re.IGNORECASE):
            return True
    return False

# Функция формирует текст для входа в модель в одном из режимов:
# Canonical: Возвращает текст с временными метками и строками
# Token: Возвращает токены (слова) через пробел
# Plain или Normalized: удаляет временные метки и собирает текст в одну строку
def build_model_text(
    canonical_text: str,
    tokens: List[str],
    *,
    mode: str,
) -> str:
    mode_norm = str(mode or "canonical").strip().lower()
    if mode_norm == "tokens":
        return " ".join(tokens).strip() or canonical_text
    if mode_norm in {"plain", "normalized"}:
        plain_text = re.sub(r"^\[\d{2}:\d{2}\]\s*", "", canonical_text, flags=re.MULTILINE)
        plain_text = re.sub(r"\s+", " ", plain_text).strip()
        return plain_text or canonical_text
    return canonical_text

# Главная функция предобработки.
# Принимает сегменты звонка, выполняет фильтрацию, нормализацию и агрегацию текста,
# формирует каноническое представление и текст для модели.
def build_canonical(
    segments: List[Tuple[float, str, Optional[str]]],
    cfg: Optional[PreprocessConfig] = None,
):
    cfg = cfg or PreprocessConfig()

    lines: List[str] = []
    for start, raw, _role in segments:
        if not raw:
            continue
        if cfg.drop_fillers and is_filler(raw):
            continue

        norm = normalize_text(raw)
        if not norm:
            continue

        if cfg.keep_timestamps:
            mm = int(max(0, start)) // 60
            ss = int(max(0, start)) % 60
            lines.append(f"[{mm:02d}:{ss:02d}] {norm}")
        else:
            lines.append(norm)

    canonical_text = "
".join(lines)
    return canonical_text
