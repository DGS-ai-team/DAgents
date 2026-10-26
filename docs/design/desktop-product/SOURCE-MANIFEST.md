# 资料来源与同步边界

本目录是便于阅读和交接的 **2026-09-24 设计快照**。下列原始文件保留在原路径，以保证项目现有引用和正在运行的原型不受影响。后续定稿应编辑原始共享资产或确定新的 Desktop 独立资产，再更新本包；不要同时在两处独立修改。

| 包内位置 | 原始位置 | 备注 |
| --- | --- | --- |
| `architecture/desktop-node-separation-plan.md` | `docs/architecture/desktop-node-separation-plan.md` | 包内副本只调整了相对链接，并标注旧版 UI 章节 |
| `architecture/assets/desktop-workspace-proposal.svg` | `docs/architecture/assets/desktop-workspace-proposal.svg` | 早期三栏布局，不是当前稿 |
| `visuals/current-desktop-mockup.png` | `C:/Users/Administrator/.codex/generated_images/01a0cc32-429f-7951-9c02-d7b01dd20615/exec-fd30cefa-3bd8-4ce8-affb-8824dbe2e674.png` | 当前界面图 |
| `visuals/exploration/*.png` | 同一 `generated_images` 目录中的其余 12 张 PNG | 过程稿，保持原文件名 |
| `prototype/` | 初始来源为 `docs/design/desktop-interactive-prototype/` | 排除构建产物与依赖目录；现已按 v3 改造，是 Desktop 原型的当前版本，旧目录保留作历史对照 |
| `icons/` | `shared/icons/desktop/` canonical assets | Desktop gallery and acceptance entry points; canonical manifest stores provenance/license notes and prototype imports shared files through `@shared-icons` without copied SVGs |
| `visual-system/brand-icon.png` | `shared/branding/brand-icon.png` | 现有品牌 PNG，不另造雪花 SVG |
| `visual-system/dagents-color-system.*` | `docs/design/dagents-color-system.*` | Node / Manage 共享色彩草案 |
| `visual-system/dagents-icon-system.*` | `docs/design/dagents-icon-system.*` | 功能图标说明及画板 |
| `visual-system/dagents-icon-samples.*` | `docs/design/dagents-icon-samples.*` | 早期样稿 |
| `visual-system/dაგენტs-icon-gallery.html` | `docs/design/dაგენტs-icon-gallery.html` | Gallery reads canonical `shared/icons/` paths; source and license history remains in `docs/design/dაგენტs-icons/README.md` |
| `visual-system/dagents-ui-audit-and-direction.html` | `docs/design/dagents-ui-audit-and-direction.html` | Node / Manage 视觉审计及统一方向 |
| `references/ui-product-grade-redesign.md` | `docs/design/ui-product-grade-redesign.md` | 现有 Node UI 背景资料 |

现有 Tauri 源码、Node API 文档及归档的旧版 Windows Shell 设计没有复制到本包；Go Shell 已从当前树退役，旧设计记录仍保留作历史参考。Canva 尝试未产生可归集的本地设计文件。
