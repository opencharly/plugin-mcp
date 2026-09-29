# AGENTS.md — plugin-mcp

Standalone out-of-tree plugin repo serving two MCP capabilities (`verb:mcp` +
`command:mcp`). The plugin is a Go module at `candy/plugin-mcp/` (module path
`github.com/opencharly/plugin-mcp/candy/plugin-mcp`); the root `charly.yml` only
declares `discover: candy` so the repo is a project and its candy is scanned.

Canonical files:

- `candy/plugin-mcp/charly.yml` — the `plugin-mcp:` candy entity (`plugin:`
  block, `plan:` checks).
- `candy/plugin-mcp/` — the Go source: `plugin.go`, `provider.go`, `methods.go`,
  `resolve.go`, `command.go`, `serve.go`, `schema/mcp.cue`,
  `params/cue_types_gen.go`, `cmd/serve/main.go`.
- `.github/workflows/tag-on-merge.yml` — CalVer tag + `CHANGELOG/` on merge.
- `README.md` — user overview only; never agent guidance.

## Load these skills first (R0)

- `/charly-internals:plugin` — the plugin authoring reference: the `plugin:`
  block, the unified Provider model, the per-plugin CUE-schema contract,
  placement. Load before touching any provider or schema.
- `/charly-build:charly-mcp-cmd` — the `mcp:` check verb (client) + `charly mcp
  serve` (server) reference, the plugin's user-facing surface. Load before
  changing a verb method or the served CLI grammar.
- `/charly-check:check` — the declarative check-step surface the `mcp:` verb is
  authored through.
- `/charly-internals:git-workflow` — before any git/PR action.

## Build / validate / test

- `go build ./...` in `candy/plugin-mcp/` — compile the plugin module.
- `go test ./...` in `candy/plugin-mcp/` — the plugin's Go tests.
- `charly box validate` at the repo root — the structural check (the candy +
  `plugin:` block, CUE schema).
- The merge gate is the **org-wide** `charly/pr-validator` (required check
  `validate / validate`, defined in `opencharly/.github`); this repo has **no**
  per-repo candy gate.
- The live R10 witness is an MCP-providing pod bed whose check composes this
  plugin (e.g. jupyter / chrome-devtools-mcp) plus the `charly-mcp` candy that
  runs `charly mcp serve`.

## Modify this repo

- Edit the `plugin-mcp:` candy entity, the Go source, and `schema/mcp.cue`
  **together** — the schema is the single source for the verb's `params/` struct,
  so a method change not mirrored in the schema desyncs the generated types.
- Keep the provider surface (the verbs/commands it contributes) in step with the
  README, which describes it from the actual `plugin:` entity.

## Landing

Load `/charly-internals:git-workflow` before any git/PR action; it owns the
landing mechanics. The authoritative rulebook is the umbrella `AGENTS.md` in
`opencharly/opencharly` and `charly/AGENTS.md` in the charly repo.
