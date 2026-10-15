# Shared SVG icons

`shared/icons/` is the source for production icon geometry used by Desktop, Node Web UI, Manage Console, and the design gallery. Keep semantic names explicit: `agent`, `workgroup`, `auto`, `skills`, `browser`, `folder-open`, `file`, `attachment`, `tools`, and `send` refer to distinct product concepts. Older technical names such as `bot`, `users`, `sparkles`, and `globe` remain available where their original meanings still apply.

## Visual language

- Use a 24-unit logical canvas where practical. Standard outline icons use roughly 1.8–2px strokes with round caps and joins; filled supplied artwork remains a filled silhouette. These are visual guides, not rules for rewriting approved artwork.
- Check the visible drawing at 16, 18, and 20 CSS pixels. Align by optical center and occupied area, allowing small stars and filled silhouettes to keep their own stroke behavior.
- Use neutral `currentColor` for ordinary controls. Hover and selected states may darken the neutral; disabled controls lower contrast. Reserve distinct colors for meaningful status icons.
- Keep the approved user silhouette for agents, the separately approved file outline for files, the approved team outline for workgroups, and the four-point star for Auto. Skills use the Lucide puzzle outline so they remain distinct from Auto.
- Browser resources use a simple window outline; the former globe is retained as a historical gallery variant. Workspaces use `folder-open`, individual files use `file`, and message attachments use `attachment`.

## Production files and history

- `desktop/` contains the approved Desktop assets and production semantic manifest. `desktop/variants/` retains earlier gallery geometry for reference; it is not a parallel runtime catalog.
- `ui/` contains shared runtime registries for Node and Manage. Thin Vue wrappers preserve their component APIs and inherit semantic SVG viewBox and drawing attributes when reusing supplied artwork.
- `lucide/` records upstream source and ISC licensing. `dagents/` records DAgents semantic artwork and provenance. `legacy-prototype/` preserves retired prototype files.
- `aliases.json` maps old paths to canonical sources. Add a distinct variant only when the artwork itself differs; do not copy a file to satisfy a legacy name.

Manifest, aliases, wrappers, production call sites, and this gallery should agree on the same canonical geometry. The [Desktop gallery](../../docs/design/desktop-product/icons/gallery.html) previews neutral default, hover, selected, and disabled states at 16/18/20px. It references local SVGs over HTTP and preserves its local-server usage notes.

## Desktop top-left slot

The fixed 30×32 top-left slot is owned by `desktop/ui/components/TopLeftSlotIcon.vue`. It provides the panel open/close control on chat and resource pages, a noninteractive page marker on settings, and page identity glyphs for terminal, workspace, browser, workgroups, and the Auto overview dialog. `desktop/ui/top-slot-state.js` owns the accessible names and interaction state. Keep the slot mounted while context changes so the animation can continue without layout movement.

Morphicons 1.7.1 (`morphicons/vue` and `morphicons/adapters`, MIT) morphs only the stroke-based panel/settings/terminal/browser/folder icons. The panel divider stays on the left throughout its 6.5↔9 unit range: the narrow left rail is the expanded-sidebar action, and the wider left rail is the collapsed-sidebar action. The filled workgroup silhouette and transformed Auto star remain the approved shared SVG masks and cross-fade briefly. Do not add Auto routes or animate those assets through the stroke adapter. The MorphIcon honors the OS reduced-motion preference (`reducedMotion="user"`); its instance is mounted once and released with the component.
