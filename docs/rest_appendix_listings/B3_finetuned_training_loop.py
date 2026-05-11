# Листинг Б.3. Обучение и оценка fine-tuned routing-модели.

from __future__ import annotations

from dataclasses import dataclass
from typing import Dict

from sklearn.metrics import accuracy_score, precision_recall_fscore_support
from sklearn.model_selection import train_test_split
from transformers import AutoModelForSequenceClassification, AutoTokenizer, Trainer, TrainingArguments


@dataclass
class TrainSplit:
    train_texts: list[str]
    val_texts: list[str]
    train_labels: list[int]
    val_labels: list[int]


def stratified_split(texts: list[str], labels: list[int], test_size: float = 0.2) -> TrainSplit:
    x_train, x_val, y_train, y_val = train_test_split(
        texts,
        labels,
        test_size=test_size,
        random_state=42,
        stratify=labels,
    )
    return TrainSplit(x_train, x_val, y_train, y_val)


def _macro_precision_recall_f1(y_true: list[int], y_pred: list[int]) -> Dict[str, float]:
    precision, recall, f1, _ = precision_recall_fscore_support(y_true, y_pred, average="macro", zero_division=0)
    return {"macro_precision": float(precision), "macro_recall": float(recall), "macro_f1": float(f1)}


def train_finetuned_model(model_name: str, num_labels: int, output_dir: str) -> None:
    tokenizer = AutoTokenizer.from_pretrained(model_name)
    model = AutoModelForSequenceClassification.from_pretrained(model_name, num_labels=num_labels)

    args = TrainingArguments(
        output_dir=output_dir,
        evaluation_strategy="epoch",
        save_strategy="epoch",
        learning_rate=1e-6,
        per_device_train_batch_size=8,
        per_device_eval_batch_size=8,
        num_train_epochs=5,
        load_best_model_at_end=True,
        metric_for_best_model="eval_macro_f1",
    )

    trainer = Trainer(model=model, args=args, tokenizer=tokenizer)
    trainer.train()
    trainer.save_model(output_dir)
    tokenizer.save_pretrained(output_dir)
