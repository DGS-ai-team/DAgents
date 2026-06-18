#!/usr/bin/env bash
# CentOS 7 特性冒烟（HTTP，无需 TUI / API Key）
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"
BASE="${DAGENTS_ENDPOINT:-http://127.0.0.1:18765}"

if ! docker compose ps --status running --quiet 2>/dev/null | grep -q .; then
  echo "error: dagents-node 未运行，请先: docker compose up --build -d" >&2
  exit 1
fi

pass=0
fail=0
check() {
  local name="$1"
  shift
  echo ""
  echo "== ${name} =="
  if "$@"; then
    echo "[PASS] ${name}"
    pass=$((pass + 1))
  else
    echo "[FAIL] ${name}" >&2
    fail=$((fail + 1))
  fi
}

json_field() {
  sed -n 's/.*"'"$1"'"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1
}

check "CentOS 7 + glibc 2.17 + static Node" docker compose exec -T dagents-node bash -c '
  cat /etc/redhat-release | grep -qi "CentOS Linux release 7"
  ldd --version 2>&1 | head -1 | grep -q "2.17"
  file /usr/local/bin/dagents-node | grep -q "statically linked"
'

check "Health + version" bash -c '
  body=$(curl -sf "'"${BASE}"'/health")
  echo "${body}"
  echo "${body}" | grep -q "\"status\":\"ok\""
  echo "${body}" | grep -q "\"version\":"
'

check "Agent capabilities (skills/filesystem)" bash -c '
  body=$(curl -sf "'"${BASE}"'/v1/agent/info")
  echo "${body}"
  echo "${body}" | grep -q "skills"
  echo "${body}" | grep -q "filesystem"
'

check "Policy bootstrap (.runtime/policy)" docker compose exec -T dagents-node test -f /workspace/.runtime/policy/tool.approval.txt

check "FS_ROOT demo file" docker compose exec -T dagents-node test -f /workspace/.runtime/demo/hello.txt

check "Session + SQLite persistence path" bash -c '
  CREATE=$(curl -sf -X POST "'"${BASE}"'/v1/sessions" -H "Content-Type: application/json" -d "{}")
  SID=$(printf "%s" "${CREATE}" | json_field session_id)
  echo "session_id=${SID}"
  test -n "${SID}"
  docker compose exec -T dagents-node test -d /workspace/.runtime/data
  LIST=$(curl -sf "'"${BASE}"'/v1/sessions")
  echo "${LIST}" | grep -q "${SID}"
'

check "Context: system_prompt + token estimate" bash -c '
  CREATE=$(curl -sf -X POST "'"${BASE}"'/v1/sessions" -H "Content-Type: application/json" -d "{}")
  SID=$(printf "%s" "${CREATE}" | json_field session_id)
  CTX=$(curl -sf "'"${BASE}"'/v1/sessions/${SID}/context")
  echo "${CTX}" | grep -q "system_prompt"
  echo "${CTX}" | grep -q "messages_total_tokens"
  echo "${CTX}" | grep -q "write-skill"
'

check "Skills: load → inject → unload" bash -c '
  CREATE=$(curl -sf -X POST "'"${BASE}"'/v1/sessions" -H "Content-Type: application/json" -d "{}")
  SID=$(printf "%s" "${CREATE}" | json_field session_id)
  LOAD=$(curl -sf -X POST "'"${BASE}"'/v1/sessions/${SID}/skills/load" \
    -H "Content-Type: application/json" -d "{\"skill_name\":\"write-skill\"}")
  echo "${LOAD}" | grep -q "write-skill"
  CTX=$(curl -sf "'"${BASE}"'/v1/sessions/${SID}/context")
  echo "${CTX}" | grep -q "write-skill"
  echo "${CTX}" | grep -q "### write-skill"
  UNLOAD=$(curl -sf -X POST "'"${BASE}"'/v1/sessions/${SID}/skills/unload" \
    -H "Content-Type: application/json" -d "{\"skill_name\":\"write-skill\"}")
  echo "${UNLOAD}" | grep -q "loaded_skills"
'

check "Mock message turn (queue + context growth)" bash -c '
  CREATE=$(curl -sf -X POST "'"${BASE}"'/v1/sessions" -H "Content-Type: application/json" -d "{}")
  SID=$(printf "%s" "${CREATE}" | json_field session_id)
  curl -sf -X POST "'"${BASE}"'/v1/messages" \
    -H "Content-Type: application/json" \
    -d "{\"session_id\":\"${SID}\",\"content\":\"feature tour ping\"}" | grep -q "\"accepted\":true"
  for i in 1 2 3 4 5 6 7 8 9 10; do
    CTX=$(curl -sf "'"${BASE}"'/v1/sessions/${SID}/context")
    if echo "${CTX}" | grep -q "feature tour ping"; then
      echo "context reflects user message"
      exit 0
    fi
    sleep 1
  done
  echo "timeout waiting for turn" >&2
  exit 1
'

echo ""
echo "summary: pass=${pass} fail=${fail}"
if [ "${fail}" -gt 0 ]; then
  echo "特性冒烟未通过。logs: docker compose logs dagents-node" >&2
  exit 1
fi
echo "CentOS 7 特性冒烟通过（${pass} 项）。"
