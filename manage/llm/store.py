from __future__ import annotations

import threading
import uuid

from manage.storage.sqlite import SQLiteDatabase
from manage.llm.models import LLMConfig, LLMConfigCreate, LLMConfigMasked, LLMResolved

def _slug(name: str) -> str:
    s = "".join(c if c.isalnum() else "-" for c in name.strip().lower()).strip("-")
    return s or uuid.uuid4().hex[:8]

def _mask_key(key: str) -> str:
    if not key:
        return ""
    return f"sk-***{key[-4:]}" if len(key) >= 4 else "sk-***"

class LLMConfigStore:
    def __init__(self, db: SQLiteDatabase | None = None) -> None:
        self._db = db if (db and db.enabled) else None
        self._lock = threading.RLock()
        self._mem: dict[str, LLMConfig] = {}

    def _save(self, cfg: LLMConfig) -> None:
        if self._db is None:
            self._mem[cfg.id] = cfg
            return
        with self._db.connect() as conn:
            conn.execute(
                "INSERT INTO llm_configs(id,payload_json) VALUES(?,?) "
                "ON CONFLICT(id) DO UPDATE SET payload_json=excluded.payload_json",
                (cfg.id, cfg.model_dump_json()))
            conn.commit()

    def _all(self) -> list[LLMConfig]:
        if self._db is None:
            return list(self._mem.values())
        with self._db.connect() as conn:
            return [LLMConfig.model_validate_json(r["payload_json"])
                    for r in conn.execute("SELECT payload_json FROM llm_configs")]

    def _clear_defaults(self, now: int) -> None:
        for c in self._all():
            if c.is_default:
                c.is_default = False
                c.updated_at = now
                self._save(c)

    def create(self, payload: LLMConfigCreate, now: int) -> LLMConfig:
        with self._lock:
            if payload.is_default:
                self._clear_defaults(now)
            cfg_id = _slug(payload.name)
            if self.get(cfg_id):
                cfg_id = f"{cfg_id}-{uuid.uuid4().hex[:6]}"
            data = payload.model_dump()
            data.pop("clear_api_key", None)
            data["provider"] = payload.normalized_provider()
            cfg = LLMConfig(id=cfg_id, created_at=now, updated_at=now, **data)
            self._save(cfg)
            return cfg

    def list(self) -> list[LLMConfig]:
        return sorted(self._all(), key=lambda c: c.created_at)

    def get(self, cfg_id: str) -> LLMConfig | None:
        if self._db is None:
            return self._mem.get(cfg_id)
        with self._db.connect() as conn:
            row = conn.execute(
                "SELECT payload_json FROM llm_configs WHERE id=?", (cfg_id,)).fetchone()
            return LLMConfig.model_validate_json(row["payload_json"]) if row else None

    def update(self, cfg_id: str, payload: LLMConfigCreate, now: int) -> LLMConfig | None:
        with self._lock:
            existing = self.get(cfg_id)
            if not existing:
                return None
            if payload.is_default:
                self._clear_defaults(now)
            data = payload.model_dump()
            data.pop("clear_api_key", None)
            data["provider"] = payload.normalized_provider()
            if payload.clear_api_key:
                data["api_key"] = ""
            elif not str(payload.api_key or "").strip():
                data["api_key"] = existing.api_key
            cfg = LLMConfig(id=cfg_id, created_at=existing.created_at, updated_at=now, **data)
            self._save(cfg)
            return cfg

    def delete(self, cfg_id: str) -> bool:
        with self._lock:
            if self._db is None:
                return self._mem.pop(cfg_id, None) is not None
            with self._db.connect() as conn:
                cur = conn.execute("DELETE FROM llm_configs WHERE id=?", (cfg_id,))
                conn.commit()
                return cur.rowcount > 0

    def get_default(self) -> LLMConfig | None:
        return next((c for c in self._all() if c.is_default), None)

    def mask(self, cfg: LLMConfig) -> LLMConfigMasked:
        data = cfg.model_dump()
        has_key = bool(str(cfg.api_key or "").strip())
        data["api_key"] = _mask_key(cfg.api_key)
        data["has_api_key"] = has_key
        data["clear_api_key"] = False
        return LLMConfigMasked(**data)

    def resolve(self, cfg: LLMConfig) -> LLMResolved:
        return LLMResolved(model=cfg.model, baseURL=cfg.base_url, apiKey=cfg.api_key)
