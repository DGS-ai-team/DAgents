import json, os, tempfile, unittest
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


class LLMAllowedGroupsTest(unittest.TestCase):
    """allowed_groups 作为 discovery_group 命名空间的可见性约束：
    member 仅能看到 allowed_groups 含其分组或为空的配置。"""

    ADMIN = "admin-tok"
    MEMBER = "member-tok"

    def setUp(self):
        self._prev = os.environ.get("MANAGE_TOKENS")
        os.environ["MANAGE_TOKENS"] = json.dumps([
            {"id": "adm", "token": self.ADMIN, "role": "admin"},
            {"id": "ops", "token": self.MEMBER, "role": "member", "discovery_groups": ["ops"]},
        ])
        self.c = _client()

    def tearDown(self):
        if self._prev is None:
            os.environ.pop("MANAGE_TOKENS", None)
        else:
            os.environ["MANAGE_TOKENS"] = self._prev

    def _h(self, token):
        return {"x-dagents-a2a-token": token}

    def _create(self, name, allowed_groups, is_default=False):
        body = dict(name=name, provider="openai", base_url="http://h/v1",
                    model="m", api_key="sk-abcd1234",
                    is_default=is_default, allowed_groups=allowed_groups)
        r = self.c.post("/v1/llm/configs", json=body, headers=self._h(self.ADMIN))
        self.assertEqual(r.status_code, 200, r.text)
        return r.json()["id"]

    def test_member_sees_only_permitted_groups(self):
        pub = self._create("public", [])
        ops = self._create("ops-only", ["ops"])
        sec = self._create("sec-only", ["sec"])
        ids = {c["id"] for c in self.c.get("/v1/llm/configs", headers=self._h(self.MEMBER)).json()}
        self.assertEqual(ids, {pub, ops})            # sec hidden from ops member
        # hidden config -> 404 on get & resolve
        self.assertEqual(self.c.get(f"/v1/llm/configs/{sec}", headers=self._h(self.MEMBER)).status_code, 404)
        self.assertEqual(self.c.get(f"/v1/llm/configs/{sec}/resolve", headers=self._h(self.MEMBER)).status_code, 404)
        # permitted config resolves
        self.assertEqual(self.c.get(f"/v1/llm/configs/{ops}/resolve", headers=self._h(self.MEMBER)).status_code, 200)
        # admin sees all three
        self.assertEqual(len(self.c.get("/v1/llm/configs", headers=self._h(self.ADMIN)).json()), 3)

    def test_default_resolve_respects_groups(self):
        self._create("sec-default", ["sec"], is_default=True)
        self.assertEqual(self.c.get("/v1/llm/configs/default/resolve", headers=self._h(self.MEMBER)).status_code, 404)
        self.assertEqual(self.c.get("/v1/llm/configs/default/resolve", headers=self._h(self.ADMIN)).status_code, 200)
