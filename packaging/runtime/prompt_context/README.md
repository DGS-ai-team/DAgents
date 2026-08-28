# `packaging/runtime/prompt_context/`

随 `packaging/runtime/` 并入本地助手包的 `bundle/.runtime/prompt_context/`。仓库内 `soul.md` / `custom.md` 为空占位（无预设文案）；已有安装中的旧 `user.md` 只在首次建 Agent 时作为迁移来源，不会注入模型上下文。运行时正文以 `agents.db` 为权威，用户称呼使用 Node 的 `PreferredName`。新安装包不再创建 `user.md`。
