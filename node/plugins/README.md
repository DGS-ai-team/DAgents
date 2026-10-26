# node/plugins

Go `-buildmode=plugin` 的 Skill / 全局 Hook 扩展源码。插件需在 **node 模块内**构建，以便合法引用 `node/internal/hooks`；skill 级插件放在对应运行时 `skills/<name>/hooks/*.so`，全局插件按 Node 的插件加载配置分发。

插件需导出 `Register` 并按 Hook Registry 契约注册扩展。Node 保留通用的加载、卸载与 Hook 生命周期机制。
