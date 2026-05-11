# Листинг Б.4. Импорт fine-tuned артефактов router в рабочий конфиг REST-системы.

from __future__ import annotations

import argparse
import json
import shutil
from pathlib import Path

import joblib


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("source_model_dir")
    parser.add_argument("target_model_dir")
    parser.add_argument("--max-length", type=int, default=512)
    args = parser.parse_args()

    src = Path(args.source_model_dir)
    dst = Path(args.target_model_dir)
    dst.mkdir(parents=True, exist_ok=True)

    for name in [
        "config.json",
        "model.safetensors",
        "tokenizer.json",
        "tokenizer_config.json",
        "special_tokens_map.json",
        "vocab.json",
        "merges.txt",
        "label_encoder.joblib",
        "temperature_call_purpose.json",
    ]:
        shutil.copy2(src / name, dst / name)

    label_encoder = joblib.load(src / "label_encoder.joblib")
    intent_ids = [str(item) for item in label_encoder.classes_]

    payload = {
        "model_name": src.name,
        "intent_ids": intent_ids,
        "trained_at": "imported",
        "max_length": args.max_length,
        "import_source": str(src),
        "label_mapping_source": "label_encoder.joblib",
    }
    (dst / "intent_ids.json").write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
