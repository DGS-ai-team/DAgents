# `scripts/service/`

systemd unit 模板，供 **`linux/install_node_service.sh`** 使用。

| 文件 | 说明 |
|------|------|
| `dagents-node.service.template` | 占位符：`@BINARY@` / `@CONFIG@` / `@WORKING_DIR@` / `@ENV_FILE_LINE@` |

安装入口见 [`../linux/install_node_service.sh`](../linux/install_node_service.sh) 与 [`../README.md`](../README.md)。
