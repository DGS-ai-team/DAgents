# RHEL 6.9 验收清单（Go Agent Node + Client）

在 **RHEL 6.9 无 GUI、SSH 交互** 场景下，按序执行并记录结果。构建矩阵见 [go-node-compatibility.md](./go-node-compatibility.md)。

---

## 0. 前置信息（记录）

```bash
cat /etc/redhat-release
ldd --version | head -1
uname -r
echo "TERM=$TERM"
```

| 项 | 值 |
|----|-----|
| 版本 | |
| glibc | |
| 内核 | |
| TERM | |
| SSH 客户端 | |

---

## 1. 安装静态包

在**构建机**（非 6.9）：

```bash
scripts/package_go_agent_client.sh
# 将 dist/dagents-agent-client-linux-amd64-*.tar.gz 拷到 6.9
```

在 **6.9**：

```bash
tar -xzf dagents-agent-client-linux-amd64-*.tar.gz
cd dagents-agent-client-linux-amd64
cp config.example.yaml config.yaml
# 编辑 config.yaml：llm、agent_id、listen
```

| 检查 | 通过 |
|------|------|
| `file bin/dagents-node` 可执行 | ☐ |
| `file bin/dagents-client` 可执行 | ☐ |
| 无 `GLIBC_x.xx not found` | ☐ |

---

## 2. Node 探活

```bash
./bin/dagents-node -config config.yaml &
sleep 2
curl -s http://127.0.0.1:18765/health
./bin/dagents-client -config config.yaml probe
```

| 检查 | 通过 |
|------|------|
| `/health` 200 | ☐ |
| `probe` 输出 `ok` | ☐ |

---

## 3. SysV 服务（可选）

```bash
sudo /path/to/install_node_service_sysv.sh install \
  --config /path/config.yaml --binary /path/bin/dagents-node
sudo /etc/init.d/dagents-node status
```

| 检查 | 通过 |
|------|------|
| `start` 后 probe 仍 ok | ☐ |
|  reboot 后自动启动（若测） | ☐ |

---

## 4. Client 交互

### 4a 全屏 TUI（默认）

```bash
./bin/dagents-client -config config.yaml tui
```

| 检查 | 通过 |
|------|------|
| 上下分区正常（输出不顶输入） | ☐ |
| 发消息收 SSE 流式回复 | ☐ |
| `/context` 可打开，Esc 返回 | ☐ |
| `/skill` 列表可显示 | ☐ |
| 工具审批：↑/↓ Space Enter | ☐ |
| 询问（有选项）：↑/↓ Space Enter | ☐ |
| Esc 取消 turn | ☐ |

### 4b 行模式兜底

```bash
./bin/dagents-client -config config.yaml tui --plain
# 或 export DAGENTS_TUI=plain
```

| 检查 | 通过 |
|------|------|
| `dagents>` 可对话 | ☐ |
| HITL y/n 可续跑 | ☐ |

---

## 5. 无 Client 能力

| 检查 | 通过 |
|------|------|
| trigger schedule 到点 fire（无 Client） | ☐ |
| 重启 Node 后 session 历史仍在 | ☐ |

---

## 6. 失败时收集

```bash
tail -100 /var/log/dagents-node.log   # SysV 安装时
# 或前台运行 Node 时的 stderr
```

将 **glibc 报错全文**、**TERM**、**全屏/ plain 表现** 记入 issue 或 `go-node-compatibility.md` 验收表。

---

## 7. 相关命令速查

```bash
# 停止 Node
kill $(cat /var/run/dagents-node.pid)   # SysV
# 或 fg 后 Ctrl+C

# 恢复 session
./bin/dagents-client -config config.yaml tui SESSION_ID
```
