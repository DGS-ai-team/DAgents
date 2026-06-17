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
