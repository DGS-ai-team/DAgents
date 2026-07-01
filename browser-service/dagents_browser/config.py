from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml

from dagents_browser.llm import LLMSettings, create_extraction_llm, llm_settings_from_config

DEFAULT_LISTEN_PORT = 18766
DEFAULT_SERVICE_URL = f"http://127.0.0.1:{DEFAULT_LISTEN_PORT}"


@dataclass
class BrowserServiceSettings:
    fs_root: str
    headed: bool = True
    chrome_path: str = ""
    cdp_url: str = ""
    debug_port: int = 9222
    ignore_https_errors: bool = False
    default_timeout_ms: int = 30000
    output_dir: str = "browser"
    max_sessions: int = 1
    llm: LLMSettings | None = None


def load_settings(config_path: str | None) -> BrowserServiceSettings:
    path = config_path or os.environ.get("DAGENTS_CONFIG", "")
    if not path:
        raise ValueError("config path required (-config or DAGENTS_CONFIG)")
    raw = yaml.safe_load(Path(path).read_text(encoding="utf-8")) or {}
    browser = raw.get("browser") or {}
    fs_root = str(raw.get("fs_root") or "").strip()
    if not fs_root:
        raise ValueError("fs_root is required in config")
    headed = browser.get("headed")
    if headed is None:
        headed = True
    return BrowserServiceSettings(
        fs_root=os.path.abspath(fs_root),
        headed=bool(headed),
        chrome_path=str(browser.get("chrome_path") or "").strip(),
        cdp_url=str(browser.get("cdp_url") or "").strip(),
        debug_port=int(browser.get("debug_port") or 9222),
        ignore_https_errors=bool(browser.get("ignore_https_errors") or False),
        default_timeout_ms=int(browser.get("default_timeout_ms") or 30000),
        output_dir=str(browser.get("output_dir") or "browser").strip("/") or "browser",
        max_sessions=int(browser.get("max_sessions") or 1),
        llm=llm_settings_from_config(raw),
    )


def parse_listen(listen: str | None, default_port: int = DEFAULT_LISTEN_PORT) -> tuple[str, int]:
    listen = (listen or os.environ.get("DAGENTS_BROWSER_LISTEN") or "").strip()
    if not listen:
        return "127.0.0.1", default_port
    if ":" not in listen:
        if listen.isdigit():
            return "127.0.0.1", int(listen)
        return listen, default_port
    host, _, port_s = listen.rpartition(":")
    host = host or "127.0.0.1"
    return host, int(port_s)
