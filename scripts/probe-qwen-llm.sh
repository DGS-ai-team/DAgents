#!/usr/bin/env bash
# 探测百炼 Qwen OpenAI 兼容接口（需可访问阿里云 + 有效 API Key）
set -euo pipefail

BASE_URL="${QWEN_BASE_URL:-https://ws-i94zryik2nyqcc90.cn-beijing.maas.aliyuncs.com/compatible-mode/v1}"
MODEL="${QWEN_MODEL:-qwen-plus}"
KEY_ENV="${QWEN_API_KEY_ENV:-QWEN_API_KEY}"

if [[ -z "${QWEN_API_KEY:-}" ]]; then
  QWEN_API_KEY="${!KEY_ENV:-}"
fi
if [[ -z "${QWEN_API_KEY:-}" ]]; then
  echo "请设置 QWEN_API_KEY 或 export ${KEY_ENV}=sk-..." >&2
  exit 1
fi

BASE_URL="${BASE_URL%/}"
BASE_URL="${BASE_URL%/chat/completions}"
ENDPOINT="${BASE_URL}/chat/completions"

echo "POST ${ENDPOINT}"
echo "model=${MODEL}"

curl -sS -w "\nHTTP=%{http_code}\n" -X POST "${ENDPOINT}" \
  -H "Authorization: Bearer ${QWEN_API_KEY}" \
  -H "Content-Type: application/json" \
  -d "{\"model\":\"${MODEL}\",\"messages\":[{\"role\":\"user\",\"content\":\"只回复 ok\"}],\"stream\":false}"

echo
echo "--- go integration test ---"
cd "$(dirname "$0")/.."
QWEN_API_KEY="${QWEN_API_KEY}" QWEN_BASE_URL="${BASE_URL}" QWEN_MODEL="${MODEL}" \
  go test -tags integration -count=1 -run TestQwenWorkspaceLive -v ./node/internal/llm/
