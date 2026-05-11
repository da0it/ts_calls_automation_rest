# Листинг Б.5. Оценка качества маршрутизации по CSV-таблице.

from __future__ import annotations

import argparse
import csv
from collections import Counter

from sklearn.metrics import accuracy_score, classification_report


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("csv_path")
    parser.add_argument("--true-col", default="true_intent_id")
    parser.add_argument("--pred-col", default="pred_intent_id")
    args = parser.parse_args()

    y_true = []
    y_pred = []
    status_counts = Counter()

    with open(args.csv_path, "r", encoding="utf-8-sig", newline="") as fh:
        reader = csv.DictReader(fh)
        for row in reader:
            y_true.append(row[args.true_col])
            y_pred.append(row[args.pred_col])
            status_counts[row.get("status", "unknown")] += 1

    print("samples=", len(y_true))
    print("accuracy=", accuracy_score(y_true, y_pred))
    print(classification_report(y_true, y_pred, digits=4))
    print("status_counts=", dict(status_counts))
