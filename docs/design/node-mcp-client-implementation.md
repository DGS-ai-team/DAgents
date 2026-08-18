# Node MCP Client implementation

This change implements the Node-side MCP client. MCP remains local to a Node
Agent: the Node owns the connection, credentials, catalog and per-Agent
binding, while Manage never receives an MCP secret.

## Included

- MCP stdio JSON-RPC client with `initialize`, `tools/list`, pagination, and
  `tools/call`.
- MCP Streamable HTTP client for remote servers. It sends JSON-RPC over POST,
  retains `Mcp-Session-Id`, and accepts JSON or SSE responses.
- Persistent server configuration in the runtime `mcp_servers.db` database.
- Service-level `enabled_tools` allowlist. An empty list is fail-closed: the
  server catalog is still loaded for administration, but no remote tool is
  exposed to an Agent until explicitly enabled.
- Environment-variable references such as `${OPENAI_API_KEY}` and literal
  `env`/`headers` values are both supported in the local Node configuration.
  Literal credentials are encrypted at rest in the local MCP database; the
  editor accepts plaintext only while saving the local configuration.
- Tool namespace `mcp__<server_id>__<tool_name>`.
- Per-Agent binding in `config_snapshot.defaults.mcp.bindings`, including a
  server enable flag and a tool allowlist.
- Existing Agent Registry, approval policy, cancellation context, and
  tool-result path are reused. The internal `call_purpose` field is exposed to
  the model/UI but is removed before the MCP request.
- Result size limiting and fail-closed validation for unsafe/duplicate tool
  names.
- Web UI for global server CRUD/test/refresh and per-Agent server/tool binding.

The server settings page keeps the complete `tools/list` catalog so an
operator can inspect names, descriptions and schemas. Each tool can be
enabled or disabled independently. Agent bindings, policy/permission choices
and runtime registration are built from the intersection of service-enabled
tools and the Agent's allowlist; disabled tools are not exposed even if an old
Agent snapshot still mentions them.

## API surface

```text
GET    /v1/mcp/servers
POST   /v1/mcp/servers
PATCH  /v1/mcp/servers/{server_id}
DELETE /v1/mcp/servers/{server_id}
POST   /v1/mcp/servers/{server_id}/test
POST   /v1/mcp/servers/{server_id}/refresh
GET    /v1/mcp/servers/{server_id}/tools
GET    /v1/agents/{agent_id}/mcp
PUT    /v1/agents/{agent_id}/mcp
GET    /v1/agents/{agent_id}/mcp/effective-tools
```

An unavailable MCP process does not prevent the Agent from starting. Its
binding remains persisted, the server is shown as offline/error, and a later
refresh can rebuild the affected Agent registries from the new catalog.

## Tencent Docs example

Tencent Docs exposes the remote MCP endpoint:

```text
https://docs.qq.com/openapi/mcp
```

Configure a Node server with:

```json
{
  "id": "tencent-docs",
  "display_name": "腾讯文档",
  "transport": "streamable_http",
  "url": "https://docs.qq.com/openapi/mcp",
  "header_refs": { "Authorization": "TENCENT_DOC_KEY" },
  "enabled": true
}
```

Set `TENCENT_DOC_KEY` in the environment of the Node process, then use the
Node Web UI's MCP connection test. The test performs `initialize` and
`tools/list`; bind the discovered tools to an Agent in that Agent's MCP panel.
The token is space-scoped and must be obtained from Tencent Docs' MCP token
page. Never put the token directly in `mcp_servers.db`, a checked-in config,
an Agent snapshot, or a log.

## Deferred

- OAuth, resources/prompts/sampling and old standalone SSE transport.
- Workgroup Supervisor/Member MCP forwarding and Manage-side secret policy.
- Automatic package installation or shell interpretation of the configured
  command. The command and arguments are passed directly to the child process.

## Verification

- `go test ./node/...`
- `npm test --prefix node/webui/frontend -- --run`
- `npm run build --prefix node/webui/frontend`
