# Prototype Instructions

Run the local server yourself and open the preview in the browser available to this environment. Do not give the user server-start instructions when you can run it.

Before making substantial visual changes, use the Product Design plugin's `get-context` skill when the visual source is unclear or no longer matches the current goal. When the user gives durable prototype-specific design feedback, preferences, or decisions, record them in `AGENTS.md`.

When implementing from a selected generated mock, treat that image as the source of truth for layout, component anatomy, density, spacing, color, typography, visible content, and hierarchy.

## Durable design decisions

- Keep the desktop menu on one row; omit search and the Node quick switch.
- Keep a fixed “对话” tab beside closable resource tabs for terminals, browsers, and files.
- Do not add assistant avatars. Keep message input addressed to the active Agent.
- In resource views, retain the compact approval, message/tool activity, and message composer area; terminal commands stay in the terminal itself.
- Keep conversation history and approval state isolated by Agent. Switching Agents opens that Agent's conversation and clears shared resource tabs.
- Browser navigation is simulated locally; label its result as demo content and never imply a real network request.

Build app UI in `src/`. Keep `.openai/hosting.json`, `worker/index.js`, `scripts/prepare-sites-build.mjs`, and `tests/sites-worker.test.mjs` intact so the same local prototype can be handed to Sites. Before a Sites handoff, run `npm run build` and `npm run test:sites`; the build must leave `dist/client/index.html`, `dist/server/index.js`, and `dist/.openai/hosting.json`.
