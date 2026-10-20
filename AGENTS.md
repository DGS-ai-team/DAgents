# AGENTS.md

## Cursor Cloud specific instructions

This is a polyglot monorepo (Go Agent Node + Go/Python clients + Tauri Desktop UI + optional Manage). Standard setup/run/test commands are in [README.md](README.md). Notes below are cloud-agent gotchas only.

### Required vs optional services

| Service | Default | Notes |
|---------|---------|-------|
| **Agent Node** (`go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml`) | **Required** | `127.0.0.1:18765`; Node runtime API only (no embedded `/ui/`) |
| **Manage** (`python3 run_manage.py`) | Optional | `0.0.0.0:8020` + Console `/console/` |
| **browser-service** | Optional | Only if testing browser tools |
| Desktop | Skip on Linux | Windows Desktop is the supported shell (`desktop/tray-tauri`); Go tray was retired |

### Non-obvious startup caveats

- **Desktop is the only Agent UI entry.** Node exposes its runtime API and does not serve `/ui/`; the Vue sources under `node/webui/frontend` are shared source only and have no standalone browser entry or Vite dev/build command. Manage Console still needs `bash manage/console/build.sh` before `python3 run_manage.py` / Python tests that hit `/console/`.
- **Config:** `cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml` (gitignored). Example is **Node bootstrap-only** (`listen` only); runtime root is fixed at `./.runtime` relative to the process working directory (not configurable). Other Node settings live in `./.runtime/node_settings.db`, LLM profiles in `llm_configs.db` — configure via DAgents Desktop or the local Setup API. An empty settings database is seeded with product defaults; do not assume legacy fat YAML will restore business settings. For keyless smoke tests enable Mock in a Desktop LLM profile. For v0.11 data restores, preserve the whole `.runtime` including `.llm_secret_key`; see [migration guidance](docs/user/migrate-from-v0.11.md). Provider environment variables must be present in the **Node process environment** when that credential mode is used.
- **Process lock:** Node creates `packaging/agent-client/.dagents-node-*.lock`. If a restart fails with “another instance is already running”, remove the stale lock after confirming no live `dagents-node` process.
- **Python:** use `python3` (no `python` alias in this environment). Prefer `python3 run_manage.py` / `python3 -m unittest …` over bare `uvicorn` CLI (`~/.local/bin` may be missing from `PATH`).
- **Mock LLM echoes** the user text as the assistant reply. Real DeepSeek returns original answers — do not treat identical text as a UI failure when `mock: true`.
- **Workgroup（v0.10.4）**：需 Manage + Node `manage.workgroup`；成员使用 Node 已注册的 AgentRef，工具权限仍由 Agent 快照与 Node policy 共同约束。操作说明见 [docs/user/workgroups.md](docs/user/workgroups.md)，回归入口见 [docs/development.md](docs/development.md)。
- **Windows 无 bash 时**：Manage Console 可使用 `npm run build --prefix manage/console/frontend`。

### Lint / test / run (pointers)

- Go tests/build: see README and `.github/workflows/go-ac.yml` (all Go modules are covered; Node builds do not need frontend assets).
- Python tests: `python3 -m unittest discover -s tests -p "test_*.py" -v` (build Manage Console first; see `.github/workflows/pr-tests.yml`).
- Shared Desktop UI source tests: `npm test --prefix node/webui/frontend`.
- No repo-level ESLint/golangci-lint gate; CI is unittest + Go test + vitest.
