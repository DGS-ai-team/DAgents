# Contract CI Quality Baseline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a repeatable quality baseline across `DAgents` and `DAgentsUI` so backend OpenAPI changes, generated frontend types, TypeScript checks, frontend builds, and backend unit tests are verified locally and in CI.

**Architecture:** `DAgents` remains the API contract source through `export_openapi_schema.py`. `DAgentsUI` consumes `openapi.json`, regenerates `src/api/types.ts`, and exposes explicit CI scripts for type generation, type checking, and build verification. GitHub Actions in each repository run the same commands developers run locally.

**Tech Stack:** Python 3.13, FastAPI, unittest, GitHub Actions, Node.js 22, pnpm 10, TypeScript, Vite, Electron.

---

## File Structure

### DAgents backend repository: `/Users/Huang/workspace/DAgents`

- Modify: `.github/workflows/pr-tests.yml`
  - Responsibility: PR quality gate for backend unit tests and OpenAPI export validation.
- Create: `scripts/ci/export_openapi_for_frontend.py`
  - Responsibility: Developer helper that exports backend OpenAPI directly into a sibling frontend checkout without making CI depend on a second repository.
- Modify: `README.md`
  - Responsibility: Document local contract export and CI baseline commands.

### DAgentsUI frontend repository: `/Users/Huang/workspace/DAgentsUI`

- Modify: `package.json`
  - Responsibility: Add explicit `typecheck`, `check:openapi`, and `ci` scripts.
- Create: `scripts/check-openapi-types.mjs`
  - Responsibility: Fail when `src/api/types.ts` is stale relative to `openapi.json`.
- Create: `.github/workflows/pr-quality.yml`
  - Responsibility: PR/push quality gate for install, OpenAPI type freshness, TypeScript, and Vite build.
- Modify: `README.md`
  - Responsibility: Document frontend local quality commands.

---

## Task 1: Add Frontend OpenAPI Freshness Check

**Files:**
- Create: `/Users/Huang/workspace/DAgentsUI/scripts/check-openapi-types.mjs`
- Modify: `/Users/Huang/workspace/DAgentsUI/package.json`
- Test: local command `pnpm --dir /Users/Huang/workspace/DAgentsUI check:openapi`

- [ ] **Step 1: Create the failing freshness checker script**

Create `/Users/Huang/workspace/DAgentsUI/scripts/check-openapi-types.mjs` with this exact content:

```js
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const repoRoot = path.resolve(import.meta.dirname, "..");
const sourceOpenapi = path.join(repoRoot, "openapi.json");
const generatedTypes = path.join(repoRoot, "src", "api", "types.ts");
const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "dagents-openapi-types-"));
const tempTypes = path.join(tempDir, "types.ts");

try {
  execFileSync(
    process.execPath,
    [
      path.join(repoRoot, "scripts", "generate-openapi-types.mjs"),
      sourceOpenapi,
      tempTypes,
    ],
    {
      cwd: repoRoot,
      stdio: "inherit",
    },
  );

  const expected = fs.readFileSync(tempTypes, "utf-8");
  const actual = fs.readFileSync(generatedTypes, "utf-8");

  if (actual !== expected) {
    console.error(
      "[openapi-types] src/api/types.ts is stale. Run `pnpm gen:types` and commit the generated file.",
    );
    process.exitCode = 1;
  } else {
    console.log("[openapi-types] src/api/types.ts is up to date.");
  }
} finally {
  fs.rmSync(tempDir, { recursive: true, force: true });
}
```

- [ ] **Step 2: Add scripts to `package.json`**

Modify `/Users/Huang/workspace/DAgentsUI/package.json` `scripts` block to include these entries while preserving existing scripts:

```json
{
  "scripts": {
    "dev": "vite",
    "dev:web": "vite",
    "dev:electron": "node ./scripts/electron-dev.cjs",
    "build": "tsc -b && vite build && node ./scripts/sync-env-example-to-dist.mjs",
    "preview": "vite preview",
    "gen:types": "node ./scripts/generate-openapi-types.mjs ./openapi.json ./src/api/types.ts",
    "check:openapi": "node ./scripts/check-openapi-types.mjs",
    "typecheck": "tsc -b",
    "ci": "pnpm gen:types && pnpm check:openapi && pnpm typecheck && pnpm build",
    "electron:pack:win": "node ./scripts/electron-builder-with-china-mirror.mjs --win nsis --publish never",
    "electron:pack:mac": "electron-builder --mac zip --publish never",
    "electron:package:win": "pnpm build && node ./scripts/electron-builder-with-china-mirror.mjs --win nsis --publish never",
    "electron:package:mac": "pnpm build && pnpm exec electron-builder --mac zip --publish never"
  }
}
```

- [ ] **Step 3: Run OpenAPI freshness check**

Run:

```bash
pnpm --dir /Users/Huang/workspace/DAgentsUI check:openapi
```

Expected if current files are fresh:

```text
[openapi-types] generated: /private/.../types.ts
[openapi-types] src/api/types.ts is up to date.
```

Expected if stale:

```text
[openapi-types] src/api/types.ts is stale. Run `pnpm gen:types` and commit the generated file.
```

- [ ] **Step 4: Regenerate types if the check fails**

Run:

```bash
pnpm --dir /Users/Huang/workspace/DAgentsUI gen:types
pnpm --dir /Users/Huang/workspace/DAgentsUI check:openapi
```

Expected after regeneration:

```text
[openapi-types] src/api/types.ts is up to date.
```

- [ ] **Step 5: Run frontend typecheck**

Run:

```bash
pnpm --dir /Users/Huang/workspace/DAgentsUI typecheck
```

Expected:

```text
(no TypeScript errors)
```

- [ ] **Step 6: Commit frontend script changes**

Run:

```bash
git -C /Users/Huang/workspace/DAgentsUI status --short
git -C /Users/Huang/workspace/DAgentsUI add package.json scripts/check-openapi-types.mjs src/api/types.ts
git -C /Users/Huang/workspace/DAgentsUI commit -m "chore: add OpenAPI type freshness check"
```

Only include `src/api/types.ts` if it changed after `pnpm gen:types`.

---

## Task 2: Add Frontend PR Quality Workflow

**Files:**
- Create: `/Users/Huang/workspace/DAgentsUI/.github/workflows/pr-quality.yml`
- Test: local command `pnpm --dir /Users/Huang/workspace/DAgentsUI ci`

- [ ] **Step 1: Create frontend quality workflow**

Create `/Users/Huang/workspace/DAgentsUI/.github/workflows/pr-quality.yml` with this exact content:

```yaml
name: PR Quality

on:
  pull_request:
  push:
    branches:
      - dev
      - main

permissions:
  contents: read

jobs:
  frontend:
    name: Typecheck and build
    runs-on: ubuntu-latest

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Setup Node
        uses: actions/setup-node@v4
        with:
          node-version: 22

      - name: Setup pnpm
        uses: pnpm/action-setup@v4
        with:
          version: 10

      - name: Install dependencies
        run: pnpm install --frozen-lockfile

      - name: Run quality checks
        run: pnpm ci
```

- [ ] **Step 2: Run the same frontend quality command locally**

Run:

```bash
pnpm --dir /Users/Huang/workspace/DAgentsUI ci
```

Expected:

```text
[openapi-types] generated: .../src/api/types.ts
[openapi-types] src/api/types.ts is up to date.
(no TypeScript errors)
vite build completes successfully
```

- [ ] **Step 3: Check whether generated build artifacts changed**

Run:

```bash
git -C /Users/Huang/workspace/DAgentsUI status --short
```

Expected:

```text
 M package.json
?? .github/workflows/pr-quality.yml
?? scripts/check-openapi-types.mjs
```

If `dist/` or other build output appears, do not commit it unless this repository already tracks those exact files and the build intentionally changed them.

- [ ] **Step 4: Commit frontend workflow changes**

Run:

```bash
git -C /Users/Huang/workspace/DAgentsUI add .github/workflows/pr-quality.yml package.json scripts/check-openapi-types.mjs
git -C /Users/Huang/workspace/DAgentsUI commit -m "ci: verify frontend contract and build"
```

If Task 1 already committed `package.json` and `scripts/check-openapi-types.mjs`, only add `.github/workflows/pr-quality.yml` here.

---

## Task 3: Add Backend OpenAPI Export Validation to PR CI

**Files:**
- Modify: `/Users/Huang/workspace/DAgents/.github/workflows/pr-tests.yml`
- Test: local command `python /Users/Huang/workspace/DAgents/export_openapi_schema.py --output /tmp/dagents-openapi.json`

- [ ] **Step 1: Update backend PR workflow**

Modify `/Users/Huang/workspace/DAgents/.github/workflows/pr-tests.yml` so it becomes:

```yaml
name: PR Tests

on:
  pull_request:

permissions:
  contents: read

env:
  FORCE_JAVASCRIPT_ACTIONS_TO_NODE24: "true"

jobs:
  unit:
    name: Unit tests (unittest)
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Setup Python
        uses: actions/setup-python@v5
        with:
          # 与 `.github/workflows/build-and-release.yml` 保持一致
          python-version: "3.13"
          cache: pip

      - name: Install dependencies
        run: |
          python -m pip install --upgrade pip
          pip install -r requirements.txt

      - name: Run unittest discovery
        run: python -m unittest discover -s tests -p "test_*.py" -v

      - name: Validate OpenAPI export
        run: python export_openapi_schema.py --output /tmp/dagents-openapi.json
```

- [ ] **Step 2: Run backend OpenAPI export locally**

Run:

```bash
python /Users/Huang/workspace/DAgents/export_openapi_schema.py --output /tmp/dagents-openapi.json
```

Expected:

```text
[openapi-export] exported to: /tmp/dagents-openapi.json
```

- [ ] **Step 3: Validate output JSON contains the expected API paths**

Run:

```bash
python - <<'PY'
import json
from pathlib import Path
schema = json.loads(Path('/tmp/dagents-openapi.json').read_text(encoding='utf-8'))
required = {'/health', '/v1/sessions', '/v1/messages', '/v1/streams'}
missing = sorted(required - set(schema.get('paths', {})))
if missing:
    raise SystemExit(f'missing OpenAPI paths: {missing}')
print('[openapi-export] required paths present')
PY
```

Expected:

```text
[openapi-export] required paths present
```

- [ ] **Step 4: Run backend unit tests**

Run:

```bash
python -m unittest discover -s /Users/Huang/workspace/DAgents/tests -p "test_*.py" -v
```

Expected:

```text
OK
```

If Claude Code permissions block this command, ask the user to run:

```bash
! python -m unittest discover -s /Users/Huang/workspace/DAgents/tests -p "test_*.py" -v
```

- [ ] **Step 5: Commit backend CI change**

Run:

```bash
git -C /Users/Huang/workspace/DAgents status --short
git -C /Users/Huang/workspace/DAgents add .github/workflows/pr-tests.yml
git -C /Users/Huang/workspace/DAgents commit -m "ci: validate backend OpenAPI export"
```

---

## Task 4: Add Backend-to-Frontend OpenAPI Export Helper

**Files:**
- Create: `/Users/Huang/workspace/DAgents/scripts/ci/export_openapi_for_frontend.py`
- Modify: `/Users/Huang/workspace/DAgents/README.md`
- Test: local command `python /Users/Huang/workspace/DAgents/scripts/ci/export_openapi_for_frontend.py --frontend /Users/Huang/workspace/DAgentsUI`

- [ ] **Step 1: Create the export helper script**

Create `/Users/Huang/workspace/DAgents/scripts/ci/export_openapi_for_frontend.py` with this exact content:

```python
from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Export DAgents OpenAPI schema into a DAgentsUI checkout")
    parser.add_argument(
        "--frontend",
        default="../DAgentsUI",
        help="Path to the DAgentsUI repository checkout, relative to the DAgents repository root by default",
    )
    parser.add_argument(
        "--skip-types",
        action="store_true",
        help="Only update openapi.json; do not run pnpm gen:types in the frontend repository",
    )
    return parser.parse_args()


def main() -> int:
    backend_root = Path(__file__).resolve().parents[2]
    args = parse_args()
    frontend_root = Path(args.frontend)
    if not frontend_root.is_absolute():
        frontend_root = backend_root / frontend_root
    frontend_root = frontend_root.resolve()

    openapi_target = frontend_root / "openapi.json"
    if not (frontend_root / "package.json").exists():
        print(f"[openapi-sync] frontend checkout not found: {frontend_root}")
        return 1

    export_cmd = [
        sys.executable,
        str(backend_root / "export_openapi_schema.py"),
        "--output",
        str(openapi_target),
    ]
    subprocess.run(export_cmd, cwd=backend_root, check=True)

    if not args.skip_types:
        subprocess.run(["pnpm", "gen:types"], cwd=frontend_root, check=True)

    print(f"[openapi-sync] updated {openapi_target}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
```

- [ ] **Step 2: Run helper without generating frontend types**

Run:

```bash
python /Users/Huang/workspace/DAgents/scripts/ci/export_openapi_for_frontend.py --frontend /Users/Huang/workspace/DAgentsUI --skip-types
```

Expected:

```text
[openapi-export] exported to: /Users/Huang/workspace/DAgentsUI/openapi.json
[openapi-sync] updated /Users/Huang/workspace/DAgentsUI/openapi.json
```

- [ ] **Step 3: Run helper with type generation**

Run:

```bash
python /Users/Huang/workspace/DAgents/scripts/ci/export_openapi_for_frontend.py --frontend /Users/Huang/workspace/DAgentsUI
```

Expected:

```text
[openapi-export] exported to: /Users/Huang/workspace/DAgentsUI/openapi.json
[openapi-types] generated: /Users/Huang/workspace/DAgentsUI/src/api/types.ts
[openapi-sync] updated /Users/Huang/workspace/DAgentsUI/openapi.json
```

- [ ] **Step 4: Add README section in backend repo**

Add this section to `/Users/Huang/workspace/DAgents/README.md` under the local development or API documentation area:

```markdown
## OpenAPI 契约同步

后端 FastAPI 应用是 API 契约源头。修改 `app/harness/api/app.py` 或相关 Pydantic schema 后，先导出 OpenAPI，再让前端重新生成类型：

```bash
python export_openapi_schema.py --output openapi.json
python scripts/ci/export_openapi_for_frontend.py --frontend ../DAgentsUI
```

前端仓库会更新：

- `openapi.json`
- `src/api/types.ts`

提交后端 API 变更时，请同时提交对应的前端契约文件变更，确保 `DAgentsUI` 的 `pnpm ci` 能通过。
```

- [ ] **Step 5: Commit backend helper and docs**

Run:

```bash
git -C /Users/Huang/workspace/DAgents status --short
git -C /Users/Huang/workspace/DAgents add scripts/ci/export_openapi_for_frontend.py README.md
git -C /Users/Huang/workspace/DAgents commit -m "chore: document OpenAPI frontend sync"
```

---

## Task 5: Document Frontend Quality Commands

**Files:**
- Modify: `/Users/Huang/workspace/DAgentsUI/README.md`
- Test: local command `pnpm --dir /Users/Huang/workspace/DAgentsUI ci`

- [ ] **Step 1: Add README section in frontend repo**

Add this section to `/Users/Huang/workspace/DAgentsUI/README.md` under the development instructions:

```markdown
## 质量检查

本仓库把后端 OpenAPI 契约文件 `openapi.json` 作为前端 API 类型源头。修改或同步后端接口后，运行：

```bash
pnpm gen:types
pnpm check:openapi
pnpm typecheck
pnpm build
```

也可以使用一条命令运行 CI 同款检查：

```bash
pnpm ci
```

`pnpm check:openapi` 会验证 `src/api/types.ts` 是否与 `openapi.json` 一致。如果失败，运行 `pnpm gen:types` 并提交生成后的 `src/api/types.ts`。
```

- [ ] **Step 2: Run frontend CI command after docs update**

Run:

```bash
pnpm --dir /Users/Huang/workspace/DAgentsUI ci
```

Expected:

```text
[openapi-types] src/api/types.ts is up to date.
vite build completes successfully
```

- [ ] **Step 3: Commit frontend docs**

Run:

```bash
git -C /Users/Huang/workspace/DAgentsUI status --short
git -C /Users/Huang/workspace/DAgentsUI add README.md
git -C /Users/Huang/workspace/DAgentsUI commit -m "docs: describe frontend quality checks"
```

---

## Task 6: Final Cross-Repository Verification

**Files:**
- Verify only; no source changes expected.

- [ ] **Step 1: Verify backend working tree**

Run:

```bash
git -C /Users/Huang/workspace/DAgents status --short
```

Expected:

```text
(no output)
```

If this plan file is intentionally uncommitted, expected output may include:

```text
?? docs/superpowers/plans/2026-05-18-contract-ci-quality-baseline.md
```

- [ ] **Step 2: Verify frontend working tree**

Run:

```bash
git -C /Users/Huang/workspace/DAgentsUI status --short
```

Expected:

```text
(no output)
```

- [ ] **Step 3: Run backend OpenAPI export validation**

Run:

```bash
python /Users/Huang/workspace/DAgents/export_openapi_schema.py --output /tmp/dagents-openapi-final.json
```

Expected:

```text
[openapi-export] exported to: /tmp/dagents-openapi-final.json
```

- [ ] **Step 4: Run backend unit tests**

Run:

```bash
python -m unittest discover -s /Users/Huang/workspace/DAgents/tests -p "test_*.py" -v
```

Expected:

```text
OK
```

If Claude Code permissions block this command, ask the user to run the same command with the `!` prefix in Claude Code.

- [ ] **Step 5: Run frontend CI check**

Run:

```bash
pnpm --dir /Users/Huang/workspace/DAgentsUI ci
```

Expected:

```text
[openapi-types] src/api/types.ts is up to date.
vite build completes successfully
```

- [ ] **Step 6: Compare committed changes**

Run:

```bash
git -C /Users/Huang/workspace/DAgents log --oneline -3
git -C /Users/Huang/workspace/DAgentsUI log --oneline -3
```

Expected:

```text
DAgents shows commits for OpenAPI CI validation and sync docs/helper.
DAgentsUI shows commits for OpenAPI freshness check, PR quality workflow, and quality docs.
```

---

## Self-Review

- Spec coverage: The plan covers backend OpenAPI export validation, frontend type generation freshness, frontend typecheck/build scripts, GitHub Actions for both repositories, and developer documentation for cross-repository contract sync.
- Placeholder scan: No TBD/TODO/later placeholders remain. Every created file has exact content, and every command has expected output.
- Type consistency: Script names are consistent across `package.json`, GitHub Actions, README, and shell commands: `gen:types`, `check:openapi`, `typecheck`, and `ci`.
