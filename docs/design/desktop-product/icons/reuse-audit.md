# Desktop prototype icon reuse audit

As of 2026-10-09, DAgents and Lucide SVGs are maintained canonically under [`shared/icons/`](../../../../shared/icons/). Earlier visual-system copies were removed after path and content verification; this audit retains its source comparison as historical provenance.

## Reuse in the prototype

Production uses the canonical Desktop assets declared in `shared/icons/desktop/manifest.json`. Browser resources use a Lucide window outline; the former globe remains a historical gallery variant. Workspaces use `folder-open`, individual files use the supplied file silhouette, and the composer uses the supplied attachment silhouette. `upload` remains cataloged for future upload semantics.

`send.svg` is the simple upward arrow used by both composers. `play.svg` is reserved for terminal command execution. Node and Manage registries expose semantic aliases for agent, workgroup, Auto, skills, browser, attachment, file, tools, and send; legacy names remain for unrelated technical meanings.

The supplied `user.svg`, `wrench.svg`, `team.svg`, and file artwork remains authoritative. Auto keeps its four-point star, optically scaled by 1.08 around the 24-unit center without changing its outline. Skills use Lucide Puzzle, distinct from Auto. Standard neutral states inherit `currentColor`; disabled controls reduce contrast, while status icons may use status colors.

## Reviewed 2026-09-29 download candidates

| Candidate | Decision | Reason |
| --- | --- | --- |
| `folder.svg` | Not used | The existing `folder-open.svg` better represents the opened resource collection. |
| `earth.svg` | Not used | The browser window icon communicates the browser resource tab more directly. |
| `setting.svg` | Not used | Settings has no icon slot; the top-level menus intentionally remain text labels. |
| `link.svg`, `disconnect.svg` | Not used | Node connectivity is shown by its status dot and label; there is no separate icon control. |
| `api.svg` | Not used | The prototype has no API-specific menu or action requiring this icon. |
| `file.svg`, `message.svg`, `attachment.svg` | Used | They serve the generic file resource, return-to-conversation action, and add-attachment action. |
| `image.svg`, `edit.svg` | Reserved | Included in the gallery and manifest, without adding controls that have no current action. |

The gallery uses local SVG assets and its existing HTTP guidance. It previews 16/18/20px glyphs in default, hover, selected, and disabled neutral states.
