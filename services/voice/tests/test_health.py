"""Тесты каркаса grade-voice (WP-1)."""
from fastapi.testclient import TestClient

from app.main import app

client = TestClient(app)


def test_health() -> None:
    resp = client.get("/api/v1/health")
    assert resp.status_code == 200
    body = resp.json()
    assert body["service"] == "grade-voice"
    assert body["stt"]["provider"] == "fake"
    assert body["tts"]["provider"] == "fake"
    assert "ru_01" in body["tts"]["speakers"]
