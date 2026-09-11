# Vendored Lucide SVGs

This directory contains the direct Lucide candidates used by the offline DAgents icon gallery. The files are copied from the official Lucide repository and are kept separate from the DAgents semantic SVGs one level above.

- Source repository: https://github.com/lucide-icons/lucide
- Source directory: https://github.com/lucide-icons/lucide/tree/main/icons
- Source revision: official `main` branch (moving source; no commit is pinned for this design sample)
- Fetched: 2026-09-11
- License: ISC License, as published by Lucide. See the upstream license at https://github.com/lucide-icons/lucide/blob/main/LICENSE

The upstream files retain Lucide's 24×24 viewBox and `fill="none"`, `stroke="currentColor"`, `stroke-width="2"`, round cap and round join defaults. DAgents does not edit or recolor these vendor files. Themes are applied by the consuming SVG/CSS context.

## Included files

`alarm-clock.svg`, `arrow-left-right.svg`, `book-open.svg`, `bot.svg`, `brain.svg`, `chevron-down.svg`, `chevron-up.svg`, `circle-check.svg`, `circle-plus.svg`, `circle-x.svg`, `clipboard.svg`, `cpu.svg`, `ellipsis.svg`, `file.svg`, `folder-open.svg`, `folder-tree.svg`, `id-card.svg`, `inbox.svg`, `info.svg`, `key-round.svg`, `layout-dashboard.svg`, `list-checks.svg`, `loader-circle.svg`, `megaphone.svg`, `menu.svg`, `message-circle.svg`, `moon-star.svg`, `network.svg`, `panel-top.svg`, `pencil.svg`, `play.svg`, `plug.svg`, `refresh-cw.svg`, `receipt-text.svg`, `save.svg`, `scroll-text.svg`, `search.svg`, `server.svg`, `settings.svg`, `sliders-horizontal.svg`, `sparkles.svg`, `square.svg`, `square-terminal.svg`, `terminal.svg`, `trash.svg`, `triangle-alert.svg`, `upload.svg`, `user-plus.svg`, `users-round.svg`, `wrench.svg`

The `delete` semantic uses the official `trash.svg` because `trash-2.svg` is not present in the current official `main/icons` directory. The gallery and mapping document this replacement rather than creating a local substitute under the old name.
