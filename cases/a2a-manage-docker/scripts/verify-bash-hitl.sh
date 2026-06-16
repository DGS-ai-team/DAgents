#!/usr/bin/env bash
# A2A bash HITL 联调：不经 TUI，经 Manage API 模拟 caller 审批，验证 node-a 续跑并完成 bash_run。
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

MANAGE_URL="${MANAGE_URL:-http://127.0.0.1:8020}"
NODE_A_URL="${NODE_A_URL:-http://127.0.0.1:18765}"
POLL_SECONDS="${POLL_SECONDS:-120}"
STEP_TIMEOUT="${STEP_TIMEOUT:-90}"

if ! docker compose ps --status running --quiet 2>/dev/null | grep -q .; then
  echo "error: 栈未运行，请先: docker compose up --build -d" >&2
  exit 1
fi

log() { echo "[verify-bash-hitl] $*"; }
fail() { echo "[FAIL] $*" >&2; exit 1; }

wait_task_status() {
  local task_id="$1" caller="$2" want_status="$3" deadline=$((SECONDS + STEP_TIMEOUT))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    local body status
    body=$(curl -sf "${MANAGE_URL}/v1/a2a/tasks/${task_id}?caller_agent_id=${caller}" \
      -H "x-dagents-agent-id: ${caller}")
    status=$(printf "%s" "${body}" | python3 -c "import json,sys; print(json.load(sys.stdin)['task']['status'])")
    if [ "${status}" = "${want_status}" ]; then
      printf "%s" "${body}"
      return 0
    fi
    sleep 1
  done
  return 1
}

poll_task_completed() {
  local task_id="$1" caller="$2" deadline=$((SECONDS + POLL_SECONDS))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    local body status result
    body=$(curl -sf "${MANAGE_URL}/v1/a2a/tasks/${task_id}?caller_agent_id=${caller}" \
      -H "x-dagents-agent-id: ${caller}")
    status=$(printf "%s" "${body}" | python3 -c "import json,sys; print(json.load(sys.stdin)['task']['status'])")
    result=$(printf "%s" "${body}" | python3 -c "import json,sys; print(json.load(sys.stdin)['task'].get('result_text',''))")
    log "task status=${status}"
    if [ "${status}" = "completed" ]; then
      printf "%s" "${result}"
      return 0
    fi
    if [ "${status}" = "failed" ] || [ "${status}" = "expired" ]; then
      echo "${body}" >&2
      return 1
    fi
    sleep 2
  done
  return 1
}

wait_session_tool_result() {
  local session_id="$1" deadline=$((SECONDS + STEP_TIMEOUT))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    local body tool_count msgs
    body=$(curl -sf "${NODE_A_URL}/v1/sessions/${session_id}/context")
    tool_count=$(printf "%s" "${body}" | python3 -c "
import json,sys
d=json.load(sys.stdin)
msgs=d.get('recent_messages') or d.get('messages') or []
print(sum(1 for m in msgs if m.get('role')=='tool'))
")
    msgs=$(printf "%s" "${body}" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('messages_count',0), d.get('turn_state',''), d.get('run_turn_phase',''), d.get('pending_tool_calls_count',0))")
    log "session ${session_id}: msgs/state ${msgs}, tool_msgs=${tool_count}"
    if [ "${tool_count}" -ge 1 ]; then
      return 0
    fi
    sleep 2
  done
  return 1
}

submit_time_inquiry() {
  local payload
  payload=$(python3 -c 'import json; print(json.dumps({
    "from_agent_id":"node-b",
    "to_agent_id":"node-a",
    "kind":"invoke",
    "content":"【合规咨询】请查看当前系统时间，执行 date 命令并将输出回复",
    "caller_session_id":"verify-bash-hitl-sess"
  }))')
  curl -sf -X POST "${MANAGE_URL}/v1/a2a/tasks" \
    -H "Content-Type: application/json" \
    -H "x-dagents-agent-id: node-b" \
    -d "${payload}"
}

auto_caller_resume() {
  TASK_ID="${1}" TASK_BODY="${2}" python3 <<'PY'
import json, os, urllib.request

body = json.loads(os.environ["TASK_BODY"])
task = body["task"]
payload = json.loads(task["result_text"])
event = payload.get("event_data") or {}
approval_id = event.get("approval_id", "")
tool_calls = (event.get("approval_args") or {}).get("tool_calls") or []
approved = [tc["id"] for tc in tool_calls if tc.get("id")]

manage = os.environ.get("MANAGE_URL", "http://127.0.0.1:8020")
task_id = os.environ["TASK_ID"]
headers = {"Content-Type": "application/json", "x-dagents-agent-id": "node-b"}

def post(path, data):
    req = urllib.request.Request(
        manage + path,
        data=json.dumps(data).encode(),
        headers=headers,
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        return resp.read()

post(f"/v1/a2a/tasks/{task_id}/caller_notify", {"caller_agent_id": "node-b"})
resume = {
    "caller_agent_id": "node-b",
    "resume_value": {
        "type": "selection",
        "approval_id": approval_id,
        "approved": approved,
        "rejected": [],
    },
}
post(f"/v1/a2a/tasks/{task_id}/caller_resume", resume)
print(json.dumps({"approval_id": approval_id, "approved": approved}))
PY
}

export MANAGE_URL NODE_A_URL
log "提交查时合规咨询 Task"
CREATE=$(submit_time_inquiry)
echo "${CREATE}"
TASK_ID=$(printf "%s" "${CREATE}" | python3 -c "import json,sys; print(json.load(sys.stdin)['task_id'])")
log "task_id=${TASK_ID}"

log "等待 Task 进入 awaiting_caller（node-a 已 requires_input）"
TASK_BODY=$(wait_task_status "${TASK_ID}" "node-b" "awaiting_caller") || fail "未进入 awaiting_caller"
CALLEE_SESSION=$(printf "%s" "${TASK_BODY}" | python3 -c "import json,sys; print(json.load(sys.stdin)['task']['callee_session_id'])")
log "callee_session_id=${CALLEE_SESSION}"

log "经 Manage API 模拟 node-b 审批（不经 TUI）"
AUTO=$(auto_caller_resume "${TASK_ID}" "${TASK_BODY}")
log "caller_resume: ${AUTO}"

log "等待 node-a inbox session 出现 bash tool 结果"
wait_session_tool_result "${CALLEE_SESSION}" || fail "审批后 node-a 未写入 bash tool 结果（可能卡在 awaiting_tool_execution）"

log "等待 Task completed"
RESULT=$(poll_task_completed "${TASK_ID}" "node-b") || fail "Task 未完成"
log "final result: ${RESULT}"

if ! printf "%s" "${RESULT}" | grep -qE 'APPROVED|DENIED|BASH_RESULT|UTC|GMT'; then
  fail "结果不像合规回复: ${RESULT}"
fi

log "通过：审批经 Manage 回到 node-a，bash_run 已执行且 Task 完成"
