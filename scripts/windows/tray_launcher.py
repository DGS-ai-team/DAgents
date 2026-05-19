"""Windows 系统托盘启动器：在通知区管理 Agent API / Register Center 子进程。

用法（在 DAgents 仓库根目录）:
  python scripts/windows/tray_launcher.py
  python scripts/windows/tray_launcher.py --with-register-center

依赖:
  pip install -r requirements.txt -r requirements-windows-tray.txt
"""

from __future__ import annotations

import argparse
import logging
import os
import subprocess
import sys
import threading
import webbrowser
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable

# 仅 Windows 提供通知区；其它平台直接退出，避免误用。
if sys.platform != "win32":
    raise SystemExit("scripts/windows/tray_launcher.py 仅支持 Windows（win32）。")

try:
    import pystray
    from PIL import Image, ImageDraw
except ImportError as exc:
    raise SystemExit(
        "缺少托盘依赖，请执行: pip install -r requirements.txt -r requirements-windows-tray.txt"
    ) from exc

_LOGGER = logging.getLogger(__name__)

# CREATE_NO_WINDOW：子进程不弹出控制台窗口。
_CREATE_NO_WINDOW = getattr(subprocess, "CREATE_NO_WINDOW", 0x08000000)


def _repo_root() -> Path:
    """返回仓库根目录（`scripts/windows/` 上两级）。"""
    return Path(__file__).resolve().parents[2]


def _resolve_python_executable() -> Path:
    """解析用于拉起 API/RC 的 Python 解释器路径。"""
    return Path(sys.executable).resolve()


def _browser_host(bind_host: str) -> str:
    """将监听地址映射为浏览器可访问的 host（`0.0.0.0` → `127.0.0.1`）。"""
    normalized = bind_host.strip().lower()
    if normalized in ("0.0.0.0", "::", "[::]", ""):
        return "127.0.0.1"
    return bind_host.strip() or "127.0.0.1"


def _load_service_urls(root: Path) -> tuple[str, str]:
    """加载 `.env` 后推导 API 与 Register Center 的本地访问 URL。

    逻辑：
    1. 将仓库根加入 `sys.path` 并 `load_env`；
    2. 读取 `Settings` 的 `api_host`/`api_port`；
    3. Register Center 端口/host 从环境变量读取（与 `run_register_center.py` 一致）。

    异常说明：
    - 配置模块导入失败时向上抛出。
    """
    if str(root) not in sys.path:
        sys.path.insert(0, str(root))
    from app.config.env import load_env
    from app.config.settings import get_settings

    load_env(root)
    settings = get_settings(reload=True)
    api_host = _browser_host(settings.api_host)
    api_url = f"http://{api_host}:{settings.api_port}"

    rc_host = (os.environ.get("REGISTER_CENTER_HOST", "0.0.0.0") or "0.0.0.0").strip()
    raw_rc_port = (os.environ.get("REGISTER_CENTER_PORT", "8010") or "8010").strip()
    try:
        rc_port = int(raw_rc_port)
        if not (1 <= rc_port <= 65535):
            rc_port = 8010
    except ValueError:
        rc_port = 8010
    rc_url = f"http://{_browser_host(rc_host)}:{rc_port}"
    return api_url, rc_url


def _create_tray_image(size: int = 64) -> Image.Image:
    """生成简易托盘图标（蓝底白字 D），避免仓库内嵌二进制 .ico。"""
    image = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(image)
    margin = size // 8
    draw.rounded_rectangle(
        (margin, margin, size - margin, size - margin),
        radius=size // 6,
        fill=(37, 99, 235, 255),
    )
    draw.text((size * 0.32, size * 0.22), "D", fill=(255, 255, 255, 255))
    return image


@dataclass
class ManagedService:
    """受管子进程（名称、命令、工作目录、可选日志文件）。"""

    name: str
    command: list[str]
    cwd: Path
    log_path: Path | None = None
    popen: subprocess.Popen[str] | None = field(default=None, init=False)
    _log_handle: object | None = field(default=None, init=False, repr=False)

    def is_running(self) -> bool:
        """子进程是否存在且未退出。"""
        return self.popen is not None and self.popen.poll() is None

    def start(self) -> None:
        """启动子进程；已运行则直接返回。

        关键分支：
        - 日志路径存在时重定向 stdout/stderr 到追加日志文件；
        - Windows 下附加 `CREATE_NO_WINDOW`。

        异常说明：
        - `Popen` 失败向上抛出。
        """
        if self.is_running():
            return
        stdout_target = None
        stderr_target = None
        if self.log_path is not None:
            self.log_path.parent.mkdir(parents=True, exist_ok=True)
            self._log_handle = self.log_path.open("a", encoding="utf-8")
            stdout_target = self._log_handle
            stderr_target = self._log_handle
        else:
            self._log_handle = None
        try:
            self.popen = subprocess.Popen(
                self.command,
                cwd=str(self.cwd),
                stdout=stdout_target,
                stderr=stderr_target,
                text=True,
                creationflags=_CREATE_NO_WINDOW,
            )
        except Exception:
            self._close_log_handle()
            raise
        _LOGGER.info("[%s] started pid=%s", self.name, self.popen.pid)

    def stop(self, *, wait_seconds: float = 5.0) -> None:
        """终止子进程：先 terminate，超时再 kill。"""
        if not self.is_running() or self.popen is None:
            self.popen = None
            return
        proc = self.popen
        proc.terminate()
        try:
            proc.wait(timeout=wait_seconds)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=wait_seconds)
        _LOGGER.info("[%s] stopped", self.name)
        self.popen = None
        self._close_log_handle()

    def _close_log_handle(self) -> None:
        """关闭日志文件句柄（子进程退出或启动失败时）。"""
        if self._log_handle is None:
            return
        try:
            self._log_handle.close()
        except Exception:
            # 关闭失败不阻断 stop 流程。
            pass
        self._log_handle = None


class TrayController:
    """托盘菜单与子进程生命周期协调。

    副作用：
    - 启动/停止 `ManagedService`；
    - 通过 `pystray` 在通知区展示图标（阻塞线程）。
    """

    def __init__(
        self,
        *,
        root: Path,
        python_exe: Path,
        api_url: str,
        rc_url: str,
        start_register_on_launch: bool,
    ) -> None:
        self._root = root
        self._python = python_exe
        self._api_url = api_url
        self._rc_url = rc_url
        self._logs_dir = root / "logs"
        self._api = ManagedService(
            name="api",
            command=[str(python_exe), "run_agent_api.py"],
            cwd=root,
            log_path=self._logs_dir / "tray-api.log",
        )
        self._register = ManagedService(
            name="register_center",
            command=[str(python_exe), "run_register_center.py"],
            cwd=root,
            log_path=self._logs_dir / "tray-register-center.log",
        )
        self._icon: pystray.Icon | None = None
        self._start_register_on_launch = start_register_on_launch

    def _status_label(self, label: str, running: bool) -> str:
        state = "运行中" if running else "已停止"
        return f"{label}: {state}"

    def _build_menu(self) -> pystray.Menu:
        """构造托盘右键菜单（含动态状态行，不可点击）。"""
        return pystray.Menu(
            pystray.MenuItem(
                lambda item: self._status_label("Agent API", self._api.is_running()),  # type: ignore[arg-type]
                None,
                enabled=False,
            ),
            pystray.MenuItem(
                lambda item: self._status_label(  # type: ignore[arg-type]
                    "Register Center",
                    self._register.is_running(),
                ),
                None,
                enabled=False,
            ),
            pystray.Menu.SEPARATOR,
            pystray.MenuItem("启动 Agent API", self._menu_start_api),
            pystray.MenuItem("停止 Agent API", self._menu_stop_api),
            pystray.MenuItem("启动 Register Center", self._menu_start_register),
            pystray.MenuItem("停止 Register Center", self._menu_stop_register),
            pystray.Menu.SEPARATOR,
            pystray.MenuItem("在浏览器中打开 API", self._menu_open_api),
            pystray.MenuItem("在浏览器中打开 Register Center", self._menu_open_register),
            pystray.MenuItem("打开安装目录", self._menu_open_folder),
            pystray.Menu.SEPARATOR,
            pystray.MenuItem("退出", self._menu_exit),
        )

    def _run_menu_action(self, action: Callable[[], None]) -> None:
        """在后台线程执行菜单动作，避免阻塞 pystray 事件循环。"""
        threading.Thread(target=action, daemon=True).start()

    def _menu_start_api(self, _icon: pystray.Icon, _item: pystray.MenuItem) -> None:
        self._run_menu_action(self._api.start)

    def _menu_stop_api(self, _icon: pystray.Icon, _item: pystray.MenuItem) -> None:
        self._run_menu_action(self._api.stop)

    def _menu_start_register(self, _icon: pystray.Icon, _item: pystray.MenuItem) -> None:
        self._run_menu_action(self._register.start)

    def _menu_stop_register(self, _icon: pystray.Icon, _item: pystray.MenuItem) -> None:
        self._run_menu_action(self._register.stop)

    def _menu_open_api(self, _icon: pystray.Icon, _item: pystray.MenuItem) -> None:
        self._run_menu_action(lambda: webbrowser.open(f"{self._api_url}/docs"))

    def _menu_open_register(self, _icon: pystray.Icon, _item: pystray.MenuItem) -> None:
        self._run_menu_action(lambda: webbrowser.open(self._rc_url))

    def _menu_open_folder(self, _icon: pystray.Icon, _item: pystray.MenuItem) -> None:
        self._run_menu_action(lambda: os.startfile(str(self._root)))  # type: ignore[attr-defined]

    def _menu_exit(self, _icon: pystray.Icon, _item: pystray.MenuItem) -> None:
        def _shutdown() -> None:
            self._api.stop()
            self._register.stop()
            if self._icon is not None:
                self._icon.stop()

        self._run_menu_action(_shutdown)

    def _autostart_services(self) -> None:
        """托盘启动时默认拉起 Agent API；可选同时拉起 Register Center。"""
        self._api.start()
        if self._start_register_on_launch:
            self._register.start()
        else:
            pass

    def run(self) -> None:
        """进入托盘主循环（阻塞直至用户退出）。"""
        self._autostart_services()
        menu = self._build_menu()
        self._icon = pystray.Icon(
            "dagents",
            _create_tray_image(),
            "DAgents",
            menu,
        )
        _LOGGER.info("tray icon running (api=%s)", self._api_url)
        self._icon.run()


def parse_args() -> argparse.Namespace:
    """解析命令行：是否在启动托盘时一并拉起 Register Center。"""
    parser = argparse.ArgumentParser(description="DAgents Windows 托盘启动器")
    parser.add_argument(
        "--with-register-center",
        action="store_true",
        help="托盘启动时同时后台运行 Register Center",
    )
    return parser.parse_args()


def main() -> None:
    """入口：校验平台、加载配置并显示托盘图标。"""
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(message)s")
    args = parse_args()
    root = _repo_root()
    os.chdir(root)
    api_url, rc_url = _load_service_urls(root)
    controller = TrayController(
        root=root,
        python_exe=_resolve_python_executable(),
        api_url=api_url,
        rc_url=rc_url,
        start_register_on_launch=bool(args.with_register_center),
    )
    controller.run()


if __name__ == "__main__":
    main()
