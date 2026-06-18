#!/bin/sh
set -eu

RUNTIME=/workspace/.runtime
PROMPT_CTX="${RUNTIME}/prompt_context"
mkdir -p "${RUNTIME}/skills" "${RUNTIME}/data" "${RUNTIME}/policy" "${PROMPT_CTX}"

if [ ! -f "${RUNTIME}/skills/write-skill/SKILL.md" ]; then
  cp -a /opt/dagents/seed/skills/. "${RUNTIME}/skills/"
fi
if [ ! -f "${RUNTIME}/policy/tool.approval.txt" ]; then
  cp -a /opt/dagents/seed/policy/. "${RUNTIME}/policy/"
fi

# 每次启动覆盖 case 角色专用 policy（如 node-a 的 bash_run=always）
ROLE_POLICY="/opt/dagents/case-policy-root/node-${NODE_ROLE:-a}"
if [ -f "${ROLE_POLICY}/tool.approval.txt" ]; then
  cp -a "${ROLE_POLICY}/." "${RUNTIME}/policy/"
fi

# 每次启动写入 case 专用 custom.md（覆盖空占位，便于 A2A 读取 codeword）
cp /etc/dagents/custom.md.seed "${PROMPT_CTX}/custom.md"

if [ "${LLM_MOCK:-true}" = "false" ] || [ "${LLM_MOCK:-true}" = "0" ]; then
  sed -i 's/^  mock: true/  mock: false/' /etc/dagents/config.yaml
fi

exec dagents-node -config /etc/dagents/config.yaml
