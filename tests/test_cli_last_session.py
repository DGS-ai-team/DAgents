from __future__ import annotations

from pathlib import Path

from app.cli.last_session import (
    last_session_store_path,
    load_last_session,
    read_last_session_record,
    save_last_session,
)


def test_save_and_load_last_session(tmp_path: Path, monkeypatch) -> None:
    cfg = tmp_path / "config.yaml"
    cfg.write_text("fs_root: ./.runtime\n", encoding="utf-8")
    runtime = tmp_path / ".runtime"
    monkeypatch.chdir(tmp_path)

    save_last_session(
        "http://127.0.0.1:18765",
        "sess-abc",
        config_path=str(cfg),
    )
    path = last_session_store_path(str(cfg))
    assert path.is_file()
    assert load_last_session("http://127.0.0.1:18765/", config_path=str(cfg)) == "sess-abc"
    assert load_last_session("http://127.0.0.1:9999", config_path=str(cfg)) is None


def test_load_last_session_missing_file(tmp_path: Path) -> None:
    cfg = tmp_path / "config.yaml"
    cfg.write_text("fs_root: ./.runtime\n", encoding="utf-8")
    assert load_last_session("http://127.0.0.1:18765", config_path=str(cfg)) is None


def test_read_last_session_record_ignores_endpoint(tmp_path: Path, monkeypatch) -> None:
    cfg = tmp_path / "config.yaml"
    cfg.write_text("fs_root: ./.runtime\n", encoding="utf-8")
    monkeypatch.chdir(tmp_path)
    save_last_session("http://127.0.0.1:18765", "sess-x", config_path=str(cfg))
    record = read_last_session_record(str(cfg))
    assert record is not None
    assert record["session_id"] == "sess-x"


def test_save_skips_empty_session_id(tmp_path: Path, monkeypatch) -> None:
    cfg = tmp_path / "config.yaml"
    cfg.write_text("fs_root: ./.runtime\n", encoding="utf-8")
    monkeypatch.chdir(tmp_path)
    save_last_session("http://127.0.0.1:18765", "  ", config_path=str(cfg))
    assert not last_session_store_path(str(cfg)).exists()


def test_load_tolerates_corrupt_json(tmp_path: Path, monkeypatch) -> None:
    cfg = tmp_path / "config.yaml"
    cfg.write_text("fs_root: ./.runtime\n", encoding="utf-8")
    monkeypatch.chdir(tmp_path)
    path = last_session_store_path(str(cfg))
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("{not json", encoding="utf-8")
    assert load_last_session("http://127.0.0.1:18765", config_path=str(cfg)) is None
    assert read_last_session_record(str(cfg)) is None
