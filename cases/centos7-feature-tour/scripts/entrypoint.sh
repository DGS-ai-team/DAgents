#!/bin/sh
set -eu

RUNTIME=/workspace/.runtime
mkdir -p "${RUNTIME}/skills" "${RUNTIME}/data" "${RUNTIME}/policy"

if [ ! -f "${RUNTIME}/skills/write-skill/SKILL.md" ]; then
  cp -a /opt/dagents/seed/skills/. "${RUNTIME}/skills/"
fi
if [ ! -f "${RUNTIME}/policy/tool.approval.txt" ]; then
  cp -a /opt/dagents/seed/policy/. "${RUNTIME}/policy/"
fi

# 演示 runtime_root 下的 Agent workspace 只读文件（供真实 LLM + read_file 联调）
if [ ! -f "${RUNTIME}/demo/hello.txt" ]; then
  mkdir -p "${RUNTIME}/demo"
  printf 'DAgents feature tour sample file.\n' > "${RUNTIME}/demo/hello.txt"
fi

if [ "${LLM_MOCK:-true}" = "false" ] || [ "${LLM_MOCK:-true}" = "0" ]; then
  sed -i 's/^  mock: true/  mock: false/' /etc/dagents/config.yaml
fi

exec dagents-node -config /etc/dagents/config.yaml
