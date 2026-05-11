# Листинг Б.1. Утилита пакетной предобработки текстов для routing-модели.

from __future__ import annotations

import argparse
import csv

from services.router.routing.nlp_preprocess import PreprocessConfig, build_model_text


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("input_csv")
    parser.add_argument("output_csv")
    parser.add_argument("--text-col", default="text")
    args = parser.parse_args()

    cfg = PreprocessConfig(model_text_mode="tokens", drop_fillers=True, dedupe=True)

    with open(args.input_csv, "r", encoding="utf-8-sig", newline="") as src, open(args.output_csv, "w", encoding="utf-8", newline="") as dst:
        reader = csv.DictReader(src)
        fieldnames = list(reader.fieldnames or []) + ["model_text"]
        writer = csv.DictWriter(dst, fieldnames=fieldnames)
        writer.writeheader()

        for row in reader:
            row["model_text"] = build_model_text(row.get(args.text_col, ""), [], mode=cfg.model_text_mode)
            writer.writerow(row)
