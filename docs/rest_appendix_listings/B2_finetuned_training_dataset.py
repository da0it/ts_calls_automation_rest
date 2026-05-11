# Листинг Б.2. Подготовка обучающей выборки для fine-tuned router.

from __future__ import annotations

from collections import Counter
from pathlib import Path
from typing import List

import pandas as pd


def build_training_dataframe(base_csv: Path, examples_csv: Path | None = None, feedback_csv: Path | None = None) -> pd.DataFrame:
    rows: List[dict] = []
    source_counts = Counter()

    base_df = pd.read_csv(base_csv)
    for _, rec in base_df.iterrows():
        rows.append({
            "text": rec["text"],
            "intent_id": rec["intent_id"],
            "source": "base_dataset",
        })
        source_counts["base_dataset"] += 1

    if examples_csv and examples_csv.exists():
        examples_df = pd.read_csv(examples_csv)
        for _, rec in examples_df.iterrows():
            rows.append({
                "text": rec["text"],
                "intent_id": rec["intent_id"],
                "source": "intent_examples",
            })
            source_counts["intent_examples"] += 1

    if feedback_csv and feedback_csv.exists():
        feedback_df = pd.read_csv(feedback_csv)
        for _, rec in feedback_df.iterrows():
            rows.append({
                "text": rec["resolved_text"],
                "intent_id": rec["resolved_intent_id"],
                "source": "operator_feedback",
            })
            source_counts["operator_feedback"] += 1

    df = pd.DataFrame(rows).dropna(subset=["text", "intent_id"]).drop_duplicates(subset=["text", "intent_id"])
    class_counts = df["intent_id"].value_counts().to_dict()

    print("rows:", len(df))
    print("source_counts:", dict(source_counts))
    print("class_counts:", class_counts)
    return df
