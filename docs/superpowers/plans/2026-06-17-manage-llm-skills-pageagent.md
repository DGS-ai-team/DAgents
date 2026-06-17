# Manage LLM Config + Skills + PageAgent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an LLM-config registry and a (lite) Skills distribution center to the Manage control plane, and embed alibaba/page-agent into the Manage Console so the Console is operable by natural language using a registered LLM config.

**Architecture:** Three Manage-side modules (`manage/llm/`, `manage/skills/`, `manage/platform` Blob routes) following the existing `manage/registry/` pattern (pydantic `models.py` + `XStore` over `SQLiteDatabase` + `build_X_router` wired in `manage_app.create_app`). Console additions are static HTML/JS under `manage/console/static/`. No Go Node changes.

**Tech Stack:** Python 3.11+, FastAPI, pydantic v2, stdlib `unittest` + `fastapi.testclient.TestClient`, SQLite (`manage/storage/sqlite.py`), vanilla JS Console, vendored `page-agent@1.7.1` IIFE.

## Global Constraints

- No Go Node / `node/**` / `client/**` changes in this PR (spec §2). Pure Python + Console.
- Storage rows use the existing `id + payload_json TEXT` JSON-blob convention (see `registry_agents`, `a2a_tasks`); add tables in `SQLiteDatabase._init_schema`, bump `schema_meta.schema_version` to `'3'`.
- Stores accept `SQLiteDatabase | None`; when `db is None` (or `not db.enabled`) fall back to an in-memory dict, exactly like `AgentRegistryStore`.
- Auth: mutating endpoints call `require_admin(authenticate(request))`; read endpoints call `authenticate(request)`. Open mode (no tokens configured) yields an admin `AuthContext` — tests run in open mode.
- `api_key` is stored plaintext; list/detail responses MASK it (`sk-***last4`), only `/resolve` returns plaintext (spec §4.3, §7).
- `provider` ∈ `openai | deepseek | qwen | vllm`. `risk_level` ∈ `low | medium | high`. `skill_packages.status` ∈ `draft | published`.
- Tests: `python -m unittest discover -s tests -p "test_*.py"` must stay green; new tests live in `tests/test_manage_llm.py`, `tests/test_manage_skills.py`.
- Conventional commits (`feat:` / `test:` / `docs:`); no attribution footer.

---

## File Structure

- Create `manage/llm/__init__.py`, `manage/llm/models.py`, `manage/llm/store.py`, `manage/llm/routes.py`
- Create `manage/skills/models.py`, `manage/skills/store.py`, `manage/skills/routes.py` (package `manage/skills/__init__.py` already exists)
- Create `manage/platform/blob_routes.py` (routes over existing `BlobStore`)
- Modify `manage/storage/sqlite.py` (`_init_schema`: +3 tables, version bump)
- Modify `manage/manage_app.py` (instantiate stores, include 3 routers)
- Create `tests/test_manage_llm.py`, `tests/test_manage_skills.py`
- Create `manage/console/static/llm.html`, `manage/console/static/skills.html`, `manage/console/static/vendor/page-agent.iife.js`, `manage/console/static/agent-bar.js`
- Modify `manage/console/static/index.html` (nav links + command bar mount), `manage/console/frontend/README.md` (vendor version note)
- Modify `docs/design/manage-architecture.md`, `CHANGELOG.md`

---

## Task 1: Storage schema — add tables

**Files:**
- Modify: `manage/storage/sqlite.py` (`_init_schema`)
- Test: `tests/test_manage_storage_schema.py` (create)

**Interfaces:**
- Produces: tables `llm_configs(id PK, payload_json)`, `skill_packages(skill_id, version, payload_json, PK(skill_id,version))`, `blobs(blob_id PK, payload_json)`.

- [ ] **Step 1: Write the failing test**

```python
# tests/test_manage_storage_schema.py
import tempfile, unittest
from pathlib import Path
from manage.storage.sqlite import SQLiteDatabase

class SchemaTest(unittest.TestCase):
    def test_new_tables_exist(self):
        with tempfile.TemporaryDirectory() as d:
            db = SQLiteDatabase(Path(d) / "m.db")
            with db.connect() as conn:
                rows = {r[0] for r in conn.execute(
                    "SELECT name FROM sqlite_master WHERE type='table'")}
            self.assertEqual({"llm_configs", "skill_packages", "blobs"} - rows, set())
            with db.connect() as conn:
                ver = conn.execute(
                    "SELECT value FROM schema_meta WHERE key='schema_version'").fetchone()[0]
            self.assertEqual(ver, "3")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python -m unittest tests.test_manage_storage_schema -v`
Expected: FAIL (tables missing / version is '2').

- [ ] **Step 3: Implement — extend `_init_schema`**

In `manage/storage/sqlite.py`, inside the `executescript("""...""")` block, change the `schema_version` insert value from `'2'` to `'3'` and append before the closing `"""`:

```sql
CREATE TABLE IF NOT EXISTS llm_configs (
    id TEXT PRIMARY KEY,
    payload_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS skill_packages (
    skill_id TEXT NOT NULL,
    version TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    PRIMARY KEY (skill_id, version)
);
CREATE TABLE IF NOT EXISTS blobs (
    blob_id TEXT PRIMARY KEY,
    payload_json TEXT NOT NULL
);
```

Note: `INSERT OR IGNORE` will NOT upgrade an existing DB's version row. Add right after the executescript block, still inside the `with` block:

```python
conn.execute(
    "INSERT INTO schema_meta(key,value) VALUES('schema_version','3') "
    "ON CONFLICT(key) DO UPDATE SET value='3'"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `python -m unittest tests.test_manage_storage_schema -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add manage/storage/sqlite.py tests/test_manage_storage_schema.py
git commit -m "feat(manage): add llm_configs/skill_packages/blobs tables (schema v3)"
```

---

## Task 2: LLM config models + store

**Files:**
- Create: `manage/llm/__init__.py` (empty), `manage/llm/models.py`, `manage/llm/store.py`
- Test: `tests/test_manage_llm.py` (create; store portion)

**Interfaces:**
- Produces:
  - `models.LLMConfig` (pydantic): `id:str, name:str, provider:str, base_url:str, model:str, api_key:str, reasoning_effort:str|None=None, thinking:str|None=None, is_default:bool=False, allowed_groups:list[str]=[], created_at:int, updated_at:int`
  - `models.LLMConfigCreate` (no id/timestamps), `models.LLMConfigMasked` (api_key masked), `models.LLMResolved` (`model:str, baseURL:str, apiKey:str`)
  - `store.LLMConfigStore(db: SQLiteDatabase | None)` with `create(payload, now)->LLMConfig`, `list()->list[LLMConfig]`, `get(id)->LLMConfig|None`, `update(id,payload,now)->LLMConfig|None`, `delete(id)->bool`, `get_default()->LLMConfig|None`, `mask(cfg)->LLMConfigMasked`, `resolve(cfg)->LLMResolved`.

- [ ] **Step 1: Write the failing test**

```python
# tests/test_manage_llm.py
import tempfile, unittest
from pathlib import Path
from manage.storage.sqlite import SQLiteDatabase
from manage.llm.store import LLMConfigStore
from manage.llm.models import LLMConfigCreate

def _store():
    d = tempfile.mkdtemp()
    return LLMConfigStore(SQLiteDatabase(Path(d) / "m.db"))

class LLMStoreTest(unittest.TestCase):
    def _mk(self, **kw):
        base = dict(name="cli", provider="openai",
                    base_url="http://127.0.0.1:8318/v1", model="claude-sonnet-4-6",
                    api_key="sk-abcd1234", is_default=True)
        base.update(kw)
        return LLMConfigCreate(**base)

    def test_create_get_list(self):
        s = _store()
        cfg = s.create(self._mk(), now=100)
        self.assertTrue(cfg.id)
        self.assertEqual(s.get(cfg.id).model, "claude-sonnet-4-6")
        self.assertEqual(len(s.list()), 1)

    def test_default_is_unique(self):
        s = _store()
        a = s.create(self._mk(name="a"), now=1)
        b = s.create(self._mk(name="b"), now=2)
        self.assertFalse(s.get(a.id).is_default)   # b stole default
        self.assertTrue(s.get(b.id).is_default)
        self.assertEqual(s.get_default().id, b.id)

    def test_mask_vs_resolve(self):
        s = _store()
        cfg = s.create(self._mk(), now=1)
        self.assertEqual(s.mask(cfg).api_key, "sk-***1234")
        r = s.resolve(cfg)
        self.assertEqual((r.model, r.baseURL, r.apiKey),
                         ("claude-sonnet-4-6", "http://127.0.0.1:8318/v1", "sk-abcd1234"))
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python -m unittest tests.test_manage_llm -v`
Expected: FAIL (`ModuleNotFoundError: manage.llm`).

- [ ] **Step 3: Implement `manage/llm/models.py`**

```python
from __future__ import annotations
from pydantic import BaseModel, Field

_PROVIDERS = {"openai", "deepseek", "qwen", "vllm"}

class LLMConfigCreate(BaseModel):
    name: str = Field(min_length=1)
    provider: str
    base_url: str = Field(min_length=1)
    model: str = Field(min_length=1)
    api_key: str = ""
    reasoning_effort: str | None = None
    thinking: str | None = None
    is_default: bool = False
    allowed_groups: list[str] = Field(default_factory=list)

    def normalized_provider(self) -> str:
        p = self.provider.strip().lower()
        return p if p in _PROVIDERS else "openai"

class LLMConfig(LLMConfigCreate):
    id: str
    created_at: int
    updated_at: int

class LLMConfigMasked(LLMConfig):
    pass  # api_key replaced with masked value by store.mask()

class LLMResolved(BaseModel):
    model: str
    baseURL: str
    apiKey: str
```

- [ ] **Step 4: Implement `manage/llm/store.py`**

```python
from __future__ import annotations
import json, sqlite3, threading, uuid
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
            data["provider"] = payload.normalized_provider()
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
        m = cfg.model_copy()
        m.api_key = _mask_key(cfg.api_key)
        return LLMConfigMasked(**m.model_dump())

    def resolve(self, cfg: LLMConfig) -> LLMResolved:
        return LLMResolved(model=cfg.model, baseURL=cfg.base_url, apiKey=cfg.api_key)
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `python -m unittest tests.test_manage_llm -v`
Expected: PASS (3 tests).

- [ ] **Step 6: Commit**

```bash
git add manage/llm/__init__.py manage/llm/models.py manage/llm/store.py tests/test_manage_llm.py
git commit -m "feat(manage): LLM config store with default-uniqueness and key masking"
```

---

## Task 3: LLM config router + wiring

**Files:**
- Create: `manage/llm/routes.py`
- Modify: `manage/manage_app.py`
- Test: `tests/test_manage_llm.py` (append router tests)

**Interfaces:**
- Consumes: `LLMConfigStore` (Task 2); `manage.platform.auth.authenticate/require_admin`; `manage.platform.audit.AuditLog`.
- Produces: `routes.build_llm_router(store: LLMConfigStore, audit: AuditLog) -> APIRouter` mounting the 7 endpoints from spec §4.3.

- [ ] **Step 1: Write the failing test (append to `tests/test_manage_llm.py`)**

```python
from fastapi import FastAPI
from fastapi.testclient import TestClient
from manage.llm.routes import build_llm_router
from manage.platform.audit import AuditLog

def _client():
    app = FastAPI()
    app.include_router(build_llm_router(_store(), AuditLog(max_entries=50)))
    return TestClient(app)

class LLMRouterTest(unittest.TestCase):
    def test_crud_mask_resolve(self):
        c = _client()
        body = dict(name="cli", provider="openai",
                    base_url="http://127.0.0.1:8318/v1", model="claude-sonnet-4-6",
                    api_key="sk-abcd1234", is_default=True)
        r = c.post("/v1/llm/configs", json=body)
        self.assertEqual(r.status_code, 200, r.text)
        cid = r.json()["id"]
        # list masks the key
        self.assertEqual(c.get("/v1/llm/configs").json()[0]["api_key"], "sk-***1234")
        # resolve returns plaintext PageAgent shape
        res = c.get(f"/v1/llm/configs/{cid}/resolve").json()
        self.assertEqual(res, {"model": "claude-sonnet-4-6",
                               "baseURL": "http://127.0.0.1:8318/v1",
                               "apiKey": "sk-abcd1234"})
        self.assertEqual(c.get("/v1/llm/configs/default/resolve").json()["apiKey"], "sk-abcd1234")
        self.assertEqual(c.delete(f"/v1/llm/configs/{cid}").status_code, 200)
        self.assertEqual(c.get(f"/v1/llm/configs/{cid}").status_code, 404)
```

(Replace the module-level `_store()` reuse: the appended `_client()` builds its own store; keep one `_store()` helper at top of file.)

- [ ] **Step 2: Run test to verify it fails**

Run: `python -m unittest tests.test_manage_llm -v`
Expected: FAIL (`manage.llm.routes` missing).

- [ ] **Step 3: Implement `manage/llm/routes.py`**

```python
from __future__ import annotations
import time
from fastapi import APIRouter, HTTPException, Request
from manage.llm.models import LLMConfig, LLMConfigCreate, LLMConfigMasked, LLMResolved
from manage.llm.store import LLMConfigStore
from manage.platform.audit import AuditLog
from manage.platform.auth import authenticate, require_admin

def build_llm_router(store: LLMConfigStore, audit: AuditLog) -> APIRouter:
    router = APIRouter(prefix="/v1/llm", tags=["llm"])

    @router.post("/configs", response_model=LLMConfigMasked)
    def create_config(payload: LLMConfigCreate, request: Request) -> LLMConfigMasked:
        auth = authenticate(request)
        require_admin(auth)
        cfg = store.create(payload, now=int(time.time()))
        audit.record(actor=auth.token_id, action="llm_config.create", target_agent_id=cfg.id)
        return store.mask(cfg)

    @router.get("/configs", response_model=list[LLMConfigMasked])
    def list_configs(request: Request) -> list[LLMConfigMasked]:
        authenticate(request)
        return [store.mask(c) for c in store.list()]

    @router.get("/configs/default/resolve", response_model=LLMResolved)
    def resolve_default(request: Request) -> LLMResolved:
        authenticate(request)
        cfg = store.get_default()
        if not cfg:
            raise HTTPException(status_code=404, detail="no default llm config")
        return store.resolve(cfg)

    @router.get("/configs/{cfg_id}", response_model=LLMConfigMasked)
    def get_config(cfg_id: str, request: Request) -> LLMConfigMasked:
        authenticate(request)
        cfg = store.get(cfg_id)
        if not cfg:
            raise HTTPException(status_code=404, detail="not found")
        return store.mask(cfg)

    @router.get("/configs/{cfg_id}/resolve", response_model=LLMResolved)
    def resolve_config(cfg_id: str, request: Request) -> LLMResolved:
        authenticate(request)
        cfg = store.get(cfg_id)
        if not cfg:
            raise HTTPException(status_code=404, detail="not found")
        return store.resolve(cfg)

    @router.put("/configs/{cfg_id}", response_model=LLMConfigMasked)
    def update_config(cfg_id: str, payload: LLMConfigCreate, request: Request) -> LLMConfigMasked:
        auth = authenticate(request)
        require_admin(auth)
        cfg = store.update(cfg_id, payload, now=int(time.time()))
        if not cfg:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(actor=auth.token_id, action="llm_config.update", target_agent_id=cfg_id)
        return store.mask(cfg)

    @router.delete("/configs/{cfg_id}")
    def delete_config(cfg_id: str, request: Request) -> dict[str, bool]:
        auth = authenticate(request)
        require_admin(auth)
        ok = store.delete(cfg_id)
        if not ok:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(actor=auth.token_id, action="llm_config.delete", target_agent_id=cfg_id)
        return {"deleted": True}

    return router
```

Route ordering note: `/configs/default/resolve` is declared BEFORE `/configs/{cfg_id}` so "default" is not captured as an id.

Confirm `AuditLog.record(action=..., target=...)` matches the existing signature — open `manage/platform/audit.py` and align argument names; if it differs (e.g. `audit.record(actor=..., action=..., target_agent_id=...)`), match it. (Read the file before writing this router.)

- [ ] **Step 4: Wire into `manage/manage_app.py`**

After the existing store instantiations add:

```python
from manage.llm.routes import build_llm_router
from manage.llm.store import LLMConfigStore
# ...
llm_store = LLMConfigStore(db=db if db.enabled else None)
# ... with the other include_router calls:
app.include_router(build_llm_router(llm_store, audit))
# ... near app.state assignments:
app.state.llm_store = llm_store
```

- [ ] **Step 5: Run tests**

Run: `python -m unittest tests.test_manage_llm -v`
Expected: PASS. Then smoke the app import: `python -c "import manage.manage_app"` (no error).

- [ ] **Step 6: Commit**

```bash
git add manage/llm/routes.py manage/manage_app.py tests/test_manage_llm.py
git commit -m "feat(manage): LLM config registry API + console wiring"
```

---

## Task 4: Platform Blob API

**Files:**
- Create: `manage/platform/blob_routes.py`
- Modify: `manage/manage_app.py`
- Test: `tests/test_manage_skills.py` (create; blob portion)

**Interfaces:**
- Consumes: existing `manage.platform.blob.BlobStore` + `BlobStoreConfig(root, max_bytes)`. **Confirmed: `BlobStore` currently exposes only `status()`** — ADD: `put(data: bytes, content_type: str) -> dict` (returns `{blob_id, sha256, size, content_type}`), `get(blob_id) -> tuple[bytes, dict] | None`, `head(blob_id) -> dict | None`, `delete(blob_id) -> bool`. Content-addressed: `blob_id = sha256`; bytes at `config.root/{blob_id}`, JSON sidecar `config.root/{blob_id}.json`. Raise `HTTPException(413)` if `config.max_bytes` exceeded.
- Produces: `blob_routes.build_blob_router(blob: BlobStore) -> APIRouter` with `POST /v1/blobs`, `GET/HEAD/DELETE /v1/blobs/{id}`. POST returns `{blob_id, sha256, size}`.

- [ ] **Step 1: Write the failing test**

```python
# tests/test_manage_skills.py
import hashlib, tempfile, unittest
from pathlib import Path
from fastapi import FastAPI
from fastapi.testclient import TestClient
from manage.platform.blob import BlobStore, BlobStoreConfig
from manage.platform.blob_routes import build_blob_router

def _blob_client():
    d = tempfile.mkdtemp()
    blob = BlobStore(BlobStoreConfig(root=Path(d), max_bytes=None))
    app = FastAPI(); app.include_router(build_blob_router(blob))
    return TestClient(app)

class BlobTest(unittest.TestCase):
    def test_upload_download_roundtrip(self):
        c = _blob_client()
        data = b"hello-skill-zip-bytes"
        r = c.post("/v1/blobs", files={"file": ("s.zip", data, "application/zip")})
        self.assertEqual(r.status_code, 200, r.text)
        bid = r.json()["blob_id"]
        self.assertEqual(r.json()["sha256"], hashlib.sha256(data).hexdigest())
        got = c.get(f"/v1/blobs/{bid}")
        self.assertEqual(got.content, data)
        self.assertEqual(c.head(f"/v1/blobs/{bid}").status_code, 200)
```

(`BlobStoreConfig` real fields are `root: Path|None` and `max_bytes: int|None` — confirmed in `blob.py`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `python -m unittest tests.test_manage_skills.BlobTest -v`
Expected: FAIL.

- [ ] **Step 3: Implement `BlobStore` put/get/head/delete (if missing) + `manage/platform/blob_routes.py`**

```python
# manage/platform/blob_routes.py
from __future__ import annotations
from fastapi import APIRouter, HTTPException, Request, Response, UploadFile, File
from manage.platform.auth import authenticate, require_admin
from manage.platform.blob import BlobStore

def build_blob_router(blob: BlobStore) -> APIRouter:
    router = APIRouter(prefix="/v1/blobs", tags=["blob"])

    @router.post("")
    async def upload(request: Request, file: UploadFile = File(...)) -> dict:
        require_admin(authenticate(request))
        data = await file.read()
        meta = blob.put(data, content_type=file.content_type or "application/octet-stream")
        return {"blob_id": meta["blob_id"], "sha256": meta["sha256"], "size": meta["size"]}

    @router.get("/{blob_id}")
    def download(blob_id: str, request: Request) -> Response:
        authenticate(request)
        got = blob.get(blob_id)
        if got is None:
            raise HTTPException(status_code=404, detail="not found")
        data, meta = got
        return Response(content=data, media_type=meta.get("content_type", "application/octet-stream"))

    @router.head("/{blob_id}")
    def head(blob_id: str, request: Request) -> Response:
        authenticate(request)
        meta = blob.head(blob_id)
        if meta is None:
            raise HTTPException(status_code=404, detail="not found")
        return Response(status_code=200, headers={"x-blob-sha256": meta["sha256"],
                                                   "x-blob-size": str(meta["size"])})

    @router.delete("/{blob_id}")
    def delete(blob_id: str, request: Request) -> dict:
        require_admin(authenticate(request))
        return {"deleted": blob.delete(blob_id)}

    return router
```

`BlobStore.put`: compute `sha256 = hashlib.sha256(data).hexdigest()`, `blob_id = sha256` (content-addressed), write bytes to `{config.root}/{blob_id}`, write a `{config.root}/{blob_id}.json` sidecar with `{sha256,size,content_type}`; enforce `config.max_bytes` if set (raise `HTTPException(413)`). `get`/`head` read them back; `delete` removes both files. (`config.root` is guaranteed non-None when blob routes are mounted; guard with `if not blob.enabled: raise HTTPException(503)`.)

- [ ] **Step 4: Wire into `manage_app.py` and run tests**

Add `from manage.platform.blob_routes import build_blob_router` and `app.include_router(build_blob_router(blob))` (the `blob` instance already exists in `create_app`).

Run: `python -m unittest tests.test_manage_skills.BlobTest -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add manage/platform/blob.py manage/platform/blob_routes.py manage/manage_app.py tests/test_manage_skills.py
git commit -m "feat(manage): Platform Blob API (upload/download/head/delete)"
```

---

## Task 5: Skills models + store

**Files:**
- Create: `manage/skills/models.py`, `manage/skills/store.py`
- Test: `tests/test_manage_skills.py` (append store tests)

**Interfaces:**
- Produces:
  - `models.SkillPackage` (pydantic): `skill_id, version, name, description="", owner="", team="", risk_level="low", required_tools=[], required_scopes=[], blob_id, status="draft", created_at, updated_at`
  - `models.SkillPackageCreate` (no status/timestamps; `blob_id` set by router after blob upload)
  - `store.SkillPackageStore(db)` with `create(pkg, now)`, `publish(skill_id,version,now)->SkillPackage|None`, `get(skill_id)->list[SkillPackage]`, `get_version(skill_id,version)`, `catalog()->list[SkillPackage]` (published only), `catalog_version()->int`, `sync_manifest(since:int)->list[dict]`.
- `catalog_version` is the count of publish events; persist a `skills_catalog_version` row in `schema_meta` incremented on each publish.

- [ ] **Step 1: Write the failing test (append)**

```python
from manage.storage.sqlite import SQLiteDatabase
from manage.skills.store import SkillPackageStore
from manage.skills.models import SkillPackageCreate

def _skill_store():
    d = tempfile.mkdtemp()
    return SkillPackageStore(SQLiteDatabase(Path(d) / "m.db"))

class SkillStoreTest(unittest.TestCase):
    def _mk(self, **kw):
        base = dict(skill_id="svc-restart", version="1.0.0", name="Service Restart",
                    risk_level="medium", blob_id="deadbeef")
        base.update(kw); return SkillPackageCreate(**base)

    def test_draft_then_publish_appears_in_catalog(self):
        s = _skill_store()
        s.create(self._mk(), now=1)
        self.assertEqual(s.catalog(), [])               # draft not in catalog
        pub = s.publish("svc-restart", "1.0.0", now=2)
        self.assertEqual(pub.status, "published")
        self.assertEqual(len(s.catalog()), 1)
        self.assertEqual(s.catalog_version(), 1)

    def test_sync_manifest_since(self):
        s = _skill_store()
        s.create(self._mk(), now=1); s.publish("svc-restart", "1.0.0", now=2)
        self.assertEqual(len(s.sync_manifest(since=0)), 1)
        self.assertEqual(s.sync_manifest(since=1), [])   # nothing new past current version
```

- [ ] **Step 2: Run — verify fail.** `python -m unittest tests.test_manage_skills.SkillStoreTest -v` → FAIL.

- [ ] **Step 3: Implement `manage/skills/models.py`**

```python
from __future__ import annotations
from pydantic import BaseModel, Field

class SkillPackageCreate(BaseModel):
    skill_id: str = Field(min_length=1)
    version: str = Field(min_length=1)
    name: str = Field(min_length=1)
    description: str = ""
    owner: str = ""
    team: str = ""
    risk_level: str = "low"
    required_tools: list[str] = Field(default_factory=list)
    required_scopes: list[str] = Field(default_factory=list)
    blob_id: str = ""

class SkillPackage(SkillPackageCreate):
    status: str = "draft"
    created_at: int
    updated_at: int
    catalog_seq: int = 0   # publish sequence number, 0 = unpublished
```

- [ ] **Step 4: Implement `manage/skills/store.py`**

```python
from __future__ import annotations
import threading
from manage.storage.sqlite import SQLiteDatabase
from manage.skills.models import SkillPackage, SkillPackageCreate

class SkillPackageStore:
    def __init__(self, db: SQLiteDatabase | None = None) -> None:
        self._db = db if (db and db.enabled) else None
        self._lock = threading.RLock()
        self._mem: dict[tuple[str, str], SkillPackage] = {}
        self._mem_ver = 0

    def _save(self, pkg: SkillPackage) -> None:
        if self._db is None:
            self._mem[(pkg.skill_id, pkg.version)] = pkg; return
        with self._db.connect() as conn:
            conn.execute(
                "INSERT INTO skill_packages(skill_id,version,payload_json) VALUES(?,?,?) "
                "ON CONFLICT(skill_id,version) DO UPDATE SET payload_json=excluded.payload_json",
                (pkg.skill_id, pkg.version, pkg.model_dump_json()))
            conn.commit()

    def _all(self) -> list[SkillPackage]:
        if self._db is None:
            return list(self._mem.values())
        with self._db.connect() as conn:
            return [SkillPackage.model_validate_json(r["payload_json"])
                    for r in conn.execute("SELECT payload_json FROM skill_packages")]

    def catalog_version(self) -> int:
        if self._db is None:
            return self._mem_ver
        with self._db.connect() as conn:
            row = conn.execute(
                "SELECT value FROM schema_meta WHERE key='skills_catalog_version'").fetchone()
            return int(row["value"]) if row else 0

    def _bump_version(self) -> int:
        if self._db is None:
            self._mem_ver += 1; return self._mem_ver
        with self._db.connect() as conn:
            cur = self.catalog_version() + 1
            conn.execute(
                "INSERT INTO schema_meta(key,value) VALUES('skills_catalog_version',?) "
                "ON CONFLICT(key) DO UPDATE SET value=?", (str(cur), str(cur)))
            conn.commit(); return cur

    def create(self, payload: SkillPackageCreate, now: int) -> SkillPackage:
        with self._lock:
            pkg = SkillPackage(status="draft", created_at=now, updated_at=now,
                               **payload.model_dump())
            self._save(pkg); return pkg

    def get_version(self, skill_id: str, version: str) -> SkillPackage | None:
        return next((p for p in self._all()
                     if p.skill_id == skill_id and p.version == version), None)

    def publish(self, skill_id: str, version: str, now: int) -> SkillPackage | None:
        with self._lock:
            pkg = self.get_version(skill_id, version)
            if not pkg:
                return None
            pkg.status = "published"; pkg.updated_at = now
            pkg.catalog_seq = self._bump_version()
            self._save(pkg); return pkg

    def get(self, skill_id: str) -> list[SkillPackage]:
        return [p for p in self._all() if p.skill_id == skill_id]

    def catalog(self) -> list[SkillPackage]:
        return sorted([p for p in self._all() if p.status == "published"],
                      key=lambda p: p.catalog_seq)

    def sync_manifest(self, since: int) -> list[dict]:
        return [{"skill_id": p.skill_id, "version": p.version, "blob_id": p.blob_id,
                 "download_url": f"/v1/skills/catalog/{p.skill_id}/versions/{p.version}/download"}
                for p in self.catalog() if p.catalog_seq > since]
```

- [ ] **Step 5: Run — verify pass.** `python -m unittest tests.test_manage_skills.SkillStoreTest -v` → PASS.

- [ ] **Step 6: Commit**

```bash
git add manage/skills/models.py manage/skills/store.py tests/test_manage_skills.py
git commit -m "feat(manage): skill package store (draft/publish, catalog, sync manifest)"
```

---

## Task 6: Skills router + wiring

**Files:**
- Create: `manage/skills/routes.py`
- Modify: `manage/manage_app.py`
- Test: `tests/test_manage_skills.py` (append router tests)

**Interfaces:**
- Consumes: `SkillPackageStore` (Task 5), `BlobStore` (Task 4), auth, audit.
- Produces: `routes.build_skills_router(store, blob, audit) -> APIRouter` per spec §5.4.

- [ ] **Step 1: Write the failing test (append)**

```python
from manage.skills.routes import build_skills_router
from manage.platform.audit import AuditLog

def _skills_client():
    d = tempfile.mkdtemp()
    db = SQLiteDatabase(Path(d) / "m.db")
    blob = BlobStore(BlobStoreConfig(root=Path(d) / "blobs", max_bytes=None))
    app = FastAPI()
    app.include_router(build_skills_router(SkillPackageStore(db), blob, AuditLog(max_entries=50)))
    return TestClient(app)

class SkillRouterTest(unittest.TestCase):
    def test_upload_publish_catalog_download(self):
        c = _skills_client()
        zip_bytes = b"PK\x03\x04 fake skill zip"
        r = c.post("/v1/skills/packages",
                   data={"skill_id": "svc-restart", "version": "1.0.0",
                         "name": "Service Restart", "risk_level": "medium"},
                   files={"file": ("svc.zip", zip_bytes, "application/zip")})
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json()["status"], "draft")
        self.assertEqual(c.get("/v1/skills/catalog").json(), [])  # draft hidden
        self.assertEqual(
            c.post("/v1/skills/packages/svc-restart/versions/1.0.0/publish").status_code, 200)
        cat = c.get("/v1/skills/catalog").json()
        self.assertEqual(len(cat), 1)
        dl = c.get("/v1/skills/catalog/svc-restart/versions/1.0.0/download")
        self.assertEqual(dl.content, zip_bytes)
        self.assertEqual(len(c.get("/v1/skills/sync/manifest?since=0").json()), 1)
```

- [ ] **Step 2: Run — verify fail.**

- [ ] **Step 3: Implement `manage/skills/routes.py`**

```python
from __future__ import annotations
import time
from fastapi import APIRouter, File, Form, HTTPException, Request, Response, UploadFile
from manage.platform.audit import AuditLog
from manage.platform.auth import authenticate, require_admin
from manage.platform.blob import BlobStore
from manage.skills.models import SkillPackage, SkillPackageCreate
from manage.skills.store import SkillPackageStore

def build_skills_router(store: SkillPackageStore, blob: BlobStore, audit: AuditLog) -> APIRouter:
    router = APIRouter(prefix="/v1/skills", tags=["skills"])

    @router.post("/packages", response_model=SkillPackage)
    async def upload(request: Request, file: UploadFile = File(...),
                     skill_id: str = Form(...), version: str = Form(...),
                     name: str = Form(...), description: str = Form(""),
                     owner: str = Form(""), team: str = Form(""),
                     risk_level: str = Form("low")) -> SkillPackage:
        auth = authenticate(request)
        require_admin(auth)
        data = await file.read()
        meta = blob.put(data, content_type="application/zip")
        payload = SkillPackageCreate(skill_id=skill_id, version=version, name=name,
                                     description=description, owner=owner, team=team,
                                     risk_level=risk_level, blob_id=meta["blob_id"])
        pkg = store.create(payload, now=int(time.time()))
        audit.record(actor=auth.token_id, action="skill.upload", target_agent_id=f"{skill_id}@{version}")
        return pkg

    @router.post("/packages/{skill_id}/versions/{version}/publish", response_model=SkillPackage)
    def publish(skill_id: str, version: str, request: Request) -> SkillPackage:
        auth = authenticate(request)
        require_admin(auth)
        pkg = store.publish(skill_id, version, now=int(time.time()))
        if not pkg:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(actor=auth.token_id, action="skill.publish", target_agent_id=f"{skill_id}@{version}")
        return pkg

    @router.get("/catalog", response_model=list[SkillPackage])
    def catalog(request: Request) -> list[SkillPackage]:
        authenticate(request); return store.catalog()

    @router.get("/sync/manifest")
    def sync_manifest(request: Request, since: int = 0) -> dict:
        authenticate(request)
        return {"catalog_version": store.catalog_version(), "items": store.sync_manifest(since)}

    @router.get("/catalog/{skill_id}", response_model=list[SkillPackage])
    def get_skill(skill_id: str, request: Request) -> list[SkillPackage]:
        authenticate(request)
        pkgs = store.get(skill_id)
        if not pkgs:
            raise HTTPException(status_code=404, detail="not found")
        return pkgs

    @router.get("/catalog/{skill_id}/versions/{version}/download")
    def download(skill_id: str, version: str, request: Request) -> Response:
        authenticate(request)
        pkg = store.get_version(skill_id, version)
        if not pkg or pkg.status != "published":
            raise HTTPException(status_code=404, detail="not found")
        got = blob.get(pkg.blob_id)
        if got is None:
            raise HTTPException(status_code=404, detail="blob missing")
        data, meta = got
        return Response(content=data, media_type="application/zip")

    return router
```

Route ordering: `/sync/manifest` and `/catalog` are declared before `/catalog/{skill_id}` (FastAPI matches in declaration order for overlapping static vs param segments — keep static paths first).

- [ ] **Step 4: Wire into `manage_app.py` + run tests**

```python
from manage.skills.routes import build_skills_router
from manage.skills.store import SkillPackageStore
skills_store = SkillPackageStore(db=db if db.enabled else None)
app.include_router(build_skills_router(skills_store, blob, audit))
app.state.skills_store = skills_store
```

Run: `python -m unittest tests.test_manage_skills -v` (all classes) → PASS.

- [ ] **Step 5: Full-suite regression**

Run: `python -m unittest discover -s tests -p "test_*.py"`
Expected: OK (no regressions in existing manage/cli tests).

- [ ] **Step 6: Commit**

```bash
git add manage/skills/routes.py manage/manage_app.py tests/test_manage_skills.py
git commit -m "feat(manage): skills registry API (upload/publish/catalog/download/sync)"
```

---

## Task 7: Console — LLM config + Skills pages

**Files:**
- Create: `manage/console/static/llm.html`, `manage/console/static/skills.html`
- Modify: `manage/console/static/index.html` (top-nav links to the two pages)

**No automated test** (Manage Console is plain static HTML; repo has no frontend test harness here). Verification is manual via the running app.

- [ ] **Step 1: Implement `llm.html`** — a self-contained page (fetch + render, no build step, match existing console styling/tokens used by `index.html`):
  - Table of configs from `GET /v1/llm/configs` (shows masked `api_key`).
  - "New" form (name, provider `<select openai|deepseek|qwen|vllm>`, base_url, model, api_key, reasoning_effort, thinking, is_default checkbox, allowed_groups comma-list) → `POST /v1/llm/configs`.
  - Row actions: edit (`PUT`), delete (`DELETE`). Send the `x-dagents-a2a-token` header only if a token is present in `localStorage` (open-mode dev needs none).

- [ ] **Step 2: Implement `skills.html`** — upload form (skill_id, version, name, risk_level `<select>`, zip file) → `POST /v1/skills/packages` (multipart); list draft+published from `GET /v1/skills/catalog` plus `GET /v1/skills/catalog/{id}`; publish button → publish endpoint; download link.

- [ ] **Step 3: Add nav** — in `index.html` add header links: `Agent 目录` (current) · `LLM 配置` (llm.html) · `Skills` (skills.html).

- [ ] **Step 4: Manual verification**

```bash
MANAGE_HOST=127.0.0.1 MANAGE_PORT=9351 MANAGE_DB_PATH=/tmp/mtest.db python run_manage.py &
# open http://127.0.0.1:9351/console/llm.html → create a config → see it masked in the table
curl -s localhost:9351/v1/llm/configs | python -m json.tool   # api_key masked
```
Expected: config created, listed with masked key; skills.html upload+publish shows in catalog.

- [ ] **Step 5: Commit**

```bash
git add manage/console/static/llm.html manage/console/static/skills.html manage/console/static/index.html
git commit -m "feat(console): LLM config and skills management pages"
```

---

## Task 8: Console — PageAgent command bar

**Files:**
- Create: `manage/console/static/vendor/page-agent.iife.js` (vendored), `manage/console/static/agent-bar.js`
- Modify: `manage/console/static/index.html` (load vendor + agent-bar; add command-bar markup)
- Modify: `manage/console/frontend/README.md` (vendor version note)

**Interfaces:**
- Consumes: `GET /v1/llm/configs` (populate selector), `GET /v1/llm/configs/{id}/resolve` (→ `{model, baseURL, apiKey}`), global `PageAgent` from the vendored IIFE.

- [ ] **Step 1: Vendor page-agent 1.7.1**

```bash
mkdir -p manage/console/static/vendor
curl -fsSL "https://registry.npmmirror.com/page-agent/1.7.1/files/dist/iife/page-agent.js" \
  -o manage/console/static/vendor/page-agent.iife.js
test -s manage/console/static/vendor/page-agent.iife.js && echo OK
```
(If the IIFE filename differs, list `https://registry.npmmirror.com/page-agent/1.7.1/files/dist/iife/` and pick the non-demo bundle. Record the exact source URL in the README.)

- [ ] **Step 2: Implement `agent-bar.js`**

```javascript
// Populates the LLM-config selector and runs PageAgent against the Console DOM.
async function hdr() {
  const t = localStorage.getItem("manageToken");
  return t ? { "x-dagents-a2a-token": t } : {};
}
async function loadConfigs() {
  const r = await fetch("/v1/llm/configs", { headers: await hdr() });
  const sel = document.getElementById("agent-llm");
  (await r.json()).forEach(c => {
    const o = document.createElement("option");
    o.value = c.id; o.textContent = `${c.name} (${c.model})`;
    if (c.is_default) o.selected = true;
    sel.appendChild(o);
  });
}
async function runAgentTask() {
  const id = document.getElementById("agent-llm").value;
  const task = document.getElementById("agent-task").value.trim();
  const status = document.getElementById("agent-status");
  if (!id || !task) { status.textContent = "选择配置并输入任务"; return; }
  status.textContent = "解析配置…";
  const cfg = await (await fetch(`/v1/llm/configs/${id}/resolve`, { headers: await hdr() })).json();
  status.textContent = "执行中…";
  try {
    const agent = new PageAgent({ ...cfg, language: "zh-CN" });
    const res = await agent.execute(task);
    status.textContent = "完成：" + (res?.summary ?? JSON.stringify(res)).slice(0, 200);
  } catch (e) { status.textContent = "失败：" + e.message; }
}
window.addEventListener("DOMContentLoaded", loadConfigs);
```

- [ ] **Step 3: Add markup + scripts to `index.html`**

```html
<div id="agent-bar" style="display:flex;gap:8px;align-items:center;padding:8px">
  <select id="agent-llm"></select>
  <input id="agent-task" placeholder="用自然语言操作控制台，如：给 node-a 分配 a2a-lab 组" style="flex:1"/>
  <button onclick="runAgentTask()">执行</button>
  <span id="agent-status"></span>
</div>
<script src="/console/vendor/page-agent.iife.js"></script>
<script src="/console/agent-bar.js"></script>
```

- [ ] **Step 4: Manual verification**

With Manage running and at least one LLM config whose upstream is reachable (e.g. cliproxy `claude-sonnet-4-6`):
- open `http://127.0.0.1:9351/console/`, the selector lists configs (default preselected);
- type "列出所有在线 Node" → status shows 执行中 → PageAgent drives the page; on success status shows a summary.
Note in README: PageAgent calls the LLM directly from the browser; cliproxy must allow the Console origin (CORS) or be reachable.

- [ ] **Step 5: Commit**

```bash
git add manage/console/static/vendor/page-agent.iife.js manage/console/static/agent-bar.js manage/console/static/index.html manage/console/frontend/README.md
git commit -m "feat(console): embed PageAgent command bar reusing LLM config"
```

---

## Task 9: Docs + CHANGELOG

**Files:**
- Modify: `docs/design/manage-architecture.md`, `CHANGELOG.md`

- [ ] **Step 1:** In `manage-architecture.md`: mark §4.3 Skills "精简版已落地（draft/published 单步、Platform Blob 分发、sync/manifest 契约就绪；Node 自动同步 Phase 2）"; add a "LLM 配置注册中心" subsection summarizing the table + API + resolve + security model (link this plan's spec).
- [ ] **Step 2:** In `CHANGELOG.md` under `## [Unreleased]` → `### 新增`, add:
  - `Manage LLM 配置注册中心（CRUD + resolve，多 Node/外部复用；key 掩码，resolve 明文仅本地/局域网）`
  - `Manage Skills 分发（精简）：Platform Blob API + skill 包 draft/publish + catalog/download + sync manifest`
  - `Manage Console 集成 PageAgent：命令栏复用 LLM 配置，自然语言操作控制台`
- [ ] **Step 3: Commit**

```bash
git add docs/design/manage-architecture.md CHANGELOG.md
git commit -m "docs(manage): record LLM config registry + skills distribution + PageAgent console"
```

---

## Final verification (before PR)

- [ ] `python -m unittest discover -s tests -p "test_*.py"` → OK (all green)
- [ ] `python -c "import manage.manage_app"` → no error
- [ ] `python run_manage.py` boots; `/console/` shows nav (LLM 配置 / Skills) + command bar
- [ ] Spec §11 acceptance items 1–5 all pass
- [ ] No files under `node/`, `client/`, `shared/`, `go.work*` changed (`git diff --name-only upstream/dev... | grep -E '^(node|client|shared)/' ` empty)
- [ ] Open PR: `gh pr create --repo DGS-ai-team/DAgents --base dev --head sgme94:feat/manage-llm-skills-pageagent --fill`
