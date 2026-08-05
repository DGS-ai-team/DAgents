# AGENTS.md

## Cursor Cloud specific instructions

This is a polyglot monorepo (Go Agent Node + Go/Python clients + Vue Web UI + optional Manage). Standard setup/run/test commands are in [README.md](README.md). Notes below are cloud-agent gotchas only.

### Required vs optional services

| Service | Default | Notes |
|---------|---------|-------|
| **Agent Node** (`go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml`) | **Required** | `127.0.0.1:18765`; embeds Web UI at `/ui/` |
| **Manage** (`python3 run_manage.py`) | Optional | `0.0.0.0:8020` + Console `/console/` |
| **browser-service** | Optional | Only if testing browser tools |
| Desktop tray (`desktop/tray-tauri`) | Skip on Linux | Windows-only Tauri Shell；Go `desktop/tray` 已退役 |

### Non-obvious startup caveats

- **Web UI static assets are not committed.** `node/internal/webui` uses `go:embed all:static`. On a fresh clone (or after wiping `node/internal/webui/static/`), `go build`/`go test`/`go run` for the Node **fail** until `bash node/webui/build.sh` has run. Manage Console similarly needs `bash manage/console/build.sh` before `python3 run_manage.py` / Python tests that hit `/console/`.
- **Config:** `cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml` (gitignored). Example is **bootstrap-only** (`listen` / `local`); runtime root is fixed at `./.runtime` (not configurable). Other Node settings live in `./.runtime/node_settings.db`, LLM profiles in `llm_configs.db` — configure via Web UI. Legacy fat YAML is migrated on first start. For keyless smoke tests set `llm.mock: true` in Web UI / `node_settings` (or migrate from old yaml). For real DeepSeek ensure `OPENAI_API_KEY` is in the **Node process environment** (the Node reads `llm.api_key_env`, default `OPENAI_API_KEY`).
- **Process lock:** Node creates `packaging/agent-client/.dagents-node-*.lock`. If a restart fails with “another instance is already running”, remove the stale lock after confirming no live `dagents-node` process.
- **Python:** use `python3` (no `python` alias in this environment). Prefer `python3 run_manage.py` / `python3 -m unittest …` over bare `uvicorn` CLI (`~/.local/bin` may be missing from `PATH`).
- **Mock LLM echoes** the user text as the assistant reply. Real DeepSeek returns original answers — do not treat identical text as a UI failure when `mock: true`.

### Lint / test / run (pointers)

- Go tests/build: see README and `.github/workflows/go-ac.yml` (`go test ./node/... ./client/... ./shared/config/...`; build Web UI first).
- Python tests: `python3 -m unittest discover -s tests -p "test_*.py" -v` (build Manage Console first; see `.github/workflows/pr-tests.yml`).
- Web UI unit tests: `npm test --prefix node/webui/frontend`.
- No repo-level ESLint/golangci-lint gate; CI is unittest + Go test + vitest.
