# plugin-mcp

Model Context Protocol, both directions, for OpenCharly — the declarative `mcp:`
check verb (the MCP client) and `charly mcp` (the MCP server).

The plugin is an out-of-tree Go module: charly fetches this repo at the pinned
tag, go-builds the provider on the host, and serves it **out-of-process** over
go-plugin gRPC via the plugin SDK. That keeps the
`github.com/modelcontextprotocol/go-sdk` dependency out of charly's core
`go.mod`, while `mcp:` authoring stays unchanged — the verb dispatches through
the provider registry exactly like a built-in.

## What it provides

| Capability | Surface |
|---|---|
| `verb:mcp` | the `mcp:` check verb — the MCP client. 7 methods: `ping`, `servers`, `list-tools`, `list-resources`, `list-prompts`, `call`, `read` |
| `command:mcp` | `charly mcp serve` — the MCP server, exposing the charly CLI surface over Streamable HTTP or stdio |

`verb:mcp` is served over gRPC (the provider registry); `command:mcp` is served
via the CLI fork/exec path, so it is declared for the CLI-grammar prescan but not
advertised in `Describe`.

The `mcp:` verb resolves the target server from the image's
`ai.opencharly.mcp_provide` OCI label (or the check-env `mcp_provide`
declarations) and maps it to a host-routable address — no MCP URL is ever typed
by the user.

## How to use it

Compose the plugin candy in a box or check bed's `candy:` list:

```yaml
- '@github.com/opencharly/plugin-mcp/candy/plugin-mcp:<tag>'
```

Then author the verb in a plan:

```yaml
- check: jupyter exposes its core notebook mcp tools
  context: [deploy]
  mcp: list-tools
  stdout:
    - contains: insert_cell
    - contains: execute_cell
```

The MCP-exclusive fields (`tool:`, `uri:`, `input:`, `mcp_name:`) live inside the
`mcp:` map; the shared matchers (`stdout:`, `stderr:`, `exit_status:`) and
`timeout:` stay siblings. Run a candy's baked steps with
`charly check live <image> --filter mcp`.

## Layout

- `candy/plugin-mcp/` — the plugin module: `plugin.go` (provider + meta),
  `methods.go`, `resolve.go`, `command.go`, `serve.go`, `schema/mcp.cue` (the
  self-contained `#McpInput`), `params/cue_types_gen.go`, and `cmd/serve/main.go`.
- `charly.yml` — the root project manifest (`discover: candy`).
- `.github/workflows/tag-on-merge.yml` — CalVer tag + `CHANGELOG/` on merge.

## Related

- Owning skill: `/charly-build:charly-mcp-cmd` — the `mcp:` check verb + `charly
  mcp serve` reference (owned by `layer-charly-build`; the plugin candy carries
  no `skill:` entity of its own, and the gap is tracked in
  [opencharly/opencharly#291](https://github.com/opencharly/opencharly/issues/291)).
- `/charly-internals:plugin` — the plugin/provider model.
- [`opencharly/charly`](https://github.com/opencharly/charly) — the charly CLI.
