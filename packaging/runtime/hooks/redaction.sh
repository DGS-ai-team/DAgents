#!/usr/bin/env bash
# redaction.sh — 示例 command hook：对 llm.after_call 的 assistant 文本做简单脱敏。
#
# stdin  — HookContext JSON（字段 snake_case，见 node/internal/hooks/context_json.go）
# stdout — Hook Result JSON
#
# 安装：cp packaging/runtime/hooks/redaction.sh .runtime/hooks/ && chmod +x .runtime/hooks/redaction.sh

set -euo pipefail

if ! command -v python3 >/dev/null 2>&1; then
  echo '{"action":"continue"}'
  exit 0
fi

exec python3 -c 'import json,re,sys
raw=sys.stdin.read()
if not raw.strip():
 print(json.dumps({"action":"continue"})); sys.exit(0)
try: ctx=json.loads(raw)
except json.JSONDecodeError:
 print(json.dumps({"action":"continue"})); sys.exit(0)
llm=ctx.get("llm_after_call") or {}
content=llm.get("assistant_content") or ""
if not content:
 print(json.dumps({"action":"continue"})); sys.exit(0)
patterns=[(re.compile(r"sk-[A-Za-z0-9]{20,}"),"sk-[REDACTED]"),(re.compile(r"Bearer\s+[A-Za-z0-9._-]+"),"Bearer [REDACTED]")]
redacted=content
for pat,repl in patterns: redacted=pat.sub(repl,redacted)
if redacted==content: print(json.dumps({"action":"continue"}))
else: print(json.dumps({"action":"continue","mutations":{"assistant_content":redacted}},ensure_ascii=False))'
