# Листинг Б.6. Оценка качества транскрибации по метрике WER.

from __future__ import annotations

import argparse
import csv

from jiwer import wer


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("csv_path")
    parser.add_argument("--ref-col", default="reference_text")
    parser.add_argument("--hyp-col", default="hypothesis_text")
    args = parser.parse_args()

    refs = []
    hyps = []
    with open(args.csv_path, "r", encoding="utf-8-sig", newline="") as fh:
        reader = csv.DictReader(fh)
        for row in reader:
            refs.append(row[args.ref_col])
            hyps.append(row[args.hyp_col])

    print("samples=", len(refs))
    print("wer=", wer(refs, hyps))
