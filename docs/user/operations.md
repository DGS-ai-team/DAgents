# 运维与发布

## 安装与服务

发布包位于 `packaging/agent-client/`；Windows 与 Linux 安装脚本、桌面 Shell 和离线安装说明分别见：

- [agent-client packaging](../../packaging/agent-client/README.md)
- [offline install](../../packaging/OFFLINE_INSTALL.md)
- [Manage packaging](../../packaging/manage/README.md)

Node 默认监听 `127.0.0.1:18765`。需要跨机器访问时应使用防火墙白名单、反向代理或 SSH 隧道，不要未经加固直接暴露公网。

## 诊断顺序

1. `GET /health` 是否成功；
2. Web UI 是否能建立 SSE；
3. Agent 是否有有效 LLM profile；
4. 工具组和 policy 是否允许目标工具；
5. 若是工作组，检查 Node 是否已注册、WS 是否在线、ACL/订阅和成员 Agent 是否 ready；
6. 若是终端，检查目标类型、连接状态、权限、终端 WebSocket 和命令退出状态。

## 发布门禁

```bash
npm test --prefix node/webui/frontend -- --run
npm run build --prefix node/webui/frontend
npm run build --prefix manage/console/frontend
go test ./node/... ./client/... ./shared/config/...
python3 -m unittest discover -s tests -p "test_*.py" -v
git diff --check
```

发布说明、版本号和资产以根目录 `CHANGELOG.md`、`.github/workflows/` 及 [发布流程](../release-process.md) 为准。
