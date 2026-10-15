# Prototype Instructions

Run the local server yourself and open the preview in the browser available to this environment. Do not give the user server-start instructions when you can run it.

Before making substantial visual changes, use the Product Design plugin's `get-context` skill when the visual source is unclear or no longer matches the current goal. When the user gives durable prototype-specific design feedback, preferences, or decisions, record them in `AGENTS.md`.

When implementing from a selected generated mock, treat that image as the source of truth for layout, component anatomy, density, spacing, color, typography, visible content, and hierarchy.

## Durable design decisions

- Keep the desktop menu on one row; omit search and the Node quick switch.
- Keep a fixed conversation entry beside closable resource tabs. Show only its icon and active Agent name; resource tabs show only the resource icon and its actual name, such as a terminal session name, browser page title, or file name.
- Use one-column, mutually exclusive top menus with clear groups, shortcut/status hints, disabled styling for unavailable actions, outside-click dismissal, and Escape dismissal. Only show shortcuts that the prototype intercepts and executes; do not claim browser shortcuts that are not implemented.
- Menu actions operate on the active Agent's prototype state. Label Node and browser activity as simulated, and never execute real operating-system actions.
- Do not add assistant avatars. Keep message input addressed to the active Agent.
- In resource views, retain the compact approval, message/tool activity, and message composer area; pin pending approval actions above the independently scrolling activity history so the decision buttons remain visible at 1024×768. Terminal commands stay in the terminal itself.
- Keep conversation history and approval state isolated by Agent. Switching Agents opens that Agent's conversation and clears shared resource tabs.
- Keep terminal input/output isolated by resource id. Only the seeded `terminal-1` contains the sample build log; newly created terminal sessions start at an empty prompt. Creating a new conversation resets that Agent's approval and generation state and cancels its pending simulated reply.
- Browser navigation is simulated locally; label its result as demo content and never imply a real network request.
- Avoid fake presence timestamps in the Agent list. In collapsed navigation, keep group counts in the object popover rather than as tiny badges on icons.
- Keep resource canvases lightly differentiated by type, and show a concise task-start hint in an empty Auto conversation.

Build app UI in `src/`. Keep `.openai/hosting.json`, `worker/index.js`, `scripts/prepare-sites-build.mjs`, and `tests/sites-worker.test.mjs` intact so the same local prototype can be handed to Sites. Before a Sites handoff, run `npm run build` and `npm run test:sites`; the build must leave `dist/client/index.html`, `dist/server/index.js`, and `dist/.openai/hosting.json`.
