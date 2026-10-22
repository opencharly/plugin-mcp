---
name: charly-mcp-cmd
description: |-
  Either Model Context Protocol direction: the declarative out-of-process `mcp:` check verb that probes advertised servers, or `charly mcp serve` (reflected Charly CLI over Streamable HTTP or stdio). Covers URL rewriting, host-networked/published ports, tool annotations, read-only filtering, project fallback, authoring tools, the charly-mcp deployment layer.
---

# MCP - Model Context Protocol (client + server)

`charly` speaks MCP in both directions. This skill covers both:

- **Client** — the declarative `mcp:` check verb: connect to any MCP server declared via `mcp_provide` and probe/call/read. Authored as an `mcp:` step in a candy/box plan and run via `charly check live <image> --filter mcp`; it is served OUT-OF-PROCESS by `candy/plugin-mcp` and has **no** host `charly check` subcommand (parallel to the `kube:`/`spice:`/`adb:`/`appium:` plugin verbs). Used to test MCP endpoints shipped by `jupyter-mcp`, `chrome-devtools-mcp`, or the charly server itself.
- **Server** — `charly mcp serve`: expose the entire charly CLI surface (build + test + deploy modes; one tool per leaf command) as MCP over Streamable HTTP or stdio. Used by LLM agents (Claude Code, Open WebUI, OpenClaw) to drive charly remotely. Deployed in-container via the `charly-mcp` layer. The tool catalog includes a project-scaffolding + YAML-editing + file-write authoring surface, so an agent can build an `charly` project from scratch over RPC — see "Authoring tools" below.

Both surfaces are legs of the SAME plugin candy: `candy/plugin-mcp` serves `verb:mcp` (the client) AND `command:mcp` (the server, `serve.go`), sharing one `github.com/modelcontextprotocol/go-sdk`; charly's core links NO MCP SDK — its host half is the SDK-free `charly __cli-model` seam (`charly/cli_model_cmd.go`).

---

# Part 1 — Client (the declarative `mcp:` check verb)

## Overview

The `mcp:` check verb connects to Model Context Protocol servers declared by running containers via `mcp_provide`, using [github.com/modelcontextprotocol/go-sdk](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk) (v1.5.0). The methods cover the full MCP client surface: `ping`, `servers`, `list-tools`, `list-resources`, `list-prompts`, `call`, `read`. No MCP URL argument is ever typed by the user — the out-of-process plugin reads the target image's `ai.opencharly.mcp_provide` OCI label via the generic `cc.ResolveImageLabel` reverse-leg (`effectiveMCPProvides` in `resolve.go`). When the label leg fails — a VM/host venue has no podman-inspectable image label (plugin-check declines with `container for <name> is not running`) — the plugin falls back to the `mcp_provide` declarations threaded through the check env (`spec.CheckEnv.MCPProvide`, the P4 substrate-neutral path; a VM's declared forward is threaded host-routable by plugin-check, so the dial reaches the VM's server through the forwarded port). From either source it resolves `{{.ContainerName}}` templates, applies pod-aware `localhost` rewriting, and maps the container-network URL to a host-routable dial address via `cc.ResolveEndpoint` — an env-declared host-local `127.0.0.1` URL (the VM/host server's port already on the check host's loopback) is dialed as-is (the host owns that machinery behind the class-generic reverse-legs).

**Served out-of-process — no host CLI subcommand.** The verb is a DECLARATIVE check verb only; there is no `charly check` subcommand for it (just like `kube:`/`spice:`/`adb:`/`appium:`). The MCP-client implementation (the go-sdk dial + the MCP methods) lives in `candy/plugin-mcp`, an out-of-tree charly plugin that charly's loader go-builds on the host and serves out-of-process over go-plugin gRPC (`LocalTransport`). A `check:` step carrying `mcp:` dispatches through the provider registry exactly like a built-in verb (`ResolveVerb("mcp")` → grpcProvider → `Provider.Invoke` with the full `Op` marshaled + a `CheckEnv` snapshot). Exercise it with `charly check live <image> --filter mcp`.

### Authoring shape

Each `mcp:` step is a `check:` step — an ordered list item under a candy/box `plan:`. The method name is the scalar value for a bare-method step (`mcp: ping`), or the `method:` key of the `mcp:` map when the step carries mcp-exclusive fields (`tool:`, `uri:`, `input:`, `mcp_name:`) — those live INSIDE the `mcp:` map. Only the shared matchers (`stdout:`, `stderr:`, `exit_status:`) and `context:`/`timeout:`/`id:` stay siblings. See `/charly-check:check` for the parent router and the complete method allowlist. Example:

```yaml
- check: jupyter exposes its core notebook mcp tools
  context: [deploy]
  mcp: list-tools
  stdout:
    - contains: insert_cell
    - contains: execute_cell
```

## Quick Reference

| Action | Declarative step | Description |
|--------|------------------|-------------|
| Ping | `mcp: ping` | Liveness check — emits `ok` on a successful `Ping` RPC |
| Enumerate | `mcp: servers` | List MCP servers declared by the image (no dial) |
| List tools | `mcp: list-tools` | Tool name + first-line description per line |
| List resources | `mcp: list-resources` | URI + name + MIME type per line |
| List prompts | `mcp: list-prompts` | Prompt name + description per line |
| Call | `mcp: {method: call, tool: …}` (+ optional `input:`) | Invoke a tool; emits TextContent payload |
| Read | `mcp: {method: read, uri: …}` | Read a resource; emits Text content |

The `tool:`/`uri:`/`input:`/`mcp_name:` fields live INSIDE the `mcp:` map; a
bare-method step (`mcp: ping`) uses the scalar form. Every method accepts:
- `mcp_name: <server>` (in the map) — disambiguate when the image declares multiple `mcp_provide` entries
- `timeout: <duration>` (a shared `#Op` sibling) — per-operation timeout (default 30s)
- the shared matchers (`stdout:`, `stderr:`, `exit_status:`) as siblings

Output is always tab-separated plaintext fed to the matcher pipeline (no `--json` form — that was a property of the retired host CLI). Run a candy's baked `mcp:` steps against a live deployment with `charly check live <image> --filter mcp`.

## Architecture

The host owns all podman / OCI-label / port-mapping machinery behind the class-generic reverse-legs (`cc.ResolveImageLabel` for the declared servers + `cc.ResolveEndpoint` for the host-routable dial address); the out-of-process `candy/plugin-mcp` provider resolves its endpoint through them and builds a single `*mcp.ClientSession` per invocation. The plugin needs no container inspection at all.

1. **Container resolution** (host): `resolveContainer(image, instance)` → `charly-<image>[-<instance>]`.
2. **Image ref + metadata** (host): `containerImageRef` → `ExtractMetadata` → `meta.MCPProvide` (read from OCI label `ai.opencharly.mcp_provide`).
3. **Template substitution** (host): any `{{.ContainerName}}` in the URL is replaced with the resolved container name.
4. **Pod-aware rewrite** (host): `podAwareMCPProvides` folds same-image entries so the URL host becomes `localhost` (identical to the CHARLY_MCP_SERVERS path used by `charly config`).
5. **Host-side port rewrite** (host): the load-bearing piece. The host parses the URL; if the host is the container name or `localhost`, it looks up the published host port for the URL's port via `podman inspect` (same `NetworkSettings.Ports` data that powers `${HOST_PORT:N}` in declarative tests) and rewrites to `127.0.0.1:<host-port>`. Non-matching hosts (external URLs) pass through unchanged. The pre-resolved `mcp_provides` + the single picked endpoint travel to the plugin in the `CheckEnv` snapshot.
6. **Transport pick** (plugin): `transport: http` (or empty) → `StreamableClientTransport{Endpoint}`; `transport: sse` → `SSEClientTransport{Endpoint}`; any other string errors at dial time with the declared transport echoed.
7. **Session** (plugin): `mcp.NewClient(…).Connect(ctx, transport, nil)` runs the MCP `initialize` handshake and returns an opened `ClientSession`; the provider evaluates the step's matchers itself and returns a `{status,message}` verdict to the host. A `defer session.Close()` always fires.

Source: the endpoint resolution lives in `candy/plugin-mcp` (`resolve.go` — the `effectiveMCPProvides` source selector over the `cc.ResolveImageLabel` + `cc.ResolveEndpoint` reverse-legs, plus the dial: the OCI label is authoritative when it carries entries; an absent/empty label or a non-inspectable venue falls back to the check env's `mcp_provide` declarations), alongside the MCP client (the MCP methods + the dial: `provider.go` dispatch + `methods.go` client layer). See `/charly-internals:plugin` for the out-of-process provider model and `/charly-internals:go` for the host-side map.

## Methods

Each method is shown as the declarative `mcp:` step you author. Run them against a live deployment with `charly check live <image> --filter mcp`.

### Ping

```yaml
- check: the jupyter mcp server responds to ping
  context: [deploy]
  mcp: ping
  stdout:
    equals: ok
```

Calls `ClientSession.Ping(ctx, nil)`. Passes iff the server responds. Useful as a liveness check, often paired with a short `timeout:`.

### Servers (discovery only, no dial)

```yaml
- check: the image advertises the jupyter mcp server
  context: [deploy]
  mcp: servers
  stdout:
    contains: jupyter        # emits "<name>\t<url>\t<transport>" per server, e.g. jupyter http://localhost:8888/mcp http
```

Reads `mcp_provide` from the OCI label, applies template substitution + pod-aware rewrite, emits the result. No MCP handshake — metadata only. Useful for confirming which server names the image advertises before dialing.

### List tools / resources / prompts

```yaml
- check: the jupyter mcp server exposes its notebook tools
  context: [deploy]
  mcp: list-tools
  stdout:
    - contains: list_notebooks   # "list_notebooks\tList all notebooks accessible in the workspace."
    - contains: execute_cell
```

Plaintext output is tab-separated (`name\tfirst-line-of-description`) for easy `contains:` matching without JSON parsing. Multi-line descriptions collapse to the first non-empty line. `list-resources` emits `uri\tname\tmime`; `list-prompts` emits `name\tdescription`. The provider automatically pages through `NextCursor` so all results return in a single invocation.

### Call a tool

```yaml
- check: list_notebooks returns successfully
  context: [deploy]
  mcp:
    method: call
    tool: list_notebooks          # required
    input: "{}"                   # optional JSON arg blob; omit for zero-arg tools
  exit_status: 0                  # assert no IsError (shared #Op sibling)
```

The `input:` field is the tool's arguments as a JSON object (optional — omit for zero-arg tools), parsed with `encoding/json`. Returned `TextContent` blocks emit one per line; `ImageContent` / `AudioContent` emit a `[image content: <mime>, <N> bytes]` placeholder. If the server sets `IsError: true`, the step FAILS with the error text.

### Read a resource

```yaml
- check: reading a resource returns a non-empty body
  context: [deploy]
  mcp:
    method: read
    uri: file:///workspace/data.txt   # required
  stdout:
    - matches: "."                # non-empty body
```

Calls `ReadResource(URI: …)`. Text content is emitted to stdout; binary blobs emit a `[binary resource …]` placeholder.

## Disambiguating multiple servers

When an image declares more than one `mcp_provide` entry, every `mcp:` step requires the `mcp_name:` modifier; without it the step FAILS with `image provides multiple mcp servers; use mcp_name (available: jupyter, chrome-devtools)`:

```yaml
- check: the chrome-devtools mcp server responds to ping
  context: [deploy]
  mcp:
    method: ping
    mcp_name: chrome-devtools
```

No existing image ships multiple providers today — `jupyter` layers expose one server (`jupyter`), `chrome-devtools-mcp` exposes one (`chrome-devtools`). The disambiguation exists for future compositions.

## Port-publishing gotcha (encountered during live testing)

**Symptom:** a `mcp: ping` step (e.g. via `charly check live <image> --filter mcp`) fails with:

```
mcp chrome-devtools: container port 9224/tcp is not published to a host port;
declare `ports: [9224:9224]` in the image or run the test from inside the pod
```

**Cause:** the image's OCI label declares the port (e.g., `chrome-devtools-mcp` adds `9224` to `sway-browser-vnc`'s port list), but the running container's quadlet doesn't publish it because `charly.yml`'s per-image `port:` entry **replaces** (not appends to) the image-declared list. When a new mcp-providing layer is added to an image that already has a `charly.yml` entry with an explicit `port:` list, the new port silently drops out.

**Fix:** either add the mcp port to the instance's `charly.yml` entry:

```yaml
# ~/.config/charly/charly.yml
sway-browser-vnc:
    pod:
        image: sway-browser-vnc
        port:
            - "5900:5900"
            - "9250:9222"
            - "9224:9224"   # ← add this
```

… or remove the `port:` override entirely so the image default (which already includes 9224) applies. Re-run `charly config <image>` and restart the service (`charly stop && charly start` — `charly update` may no-op if the image tag hasn't changed).

**Verification** that the fix worked:

```bash
charly status <image> --json
# Expect: the mcp port listed in the port mappings with a non-null host port
charly check live <image> --filter mcp
# the mcp: ping step passes
```

Instance-specific deployments (the `-i <instance>` form) typically have their ports declared at create-time and are less prone to this drift.

## Transport dispatch

| Declared `transport:` | SDK transport | Notes |
|-----------------------|---------------|-------|
| `http` or empty | `StreamableClientTransport{Endpoint}` | Project default; jupyter-mcp and chrome-devtools-mcp both speak this |
| `streamable`, `streamable-http` | `StreamableClientTransport{Endpoint}` | Aliases |
| `sse` | `SSEClientTransport{Endpoint}` | Legacy SSE servers |
| anything else | error | `"unsupported mcp transport %q (expected http or sse)"` |

The SDK's transports are struct-literal constructors — there is no `NewStreamableClientTransport(...)` helper. Setting `Endpoint` is the only required field.

## Output format: plaintext

Every method emits author-friendly plaintext with one record per line:

- `ping` → `ok`
- `list-tools` → `<name>\t<first-line-of-description>`
- `list-resources` → `<uri>\t<name>\t<mime-type>`
- `list-prompts` → `<name>\t<description>`
- `call` → concatenated `TextContent.Text` payloads, one per line
- `read` → concatenated `ResourceContents.Text`, one per line
- `servers` → `<name>\t<url>\t<transport>`

The plaintext is fed straight to the matcher pipeline. There is no JSON output form — the retired host CLI's `--json` flag went away with the subcommand; the out-of-process `candy/plugin-mcp` provider returns the plaintext to the host, which evaluates the step's matchers.

## Declarative authoring examples

Checks currently shipping in the three provider candies (`candy/jupyter/charly.yml`, `candy/jupyter-ml/charly.yml`, `candy/chrome-devtools-mcp/charly.yml`), as list items under each candy's `plan:`:

```yaml
# Liveness check — fastest sanity verification
- check: the jupyter mcp server responds to ping
  context: [deploy]
  mcp: ping
  timeout: 10s

# Catalog assertion — ensure the server exposes the tools we expect
- check: the jupyter mcp server exposes the expected tools
  context: [deploy]
  mcp: list-tools
  stdout:
    - contains: insert_cell
    - contains: execute_cell

# Real tool invocation — exercises the full request/response path
- check: list_notebooks returns successfully
  context: [deploy]
  mcp:
    method: call
    tool: list_notebooks
    input: "{}"
  exit_status: 0

# Tool with arguments
- check: get_notebook returns a notebook with cells
  context: [deploy]
  mcp:
    method: call
    tool: get_notebook
    input: '{"path":"getting-started.ipynb"}'
  stdout:
    - contains: cells

# Resource read
- check: a prompt resource reads back non-empty
  context: [deploy]
  mcp:
    method: read
    uri: file:///workspace/prompt.txt
  stdout:
    - matches: "."
```

Each `mcp:` step is a `check:` step — a deterministic probe that satisfies the mandatory-ADE gate. **Deploy-context only.** `mcp:` steps require a running container with the mcp port published; `charly box validate` rejects build-context mcp steps at authoring time, and `charly check box` skips them at runtime with the message `"mcp: <method> requires a running container (skip under charly check box)"`. Follow the same rule as the other four live-container verbs — `cdp`, `wl`, `dbus`, `vnc`.

## Validator coverage

`charly box validate` + the CUE schema enforce (the `mcp` verb is an out-of-process plugin — its method-name enum is validated by CUE on core `#Op`, its required-modifier checks run in `candy/plugin-mcp` at dispatch):

- Method name must be in `mcpMethods` (7 entries); unknown methods list the allowed set in the error.
- `context` must include `deploy`; a build-context mcp step raises `"mcp: verb requires context:\"deploy\""`.
- `call` requires `tool:`; missing field raises `"mcp: call requires modifier \"tool\""`.
- `read` requires `uri:`.

No other required modifiers — `ping`, `servers`, `list-*` take only the optional `mcp_name:`.

---

# Part 2 — Server (`charly mcp serve`)

## Overview

`charly mcp serve` runs the charly CLI *as* an MCP server. Every leaf command in the Kong CLI tree — `box.build`, `status`, `test.mcp.ping`, `config.setup`, `box.new.project`, `candy.add-rpm`, etc. — becomes a callable MCP tool. Tool catalogs are **auto-generated from Kong struct tags** by reflection — host-side, over the hidden `charly __cli-model` seam — with no hand-written schema per command. Result: **one tool per leaf command** covering the entire build + test + deploy surface, including the MCP-first authoring verbs (`image.{new.project, new.image, set, add-layer, rm-layer, fetch, refresh, write, cat}` + `layer.{set, add-rpm, add-deb, add-pac, add-aur}`).

```bash
charly mcp serve                                # Streamable HTTP on 127.0.0.1:18765/mcp (loopback only by default)
charly mcp serve --listen 0.0.0.0:18765         # Opt-in: bind all interfaces (normal inside a pod/VM)
charly mcp serve --listen 127.0.0.1:9999        # Custom port
charly mcp serve --path /api/mcp                # Custom HTTP path prefix (default /mcp)
charly mcp serve --stdio                        # Stdio transport for editor/LLM integration
charly mcp serve --read-only                    # Skip registering the destructive tools
```

## Listen/bind default (P3) — loopback only

`charly mcp serve` binds **`127.0.0.1:18765` by default** — loopback only; the host system is the security boundary. `--listen 0.0.0.0:18765` is the explicit opt-in for remote/container reachability — **normal inside a pod/VM**, where the container network is the boundary.

The generated packages (`charly generate-packages`, the packaging section) ship **non-autostarting systemd units — system AND user scope** (rendered to `/usr/lib/systemd/{system,user}/` by `sdk/packagekit`; deb/rpm/archlinux only). They are installed but NEVER enabled: the operator starts the server on demand (`systemctl start charly-mcp` / `systemctl --user start charly-mcp`). Both units run `charly mcp serve --listen 127.0.0.1:18765` with `WorkingDirectory=/etc/charly`, resolving the package's **system-wide `/etc/charly/charly.yml`** — the plugin config the server needs, so the server uses a local project instead of falling back to a network fetch of `opencharly/charly`. A user-scope-unit server additionally reads the **invoking user's** `~/.config/charly/charly.yml` on top of the system config — the layered config merge (`/etc/charly` → user config → in-dir project, later files winning; `sdk/loaderkit/config_stack.go`).

Child tool processes inherit the **FULL environment**: `childCharlyEnv` returns `os.Environ()` with no `CHARLY_*` stripping — the environment is authoritative (config precedence env → flag → `--repo` → cwd), so an inherited `CHARLY_PROJECT_DIR`/`CHARLY_PROJECT_REPO` wins over the managed project prefix.

Server and client share one `github.com/modelcontextprotocol/go-sdk` inside the same plugin candy, so the wire format is identical and the `mcp: ping` check verb works against it unchanged.

## Architecture

The server is the `command:mcp` leg of `candy/plugin-mcp` (`serve.go`), fed by ONE hidden host seam: `charly __cli-model` (`charly/cli_model_cmd.go`), which emits charly's ASSEMBLED Kong command tree — the core CLI struct plus the builtin command-provider grammar — as an `sdk.CLIModel` JSON document on stdout. Core links no MCP SDK.

1. **Reflection (host-side)** — at startup `buildMcpServer(bin, readOnly, noDefaultRepo)` fork/execs `charly __cli-model` (`fetchCLIModel` — deliberately with NO project prefix: the model needs no project, so a cold-start network blip can never strip the project prefix) and registers one tool per model leaf via `cliLeafToTool(leaf, destructive)`:
   - **Name**: dot-joined command path (e.g. `box.build`, `test.mcp.ping`, `config.setup`).
   - **Description**: the leaf's help, with a `[destructive: …]` annotation appended for mutating tools.
   - **InputSchema**: JSON schema built from the model's args (`argToSchema`) — positionals become required properties, flags optional ones, `enum:`/`default:` surfaced. **Every schema has `additionalProperties: false`** — unknown keys are rejected by the SDK's input validation before the handler runs. The schema validator is LLM-honest about the allowed surface.
   - **Annotations**: destructive tools get `DestructiveHint: &true`; everything else gets `ReadOnlyHint: true`.

2. **Destructive gating** — `mcpDestructivePaths` (`serve.go`) is an explicit allowlist of mutating tool paths (the lifecycle, config, secrets, deploy, image build/scaffold/edit, layer edit, VM, udev, alias, record, tmux, and settings families). When `--read-only` is set, `buildMcpServer` skips registration entirely rather than gating at runtime. `box.fetch` (idempotent, additive cache prime) and `box.cat` (read-only file read) are **not** in the destructive set despite living in the authoring family — they're safe under `--read-only`.

3. **Tool invocation — fork/exec, no in-process capture** — `makeToolHandler(bin, prefix, leaf)` returns a closure. On call: decode the MCP JSON arguments, reconstruct a `[]string` argv via `argvFromJSON(…)` (booleans → bare flag, slices → repeated `--flag=value`, positionals in model order), then `forkCharly` runs `charly <prefix> <path> <args…>` as a SUBPROCESS and `assembleToolText` returns the captured stdout/stderr as a single `TextContent`. Errors become `IsError: true` tool results, not MCP-protocol errors — the LLM sees the failure text. The fork/exec design REPLACED the former in-process capture model wholesale: there is no `os.Stdout` redirect, no capture mutex, and subprocess output cannot leak past its own tool result.

4. **Project prefix** — every tool call carries the managed prefix from `computeProjectPrefix` (see "Project-dir wiring" below), computed so the ENVIRONMENT stays authoritative: `CHARLY_PROJECT_DIR`/`CHARLY_PROJECT_REPO` set, or a `charly.yml` in the cwd, both suppress the prefix (the child inherits the env and resolves the project itself); `--no-default-repo` with no local project leaves it empty too; only otherwise is a `--repo default` prefix prepended. Children inherit the FULL environment — `childCharlyEnv` returns `os.Environ()` verbatim, no `CHARLY_*` stripping.

5. **Transport** — `mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)` wraps the server for HTTP mode; `--stdio` runs the same server over the stdio transport.

## Deployment: the `charly-mcp` layer

Composing `charly-mcp` into an image deploys the server via supervisord:

```yaml
# candy/charly-mcp/charly.yml (summary; the candy's required version: is elided)
charly-mcp:
    candy:
        description: Runs the charly CLI itself as an MCP server (charly mcp serve) over Streamable HTTP on :18765
charly-mcp-candy:
    candy:
        - charly
        - '@github.com/opencharly/layer-supervisord'
charly-mcp-port:
    port:
        - 18765
charly-mcp-mcp_provide:
    mcp_provide:
        - name: charly
          url: "http://{{.ContainerName}}:18765/mcp"
          transport: http
charly-mcp-volumes:
    volumes:
        - name: project        # Bind-mount the project root; see below
          path: /workspace
charly-mcp-env:
    env:
        CHARLY_PROJECT_DIR: "/workspace"
charly-mcp-service:
    service:
        - name: charly-mcp
          exec: /usr/local/bin/charly mcp serve --listen :18765
          restart: always
          enable: true
          scope: system
```

**Project-dir wiring** — build-mode tools (`box.build`, `box.inspect`, `box.list.*`) resolve `charly.yml` via `os.Getwd()`. Inside the container, cwd is `/workspace` (set by the `charly-mcp` layer's `CHARLY_PROJECT_DIR` env + `volume:` declaration). Three deployment patterns, in order of progressively less local setup:

1. **Bind-mount** — the canonical `charly-mcp` pattern. The candy ships `env: CHARLY_PROJECT_DIR: /workspace` + `volumes: project → /workspace`; the deployer attaches a host checkout via `charly config <image> --bind project=/path/to/opencharly`. The charly CLI's global `-C` / `--dir` / `CHARLY_PROJECT_DIR` flag honours the env var before Kong dispatch, calling `os.Chdir(CHARLY_PROJECT_DIR)` once. Use this when the agent should see your in-flight local edits. The volume NAME is `project` (stable bind-mount API); the in-container PATH is `/workspace` (the generic name works whether the contents are an opencharly checkout or any other workspace).

2. **Remote pin** — set `CHARLY_PROJECT_REPO=opencharly/charly@<sha-or-ref>` in the container env (e.g. via `charly config <image> -e CHARLY_PROJECT_REPO=...`). The charly CLI clones (or hits its `~/.cache/charly/repos/` cache) and chdirs into the cache path before Kong dispatch. No bind mount required. Use this for reproducible agent runs against a pinned upstream.

3. **Auto-default** — `charly mcp serve` with no charly.yml reachable at cwd falls back to `github.com/opencharly/charly` by prepending a managed `--repo default` prefix to every project-dependent tool call (`computeProjectPrefix` in `candy/plugin-mcp/serve.go`); the child `charly` resolves + fetches the default-repo cache. The `--repo default` prefix applies only when nothing else expresses a project: an env-set `CHARLY_PROJECT_DIR`/`CHARLY_PROJECT_REPO` wins by precedence (env → flag → `--repo` → cwd) and suppresses the prefix, and so does a `charly.yml` in the cwd — children inherit the full environment (`childCharlyEnv` returns `os.Environ()` with no `CHARLY_*` stripping), so a set env var is never fought. This matters because the `charly-mcp` layer permanently sets `CHARLY_PROJECT_DIR=/workspace` — the env-driven wiring the P2 rule protects: a deployer who forgets the `--bind` still gets a RUNNING server (the tool-model fetch needs no project), but project-dependent tools (`box.*`) report no project until `/workspace` actually holds a `charly.yml`. Pass `--no-default-repo` to opt out — the server still runs, and project-dependent tools error at call time (the child reports "no project"). This is the only command surface that auto-fetches; the top-level CLI stays opt-in.

See `/charly-image:image` "Project directory resolution" for the flag/env semantics; the implementation is `computeProjectPrefix` + `childCharlyEnv` in `candy/plugin-mcp/serve.go`.

**Composition style** — `charly-mcp` uses `candy: [charly, supervisord]` (meta-layer composition) rather than `require:` (hard prerequisite) because it adds no install of its own — it's pure wiring. Boxes that want the MCP server add `charly-mcp` to their candy list; boxes that just want the charly binary continue to use the `charly` candy alone. Both `candy:` and `require:` reference other candies, but only `candy:` lets the using candy ship no install files.

## Verifying end to end

```bash
# Build an charly-mcp-bearing image (e.g. charly-arch) and start it:
charly box build charly-arch
charly config charly-arch --bind project=/home/you/opencharly
charly start charly-arch

# From the host — the container's mcp_provide URL is auto-rewritten to the published host
# port, then the candy's baked declarative mcp: steps (each carrying mcp_name: charly)
# run against the live deployment:
charly check live charly-arch --filter mcp
# the mcp: ping / list-tools / call (version, box.list.boxes) steps all pass
#   list-tools → the full tool catalog; call version → 2026.nnn.nnnn; call box.list.boxes →
#   charly-arch [testing], arch [testing], … (reads charly.yml from the bind-mounted /workspace)
```

The deploy-context `check:` step nodes in `candy/charly-mcp/charly.yml` cover this exact sequence: service-running, port-reachable, `mcp: ping`, `mcp: list-tools`, `mcp: call tool=version`, `mcp: call tool=box.list.boxes` (the last proves the bind-mount + CHARLY_PROJECT_DIR wiring). For an ad-hoc MCP client outside charly, point any MCP-speaking tool (e.g. `/charly-tools:mcporter`) at the published host port.

## Authoring tools (build-from-scratch over MCP)

Every CLI verb under `charly box …` and `charly candy …` auto-becomes an MCP tool via Kong reflection. The authoring surface added for "build a project from scratch using only `charly mcp`" exposes these tools:

| MCP tool | What it does |
|---|---|
| `box.new.project` | Scaffold `charly.yml` (the build vocabulary is embedded in the charly binary), `candy/`, `.gitignore`. |
| `box.new.box` | Append a new image entry to `charly.yml`. |
| `box.new.candy` | Scaffold `candy/<name>/charly.yml` with a stub. |
| `box.set` | Set any value in `charly.yml` by dot-path (`defaults.tag`, `images.foo.layers`, …). Value is parsed as YAML. |
| `box.add-candy` / `box.rm-candy` | Append / remove a layer from an image's `candy:` list (idempotent). |
| `candy.set` | Set any value in `candy/<name>/charly.yml` by dot-path. |
| `candy.add-rpm` / `candy.add-deb` / `candy.add-pac` / `candy.add-aur` / `candy.add-apk` | Append packages to a layer's `<format>.packages` list. Idempotent. Upgrades scaffold's null `package:` value to a real sequence. |
| `box.fetch` / `box.refresh` | Pre-prime / re-clone the remote-repo cache. Spec defaults to `default` (opencharly/charly). |
| `box.write` / `box.cat` | Write / read any file under the project root — escape hatch for free-form auxiliary files (`pixi.toml`, `package.json`, `root.yml`, scripts, `*.service`). Path is resolved against `os.Getwd()` and rejected if it escapes the project root. |

All YAML edits go through the `yaml.v3` *node* API (not value unmarshal) so comments and key order are preserved across edits. The generic comment-preserving setter/node helpers (`kit.SetByDotPath` / `kit.MappingChild`) live in `sdk/kit/yaml.go` (tested in `sdk/kit/yaml_test.go`); the box authoring editors live in `candy/plugin-authoring/authoring_edit.go` (P14b — moved from core `charly/scaffold_cmds.go`; tested in `candy/plugin-authoring/authoring_edit_test.go`); the `candy` authoring command is the compiled-in `command:candy` plugin (`candy/plugin-candy/command.go`).

End-to-end MCP-only worked example:

```jsonc
// All called as MCP tool calls (e.g. via an `mcp: call` check step, or any MCP client):
box.new.project   {"dir": "/tmp/hello"}
box.new.box     {"name": "hello", "base": "quay.io/fedora/fedora:43"}
box.new.candy     {"name": "hello-svc"}
candy.add-rpm       {"name": "hello-svc", "packages": ["openssh-server"]}
box.add-candy     {"image": "hello", "layer": "hello-svc"}
image.validate      {}
box.build         {"image": "hello"}
box.inspect       {"image": "hello"}
```

## Port choice

Default listen `127.0.0.1:18765` (loopback only — P3; `0.0.0.0` is the explicit opt-in) chosen for non-collision with other MCP layers:
- `8888` — jupyter-mcp
- `9224` — chrome-devtools-mcp (via mcp-proxy)
- `18789` — openclaw gateway

## Policy note

The server registers destructive tools with `DestructiveHint: true` rather than withholding them. The LLM runtime (Claude Code, Open WebUI) is responsible for acting on the hint — e.g. prompting the user before calling an annotated tool. For hostile-LLM scenarios or untrusted network deployments, run with `--read-only` (drops the mutating tools at registration time) and/or restrict reach via the tunnel / Traefik candy.

---

## Cross-References

- `/charly-check:check` — parent router for the declarative `mcp:` check verb. The full `mcp` method allowlist (and how the verb relates to the other out-of-process live-container plugin verbs wl/cdp/vnc/dbus/record/kube/spice/adb/appium/libvirt) lives in its "Live-container verb catalog" + "Method allowlist — mcp" sections.
- `/charly-image:layer` — `mcp_provide` / `mcp_accept` / `mcp_require` field reference for layer authoring.
- `/charly-core:charly-config` — how `mcp_provide` gets injected into `charly.yml` `provides.mcp:` and synthesized into `CHARLY_MCP_SERVERS` for consumers at `charly config` time; pod-aware resolution to `localhost`; instance-aware MCP server naming with `-<instance>` suffix.
- `/charly-build:validate` — authoring-time validation rules (method allowlist, required modifiers, scope enforcement).
- `/charly-core:deploy` — `charly.yml` `port:` override semantics (the port-publishing gotcha lives here operationally).
- `/charly-check:cdp` — sibling live-container verb (Chrome DevTools Protocol).
- `/charly-check:wl` — sibling (Wayland desktop control).
- `/charly-check:dbus` — sibling (D-Bus calls/notifications).
- `/charly-check:vnc` — sibling (VNC framebuffer / input).
- `/charly-jupyter:jupyter-mcp` — the FastMCP server implementation layer (the notebook tools over CRDT: notebook_*/cell_* + notebook_list_users + room_list; clients do not manage CRDT rooms — the server auto-attaches).
- `/charly-selkies:chrome-devtools-mcp` — the mcp-proxy wrapper around chrome-devtools-mcp (the browser-automation tools).
- `/charly-hermes:hermes` — a consumer (`mcp_accept: jupyter, chrome-devtools`); use the `mcp:` check verb (`charly check live <consumer> --filter mcp`) to verify the services hermes discovers are actually alive.
- `/charly-openwebui:openwebui` — another consumer (`mcp_accept: jupyter, chrome-devtools`).
- `/charly-jupyter:jupyter`, `/charly-jupyter:jupyter-ml`, `/charly-jupyter:jupyter-ml-notebook` — images bundling `jupyter-mcp`; `charly check live <image> --filter mcp` exercises the verb end-to-end.
- `/charly-selkies:sway-browser-vnc`, `/charly-selkies:selkies-labwc`, `/charly-selkies:selkies-labwc-nvidia` — images bundling `chrome-devtools-mcp` (transitively via the chrome metalayer).
- `/charly-internals:go` — host-side implementation map: `check_endpoint_resolve.go` (the host-endpoint reverse-legs — `resolveImageLabel` + `resolveVerbEndpoint` the mcp plugin pulls), `cli_model_cmd.go` (the `charly __cli-model` seam the server consumes; the server itself is `candy/plugin-mcp/serve.go`), `candy/plugin-box/validate_check.go` (op-level deploy-scope enforcement; the `mcp` method-name + required-modifier checks live in `candy/plugin-mcp` + the CUE `#Op` enum). The MCP CLIENT (the MCP methods + the go-sdk dial) lives out-of-process in `candy/plugin-mcp` — see `/charly-internals:plugin`.
- `/charly-coder:charly-mcp` — the deployment layer that wires `charly mcp serve` into an image via supervisord. Includes the `/workspace` bind-mount (volume NAME `project`) + `CHARLY_PROJECT_DIR` env var pattern for build-mode tools.
- `/charly-tools:charly` — the underlying binary layer; required by `charly-mcp`.
- `/charly-image:image` — "Project directory resolution" subsection documents the `-C` / `--dir` / `CHARLY_PROJECT_DIR` global flag that makes the server's project-dir bind-mount work.
- `/charly-coder:charly-arch`, `/charly-distros:charly-fedora` — canonical images composing `charly-mcp` for remote charly-via-MCP deployments.

## When to Use This Skill

**MUST be invoked** when the task involves Model Context Protocol on either side:

- **Client** — the declarative `mcp:` check verb (served out-of-process by `candy/plugin-mcp`; no host `charly check` subcommand), probing/testing MCP servers declared via `mcp_provide`, examining MCP tool/resource/prompt catalogs, debugging the URL-rewriter or port-publishing behavior, or authoring deploy-context `mcp:` step nodes in a candy/box plan.
- **Server** — `charly mcp serve` operation, the `charly-mcp` layer, destructive-hint policy, the `--read-only` filter, Kong-reflection tool generation, the project-dir bind-mount pattern, or symptoms like "MCP tool returned empty output" (check `println` vs `fmt.Println` in the invoked command — the server captures `os.Stdout`, not fd 1).

Invoke this skill BEFORE reading source code or launching Explore agents.

**Workflow position:** Test mode / live service operation. Client: deploy an image with `mcp_provide`, start it, then probe. Server: compose `charly-mcp` into an image, bind `--bind project=/path/to/opencharly` at config time, start the container, then consume from any MCP-speaking LLM runtime. Pairs with `/charly-check:check` (parent router + declarative-verb catalog), `/charly-core:charly-config` (consumer-side injection + `--bind` for the project dir), `/charly-coder:charly-mcp` (server deployment layer), and the consumer-layer skills (`/charly-hermes:hermes`, `/charly-openwebui:openwebui`).
