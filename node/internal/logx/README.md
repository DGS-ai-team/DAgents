# logx

Node 进程结构化日志辅助。

| 文件 | 说明 |
|------|------|
| `logx.go` | `ParseLevel`、`NewSplitLogger`、`OrDefault`、`Discard` |

`NewSplitLogger(full, err, level)`：完整日志写 `full`（通常 stdout → `*-DATE.log`），`error` 及以上额外写 `err`（通常 stderr → `*-DATE.err.log`）。
