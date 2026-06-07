# 安全策略（Security Policy）

本文件说明 **DAgents** 仓库的安全漏洞报告方式与当前支持范围。若与组织级策略冲突，以组织策略为准。

---

## 支持版本

| 版本 | 支持状态 |
|------|----------|
| **v0.2.x**（当前 **0.x** 预览线） | 接受安全报告；修复以补丁小版本或次版本发布。 |
| **v0.1.x** | 仅接受严重漏洞报告；建议升级到 **v0.2.2** 或最新 **`dev`** / **`main`**。 |
| **低于 v0.1.0** 的任意提交 | 不单独维护。 |

---

## 如何报告漏洞

**请勿**在公开 Issue、讨论区或 PR 中张贴 **可利用的** 漏洞细节（PoC、完整利用链、未修复的远程执行路径等），以免在修复发布前扩大暴露面。

请任选其一（按你可用的渠道优先）：

1. **GitHub Security Advisories（推荐）**  
   在本仓库页面打开 **「Security」** → **「Report a vulnerability」**（或 **「Advisories」** 下提交私有报告）。若仓库未开启该功能，请联系仓库管理员在 **Settings → Security** 中启用 **Private vulnerability reporting**。

2. **定向联系维护者**  
   若你的组织使用内部安全邮箱或工单系统，请将报告发往维护团队约定的 **私密** 渠道，并在主题中注明 **`[SECURITY] DAgents`**。

报告建议包含（在不泄露利用细节的前提下尽量完整）：

- 受影响版本或 **commit**（或 **tag**，如 **`v0.2.2`**）
- 组件范围（例如 **Agent API**、**Register Center**、依赖库等）
- 影响类型（机密性 / 完整性 / 可用性）与大致攻击面（需认证与否、默认配置是否暴露等）
- 复现环境（OS、Python 版本、是否 Docker / PyInstaller 包等）
- 若已有修复思路或补丁，可附 **私有** 附件或后续在 Advisory 中沟通

维护者一般会在 **7 个工作日内** 确认收悉；复杂问题可能需要更长时间，会在 Advisory 或邮件中同步进度。

---

## 范围说明

**在范围内（欢迎报告）**

- 本仓库 **`app/`**、**`register_center/`**、**`run_*.py`** 及默认安装路径下与 **Agent API / Register Center** 直接相关的安全问题。
- 由错误使用导致的 **敏感信息泄露**（例如误将 **`.env`**、密钥提交到公开 fork）——仍建议通过私密渠道告知，以便协助撤销与公告。

**通常不在范围内（或需单独约定）**

- 第三方依赖的漏洞：请同时查阅 **CVE** 与上游修复版本，本仓库通过 **`requirements.txt`** 升级吸纳；若需本仓库侧协调发版节奏，可通过 Advisory 说明。
- **故意**在不安全网络环境下暴露 **无鉴权** 的默认 **`127.0.0.1`** 以外的监听而未做反向代理 / 防火墙隔离——属于部署与运维责任；文档已提示默认 **无 HTTP 鉴权**。
- 对 **Register Center**、**LLM 供应商** 等外部系统的拒绝服务，除非能证明由本仓库默认配置**必然**导致且可合理修复。

---

## 披露与致谢

- 修复就绪后，维护者倾向于通过 **GitHub Security Advisory** 或 **Release 说明** / **CHANGELOG** 公开摘要（CVE 编号以实际申请为准）。
- 若报告者希望署名致谢，请在报告中说明偏好（匿名 / 署名 / 链接）。

---

## 相关文档

- 根目录 **[README.md](README.md)**：配置与安全、**`SECURITY.md`** 引用  
- **[docs/architecture/agent-node-api.md](docs/architecture/agent-node-api.md)**：Go Node HTTP / SSE 契约（部署时需结合网络与鉴权策略）
- **[docs/prometheus-metrics.md](docs/prometheus-metrics.md)**：**`/metrics`** 与 Prometheus 落地说明
