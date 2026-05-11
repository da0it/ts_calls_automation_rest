# Листинг Б.7. End-to-end оценка REST-конвейера на размеченном наборе аудио.

from __future__ import annotations

import argparse
import csv
from pathlib import Path

import requests


def login(base_url: str, username: str, password: str) -> str:
    response = requests.post(
        f"{base_url}/api/v1/auth/login",
        json={"username": username, "password": password},
        timeout=30,
    )
    response.raise_for_status()
    return response.json()["token"]


def process_call(base_url: str, token: str, audio_path: Path) -> dict:
    with audio_path.open("rb") as fh:
        response = requests.post(
            f"{base_url}/api/v1/process-call",
            headers={"Authorization": f"Bearer {token}"},
            files={"audio": (audio_path.name, fh)},
            timeout=3600,
        )
    response.raise_for_status()
    return response.json()
