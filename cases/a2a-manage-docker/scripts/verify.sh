#!/usr/bin/env bash
# 合规场景 A2A 联调：运维助手 node-b 向合规助手 node-a 发起咨询
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

MANAGE_URL="${MANAGE_URL:-http://127.0.0.1:8020}"
POLL_SECONDS="${POLL_SECONDS:-90}"
DISCOVERY_GROUP="${DISCOVERY_GROUP:-a2a-lab}"

if ! docker compose ps --status running --quiet 2>/dev/null | grep -q .; then
  echo "error: 栈未运行，请先: docker compose up --build -d" >&2
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

wait_registry_agent() {
  local agent_id="$1"
  local i=0
  while [ "${i}" -lt 60 ]; do
    if curl -sf "${MANAGE_URL}/v1/registry/agents?status=all" \
      | python3 -c "import json,sys; ids=[a['agent_id'] for a in json.load(sys.stdin)['agents']]; sys.exit(0 if '${agent_id}' in ids else 1)"; then
      return 0
    fi
    sleep 2
    i=$((i + 2))
  done
  return 1
}

assign_groups() {
  local agent_id="$1"
  curl -sf -X PATCH "${MANAGE_URL}/v1/registry/agents/${agent_id}/groups" \
    -H "Content-Type: application/json" \
    -d "{\"discovery_group\":[\"${DISCOVERY_GROUP}\"]}" >/dev/null
}

poll_task_result() {
  local task_id="$1"
  local caller="$2"
  local deadline=$((SECONDS + POLL_SECONDS))
  local status="" result=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    local body
    body=$(curl -sf "${MANAGE_URL}/v1/a2a/tasks/${task_id}?caller_agent_id=${caller}" \
      -H "x-dagents-agent-id: ${caller}")
    status=$(printf "%s" "${body}" | python3 -c "import json,sys; print(json.load(sys.stdin)['task']['status'])")
    result=$(printf "%s" "${body}" | python3 -c "import json,sys; print(json.load(sys.stdin)['task'].get('result_text',''))")
    echo "  status=${status} result=${result}"
    if [ "${status}" = "completed" ]; then
      printf "%s" "${result}"
      return 0
    fi
    sleep 2
  done
  return 1
}

submit_compliance_inquiry() {
  local content="$1"
  local payload
  payload=$(CONTENT="$content" python3 -c 'import json,os; print(json.dumps({"from_agent_id":"node-b","to_agent_id":"node-a","kind":"invoke","content":os.environ["CONTENT"]}))')
  curl -sf -X POST "${MANAGE_URL}/v1/a2a/tasks" \
    -H "Content-Type: application/json" \
    -H "x-dagents-agent-id: node-b" \
    -d "${payload}"
}

if [ -f "${ROOT}/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT}/.env"
  set +a
fi

export -f wait_registry_agent assign_groups poll_task_result submit_compliance_inquiry
export MANAGE_URL POLL_SECONDS DISCOVERY_GROUP

check "Manage /health" curl -sf "${MANAGE_URL}/health" | grep -q '"status":"ok"'

check "Nodes registered" bash -ec '
  wait_registry_agent node-a
  wait_registry_agent node-b
  assign_groups node-a
  assign_groups node-b
'

check "Agent Card 已注册（合规助手）" bash -ec "
  curl -sf '${MANAGE_URL}/v1/registry/agents/discover?discovery_group=${DISCOVERY_GROUP}' \\
    | python3 -c \"import json,sys; agents=json.load(sys.stdin)['agents']; a=next(x for x in agents if x['agent_id']=='node-a'); assert a['name']=='合规助手', a; assert a['card']['metadata']['role']=='compliance', a; print('ok')\"
"

check "Agent Card 已注册（运维助手）" bash -ec "
  curl -sf '${MANAGE_URL}/v1/registry/agents/node-b?discovery_group=${DISCOVERY_GROUP}' \\
    | python3 -c \"import json,sys; a=json.load(sys.stdin); assert a['card']['metadata']['compliance_peer']=='node-a', a; assert a['card']['name']=='运维执行助手', a; print('ok')\"
"

check "custom.md 与 Agent Card 角色" bash -c '
  docker compose exec -T node-a grep -q "R-PII-01" /workspace/.runtime/prompt_context/custom.md
  docker compose exec -T node-b grep -q "node-a" /workspace/.runtime/prompt_context/custom.md
  curl -sf "'"${MANAGE_URL}"'/v1/registry/agents/node-a?discovery_group='"${DISCOVERY_GROUP}"'" \
    | python3 -c "import json,sys; assert json.load(sys.stdin)[\"card\"][\"metadata\"][\"role\"]==\"compliance\""
  curl -sf "'"${MANAGE_URL}"'/v1/registry/agents/node-b?discovery_group='"${DISCOVERY_GROUP}"'" \
    | python3 -c "import json,sys; assert json.load(sys.stdin)[\"card\"][\"metadata\"][\"role\"]==\"ops\""
'

check "场景1：脱敏统计出境 + CHG 合规咨询" bash -ec '
  CREATE=$(submit_compliance_inquiry "【合规咨询】拟将脱敏后的日活统计发送至已备案 vendor，变更单 CHG-2026-0142，是否可执行？")
  echo "${CREATE}"
  TASK_ID=$(printf "%s" "${CREATE}" | python3 -c "import json,sys; print(json.load(sys.stdin)[\"task_id\"])")
  RESULT=$(poll_task_result "${TASK_ID}" node-b)
  echo "final: ${RESULT}"
  test -n "${RESULT}"
'

check "场景2：PII 出境境外 SaaS 合规咨询" bash -ec '
  CREATE=$(submit_compliance_inquiry "【合规咨询】拟将含手机号、姓名的客户明细导出至境外 SaaS 做分析，当前无变更单。")
  TASK_ID=$(printf "%s" "${CREATE}" | python3 -c "import json,sys; print(json.load(sys.stdin)[\"task_id\"])")
  RESULT=$(poll_task_result "${TASK_ID}" node-b)
  echo "final: ${RESULT}"
  test -n "${RESULT}"
'

check "场景3：生产发布合规咨询" bash -ec '
  CREATE=$(submit_compliance_inquiry "【合规咨询】计划今晚生产环境发布新版本，暂无变更单。")
  TASK_ID=$(printf "%s" "${CREATE}" | python3 -c "import json,sys; print(json.load(sys.stdin)[\"task_id\"])")
  RESULT=$(poll_task_result "${TASK_ID}" node-b)
  echo "final: ${RESULT}"
  test -n "${RESULT}"
'

echo ""
echo "summary: pass=${pass} fail=${fail}"
if [ "${fail}" -gt 0 ]; then
  echo "合规 A2A 联调未通过。logs: docker compose logs manage node-a node-b" >&2
  exit 1
fi
echo "合规 A2A Docker 联调通过（${pass} 项）。"
