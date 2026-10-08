# Desktop prototype icon reuse audit

`node/webui/frontend` does not have a standalone production SVG icon directory. Its production icons are inline SVG geometry in [`UiIcon.vue`](../../../../node/webui/frontend/src/components/UiIcon.vue). The standalone DAgents icon library is [`docs/design/dagents-icons`](../../dagents-icons/README.md) (also copied into `visual-system/dagents-icons`); the 60 corresponding SVG files have matching hashes.

## Reuse in the prototype

These existing DAgents assets are copied into the Desktop source library unchanged: `browser`, `terminal`, `pending`, `success`, `error`, `create`, `more`, and `upload`. Other same-name DAgents source icons also match where applicable; `folder-open` remains Lucide because its open-folder outline better describes the existing resource collection entry than the closed-folder asset. `upload` remains cataloged for future actual upload semantics and is not used for the current attachment affordance.

`send.svg` uses the `send` path geometry from the inline Node Web UI `UiIcon.vue`, extracted into a local `currentColor` SVG. Both message composers use it. `play.svg` is reserved for terminal command execution. The production Vue component remains the source reference; there is no Web UI SVG asset to copy for this symbol.

The user-provided `user.svg`, `robot.svg`, `wrench.svg`, and `team.svg` artwork remains authoritative for agent, Auto, tools, and workgroup assets. Normal agent entries consistently use the agent icon; the wrench is reserved for tool semantics.

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

The gallery is a local HTML file and loads only the manifest's local SVG assets, so it remains inspectable without a network connection.
