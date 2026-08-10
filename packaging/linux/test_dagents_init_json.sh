#!/usr/bin/env bash
# 校验 dagents init 的 JSON 组装（不启动 Node）。
set -euo pipefail

build_init_setup_json() {
  PREFERRED_NAME="${1}" NODE_NAME="${2}" PROVIDER="${3}" MODEL="${4}" BASE_URL="${5}" API_KEY="${6}" MOCK="${7}" \
    python3 - <<'PY'
import json, os

preferred = (os.environ.get("PREFERRED_NAME") or "").strip()
node_name = (os.environ.get("NODE_NAME") or "").strip()
provider = (os.environ.get("PROVIDER") or "").strip().lower()
model = (os.environ.get("MODEL") or "").strip()
base_url = (os.environ.get("BASE_URL") or "").strip()
api_key = (os.environ.get("API_KEY") or "").strip()
mock = (os.environ.get("MOCK") or "").strip() in ("1", "true", "yes", "on")

if not preferred:
    raise SystemExit("preferred_name is required")
if not node_name:
    raise SystemExit("node name is required")
if not provider:
    raise SystemExit("provider is required")

if mock or provider == "mock":
    provider = "mock"
    mock = True
    if not model:
        model = "mock"

if not model:
    raise SystemExit("model is required (non-mock providers)")

profile_id = model if model else ("mock" if mock else "default")
prof = {
    "id": profile_id,
    "provider": provider,
    "model": model,
    "mock": mock,
    "multimodal_enabled": False,
}
if base_url:
    prof["base_url"] = base_url
if api_key:
    prof["api_key"] = api_key

body = {
    "user": {"preferred_name": preferred},
    "agent": {"name": node_name, "description": ""},
    "llm": {"active": profile_id, "profiles": [prof]},
    "onboarding": {"node_profile_completed": True},
}
print(json.dumps(body, ensure_ascii=False))
PY
}

assert_payload() {
  local json="$1"
  local py="$2"
  INIT_JSON="${json}" python3 -c "${py}"
}

json="$(build_init_setup_json "xiaoming" "lab-node" "mock" "" "" "" "1")"
assert_payload "${json}" '
import json, os
body = json.loads(os.environ["INIT_JSON"])
assert body["user"]["preferred_name"] == "xiaoming"
assert body["agent"]["name"] == "lab-node"
assert body["onboarding"]["node_profile_completed"] is True
assert body["llm"]["profiles"][0]["mock"] is True
assert body["llm"]["profiles"][0]["provider"] == "mock"
print("ok mock payload")
'

json="$(build_init_setup_json "ops" "n1" "deepseek" "deepseek-chat" "https://api.example" "sk-test" "0")"
assert_payload "${json}" '
import json, os
body = json.loads(os.environ["INIT_JSON"])
p = body["llm"]["profiles"][0]
assert p["provider"] == "deepseek"
assert p["model"] == "deepseek-chat"
assert p["api_key"] == "sk-test"
assert p["base_url"] == "https://api.example"
assert p["mock"] is False
print("ok deepseek payload")
'

echo "all dagents init json checks passed"
