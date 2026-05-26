from __future__ import annotations

from rich.panel import Panel
from rich.table import Table
from rich.text import Text

from app.cli.version_info import get_cli_username, get_cli_version

# 进入 TUI 时右侧固定风险提示（中文）。
_RISK_LINES = (
    "· Agent 可能调用工具读写本机文件或执行命令，涉及审批时请仔细确认。",
    "· 请勿在对话中粘贴密码、密钥等敏感信息。",
    "· 对话与工具结果会发送至已配置的后端 API，请注意数据合规。",
)


def build_welcome_panel(
    *,
    api_base: str,
    session_id: str,
    username: str | None = None,
    version: str | None = None,
) -> Panel:
    """构造进入聊天时写入 RichLog 的欢迎 Panel（连接成功后一次性写入，不再更新）。

    逻辑：
    1. 左栏：欢迎语、用户名、backend、session；
    2. 右栏：风险提示列表；
    3. 外层 Panel 标题为版本号，边框使用红色系以贴近原 TUI 卡片样式。

    Args:
        api_base: 后端根地址。
        session_id: 当前 session id（空则展示 —）。
        username: 系统用户名，默认 `get_cli_username()`。
        version: CLI 版本，默认 `get_cli_version()`。

    Returns:
        可直接 `RichLog.write(panel)` 的 Rich Panel。
    """
    user = username if username is not None else get_cli_username()
    ver = version if version is not None else get_cli_version()
    backend = api_base.strip() or "—"
    session = session_id.strip() or "—"

    left = Text()
    left.append(f"欢迎回来 {user}！\n", style="bold")
    left.append(f"用户 · {user}\n", style="dim")
    left.append(f"backend · {backend}\n", style="dim")
    left.append(f"session · {session}", style="dim")

    right = Text("风险提示\n", style="bold red")
    for line in _RISK_LINES:
        right.append(f"{line}\n", style="dim")

    grid = Table.grid(expand=True, padding=(0, 2))
    grid.add_column(ratio=1)
    grid.add_column(ratio=1)
    grid.add_row(left, right)

    return Panel(
        grid,
        title=f"[bold]DAgents v{ver}[/bold]",
        border_style="red",
        padding=(1, 2),
    )
