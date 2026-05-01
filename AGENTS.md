# AGENTS.md

Guidance for coding agents working in this repository. Follows the [AGENTS.md](https://agents.md) convention — picked up by Claude Code, OpenAI Codex, Cursor, Aider, and others without further configuration.

## Project

Go CLI that exposes a Jira Cloud MCP server over stdio. Four tools only (`jira_read`, `jira_write`, `jira_schema`, `jira_user_search`) — this minimal surface is a deliberate design goal; do not add tools without checking README framing first.

## Common Commands

This project uses [`task`](https://taskfile.dev), not `make`.

- `task build` — compile to `bin/jira-mcp` (entrypoint `./cmd/jira-mcp`).
- `task test` — `go test ./...`.
- `task lint` — `golangci-lint run ./...`.
- `task fmt` — `golangci-lint fmt ./...` then `go mod tidy`.

Running a single test: `go test ./internal/jiramcp/ -run TestHandleWrite_Create -v`.

Running the server locally requires three env vars: `JIRA_URL`, `JIRA_EMAIL`, `JIRA_API_TOKEN`. It speaks MCP over stdio and will fail fast if any env var is missing.

## Architecture

Three internal packages with a one-way dependency: `cmd/jira-mcp` → `jiramcp` → `jira` / `mdconv`.

### `internal/jira` — thin Jira Cloud API wrapper

Wraps `github.com/andygrunwald/go-jira` and adds call-level retry on HTTP 429 with exponential backoff (`Config.MaxRetries`, `Config.BaseDelay`). Every method follows the pattern `c.retry(ctx, func() (*jira.Response, error) { ... })`. When adding new API calls, stick to this pattern — do not call `c.j` directly without going through `retry`.

### `internal/jiramcp` — MCP tool handlers

- `server.go` registers the four tools and composes server-level `Instructions` by appending the authenticated user and the full project list. The project list is fetched at startup so LLMs have valid project keys without round-trips.
- `client.go` defines the `JiraClient` interface — handlers depend on this interface, not on `*jira.Client`, which is what enables the extensive mock-based tests in `mock_client_test.go`. When adding a Jira method, add it to the interface and update the mock.
- `tool_*.go` — one file per tool. Arg structs use `jsonschema:"..."` struct tags; those strings become the tool schema the LLM sees, so treat them as user-facing documentation.
- `tool_write.go` uses `createMetaCache` to dedupe create-metadata lookups within a single batch call. Required-field validation is advisory only for issue creation (see commit `37ea309`) — do not reintroduce blocking behaviour without discussion.

### `internal/mdconv` — Markdown → ADF

Converts Markdown to Atlassian Document Format using `goldmark`. Called from `tool_write.go` for description and comment bodies. The `goldmark` parser is stateless and shared via a package-level var.

## Conventions

- Go 1.25. Table-driven tests with `testify`.
- Handler tests mock `JiraClient`; do not hit live Jira from tests.
- Tool descriptions and `jsonschema` struct tags are part of the product — they shape LLM behaviour. Edit them with the same care as user-facing docs.
- The README's positioning ("4 tools, not 72") is load-bearing. New tools need a strong justification.

## Release

Releases are driven by `.goreleaser.yaml` and the Homebrew tap in `Casks/`. `cmd/jira-mcp/main.go` exposes `-version` backed by `-ldflags`-injected `version`/`commit`/`date` vars.
