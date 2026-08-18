# `tests/` REFERENCE

当前测试索引以实际存在的 `test_*.py` 和各 Go 包中的 `*_test.go` 为准；规划与运行命令见 [`UNIT_TEST_CHECKLIST.md`](UNIT_TEST_CHECKLIST.md)。

## Python

- `test_smoke.py`：验证工作区基础模块可导入。
- `test_manage_*.py`：Manage Registry、A2A、Skills、LLM、Release、Cases、Admin 与 Workgroup。
- `test_workgroup_*.py`：Workgroup D05 索引和 store golden 用例。
- `integration/`：可选的真实 LLM 冒烟，不属于默认 discovery；详见 [`integration/README.md`](integration/README.md)。
- `test_support/`：测试替身与公共辅助代码；详见 [`test_support/REFERENCE.md`](test_support/REFERENCE.md)。

## Go

- `node/`：Node API、turn、session、queue、store、stream、tools、policy、skills、triggers 等运行时测试。
- `client/internal/api/`：Node HTTP 客户端。
- `client/internal/desktop/`：桌面更新辅助。
- `client/internal/probe/`：健康探测。
- `client/internal/update/`：Release Hub 更新。

`client/cmd/` 目前是 probe/update/version 运维入口，不包含对话界面测试。
