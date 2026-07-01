from __future__ import annotations

import argparse

import uvicorn

from dagents_browser.config import load_settings, parse_listen
from dagents_browser.server import create_app


def main() -> None:
    parser = argparse.ArgumentParser(description="dagents-browser thin service (browser-use + local Chrome)")
    parser.add_argument("--config", default="", help="path to config.yaml (same as dagents-node)")
    parser.add_argument("--listen", default="", help="listen host:port (default 127.0.0.1:18766)")
    args = parser.parse_args()
    settings = load_settings(args.config or None)
    host, port = parse_listen(args.listen)
    app = create_app(settings)
    uvicorn.run(app, host=host, port=port, log_level="info")


if __name__ == "__main__":
    main()
