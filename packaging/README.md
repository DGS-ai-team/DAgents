# `packaging/`

分发辅助说明（非运行时模块），可随内网拷贝包附带给用户。

| 路径 | 说明 |
|------|------|
| **`OFFLINE_INSTALL.md`** | 离线安装步骤摘要；亦可复制到 README「离线依赖」章节 |
| **`runtime/`** | 预编译包内 **`.runtime/`** 整树占位：**`prompt_context/`**（空 **`soul.md`/`user.md`/`custom.md`**）、**`scripts/`**、**`data/`**、**`skills/`**、**`history/`**、**`memory/`**、**`agent/`** 等；打 zip 时并入 **`bundle/.runtime/`**；侧车无预设文案，运行时仍可由 **`prompt.py`** 补建缺失文件 |
