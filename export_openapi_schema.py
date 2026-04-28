"""
导出后端 OpenAPI schema，供前端作为契约单一来源使用。

用法（在 DAgents 根目录）:
  python export_openapi_schema.py
  python export_openapi_schema.py --output /path/to/frontend-repo/openapi.json
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path


def parse_args() -> argparse.Namespace:
    """解析命令行参数。

    逻辑：
    1. 定义输出文件路径参数；
    2. 提供默认输出到仓库根 `openapi.json`；
    3. 返回参数供导出逻辑使用。

    关键分支/边界：
    - 输出路径允许传入相对/绝对路径；
    - 若父目录不存在会在后续步骤自动创建。

    与外部交互：
    - 读取命令行参数。

    异常说明：
    - 参数非法时由 argparse 自动报错并退出。

    副作用说明：
    - 无。
    """

    parser = argparse.ArgumentParser(description="Export OpenAPI schema for frontend")
    parser.add_argument(
        "--output",
        default="openapi.json",
        help="导出文件路径（默认 openapi.json）",
    )
    return parser.parse_args()


def export_schema(output_path: Path) -> None:
    """导出 FastAPI OpenAPI 文档到指定文件。

    逻辑：
    1. 导入后端 FastAPI 应用对象；
    2. 调用 `app.openapi()` 生成 schema；
    3. 创建输出目录并写入 JSON 文件。

    关键分支/边界：
    - 导出流程不依赖启动 uvicorn，不触发长期服务；
    - 写文件使用 UTF-8 与 `ensure_ascii=False`，保留中文描述可读性。

    与外部交互：
    - 读取后端代码模块；
    - 写入本地文件系统。

    异常说明：
    - 导入失败或写文件失败会向上抛出，由主流程统一处理并返回非 0。

    副作用说明：
    - 会覆盖目标输出文件内容。
    """

    root = Path(__file__).resolve().parent
    sys.path.insert(0, str(root))
    from app.harness.api.app import app  # noqa: WPS433, E402

    schema = app.openapi()
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(
        json.dumps(schema, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )


def main() -> int:
    """程序入口：执行 schema 导出并输出结果。

    逻辑：
    1. 解析参数并计算绝对输出路径；
    2. 调用导出函数；
    3. 打印导出结果路径。

    关键分支/边界：
    - 相对路径会被解析到仓库根目录下；
    - 失败时返回退出码 1，便于 CI/脚本判断。

    与外部交互：
    - 写本地 schema 文件。

    异常说明：
    - 捕获异常后仅打印摘要，不吞细节（repr）。

    副作用说明：
    - 生成/更新 OpenAPI JSON 文件。
    """

    args = parse_args()
    root = Path(__file__).resolve().parent
    output = Path(args.output)
    if output.is_absolute():
        final_output = output
    else:
        final_output = root / output

    try:
        export_schema(final_output)
    except Exception as exc:
        print(f"[openapi-export] failed: {exc!r}")
        return 1
    else:
        print(f"[openapi-export] exported to: {final_output}")
        return 0


if __name__ == "__main__":
    raise SystemExit(main())

