# Листинг А.5. Извлечение сущностей из транскрипта звонка.

from __future__ import annotations

# Модуль используется для создания регулярных выражений
import re
import logging
from typing import List, Dict, Optional

from .models import Entities, ExtractedEntity, Segment

logger = logging.getLogger(__name__)

# Класс отвечает за извлечение сущностей из текста звонка через DeepPavlov NER
# или регулярные выражения
class EntityExtractor:
    def __init__(
        self,
        use_ner: bool = True,
        *,
        allow_download: bool = False,
        allow_install: bool = False,
    ):
        self.use_ner = use_ner
        self.ner_model = None
        self.mode = "regex"
        self.startup_error = ""

    # Функция непосредственно извлекает сущности из сегментов диалога. Принимает список сегментов с полями start, end, speaker, text
    # Возвращает объект сущности с извлеченными данными
    def extract(self, segments: List[Segment]) -> Entities:
        full_text = " ".join(seg.text for seg in segments if seg.text)

        # Создание экземпляра класса данных сущностей
        entities = Entities()

        # Извлекаются персоны и организации через DeepPavlov NER
        if self.ner_model:
            ner_entities = self._extract_ner_entities(full_text)
            entities.persons = ner_entities.get("persons", [])
        else:
            # Fallback: простое извлечение имен через regex
            entities.persons = self._extract_persons_regex(full_text)

        # Извлекаются телефоны через регулярные выражения
        entities.phones = self._extract_phones(full_text)

        # Извлекаются emails через регулярные выражения
        entities.emails = self._extract_emails(full_text)
        return entities

    # Функция извлекает сущности из полного текста обращения с помощью DeepPavlov NER.
    def _extract_ner_entities(self, text: str) -> Dict[str, List[ExtractedEntity]]:
        try:
            result = self.ner_model([text])
            tokens = result[0][0]
            tags = result[1][0]
            entities = {"persons": [], "organizations": [], "locations": []}

            current_tokens = []
            current_type = None
            for token, tag in zip(tokens, tags):
                if tag.startswith("B-"):
                    if current_tokens:
                        entity = self._create_entity_from_tokens(current_tokens, current_type, text)
                        if entity:
                            entities[self._map_tag_to_type(current_type)].append(entity)
                    current_tokens = [token]
                    current_type = tag[2:]
                elif tag.startswith("I-") and current_tokens:
                    current_tokens.append(token)
                else:
                    if current_tokens:
                        entity = self._create_entity_from_tokens(current_tokens, current_type, text)
                        if entity:
                            entities[self._map_tag_to_type(current_type)].append(entity)
                    current_tokens = []
                    current_type = None
            return entities
        except Exception:
            return {"persons": [], "organizations": [], "locations": []}

    # Функция является запасной для извлечения ФИО через регулярные выражения (на случай, если по какой-либо причине NER не отработал)
    def _extract_persons_regex(self, text: str) -> List[ExtractedEntity]:
        patterns = [
            r'(?:меня\s+зовут|зовут|я|это)\s+([А-ЯЁ][а-яё]+(?:[\s,]+[А-ЯЁ][а-яё]+){0,2})',
            r'([А-ЯЁ][а-яё]+(?:[\s,]+[А-ЯЁ][а-яё]+){1,2})',
        ]
        persons = []
        seen = set()
        for pattern in patterns:
            for m in re.finditer(pattern, text):
                name = re.sub(r'[\s,]+', ' ', m.group(1)).strip()
                key = name.lower()
                if key in seen:
                    continue
                seen.add(key)
                persons.append(ExtractedEntity(type="person", value=name, confidence=0.7, context=text[max(0, m.start()-30):m.end()+30]))
        return persons
