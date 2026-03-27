# Jira MCP

Give your AI agent full Jira access with just 4 tools.

Most Jira MCPs dump their entire API surface into the model's context. The agent wastes tokens picking between `jira_get_issue`, `jira_fetch_issue`, `jira_issue_get`... and still gets it wrong.

jira-mcp gives the model exactly what it needs:

| Tool | What it does |
|---|---|
| `jira_read` | Fetch issues by key, search by JQL, list projects/boards/sprints |
| `jira_write` | Create, update, delete, transition, comment — accepts Markdown, supports `dry_run` |
| `jira_schema` | Discover fields, transitions, and allowed values |
| `jira_user_search` | Find users by name or email, get account IDs for assignment |

Four tools that compose naturally: schema to discover, read to find, write to change, user search to resolve people. Less surface area means fewer wrong picks, fewer redundant calls, more context for your actual work.

**Your credentials stay on your machine.** jira-mcp runs as a local process over stdio — no server, no proxy, nothing between your agent and Atlassian.

**Smart by default.** jira-mcp hides Jira's complexity so your agent doesn't have to learn it:

- **User search** — `jira_user_search` resolves names and emails to account IDs in one call. No more guessing JQL syntax for assignee lookups.
- **Required field validation** — before creating an issue, jira-mcp checks the project's required fields and returns missing ones by name with allowed values. Your agent gets it right on the first try instead of decoding opaque `customfield_10104` errors.
- **Issue type validation** — if the issue type doesn't exist in the target project, the error lists available types immediately.

## Compared to [mcp-atlassian](https://github.com/sooperset/mcp-atlassian)

mcp-atlassian is a full Atlassian suite — 72 tools, Confluence, OAuth, SSE transport. That's powerful, and it's the right pick if you need all of it.

jira-mcp does one thing: Jira. And it does it with as little friction as possible.

**Why that matters for your agent:**
- **4 tools, not 72** — less context burned, sharper focus, fewer hallucinated tool calls
- **Zero runtime dependencies** — single Go binary, no Python, no venv, no pip
- **Works out of the box** — API token auth, stdio transport, ship it

Use mcp-atlassian if you need Confluence, OAuth, or SSE. Use jira-mcp if you want Jira to just work.

## Compared to [acli](https://developer.atlassian.com/cloud/acli/guides/introduction/)

acli is great for humans typing commands in a terminal. jira-mcp is built for AI agents — and that difference matters.

When you give an AI shell access, it can do anything: delete users, change org settings, trigger [Rovodev](https://www.atlassian.com/software/rovo/dev). jira-mcp limits the blast radius to Jira. Structured tool calls instead of shell execution means no injection risk, no accidental admin actions, and a model that stays in its lane.

**jira-mcp gives your AI agent:**
- **Safety by default** — no shell injection risk, Jira-only blast radius
- **Native Markdown** — write comments and descriptions in Markdown, it converts automatically
- **Built-in dry run** — preview every write before it happens
- **Lean context** — 4 tools vs. the full CLI surface; your agent stays focused
- **One-line read-only mode** — just instruct the model to use `jira_read` only, no extra tokens needed

Use acli when a human is at the keyboard or when you need Admin/Rovodev operations. Use jira-mcp when an AI agent is driving.

## Quick start

**Prerequisites:** A JIRA Cloud account with an API token.

### 1. Get an API token

1. Go to [API token management](https://id.atlassian.com/manage-profile/security/api-tokens)
2. Click "Create API token"
3. Give it a label, click "Create"
4. Copy the token immediately — you can't see it again

### 2. Install and add to Claude Code

Pick one path:

**Homebrew**

```bash
brew tap mmatczuk/jira-mcp https://github.com/mmatczuk/jira-mcp
brew install jira-mcp
```

```bash
claude mcp add-json jira '{
  "command": "jira-mcp",
  "env": {
    "JIRA_URL": "https://yourcompany.atlassian.net",
    "JIRA_EMAIL": "you@company.com",
    "JIRA_API_TOKEN": "your-api-token"
  }
}'
```

**Docker**

No install needed. The `-e VAR` flags (without a value) forward each variable from `env` into the container:

```bash
claude mcp add-json jira '{
  "command": "docker",
  "args": [
    "run", "-i", "--rm",
    "-e", "JIRA_URL",
    "-e", "JIRA_EMAIL",
    "-e", "JIRA_API_TOKEN",
    "mmatczuk/jira-mcp"
  ],
  "env": {
    "JIRA_URL": "https://yourcompany.atlassian.net",
    "JIRA_EMAIL": "you@company.com",
    "JIRA_API_TOKEN": "your-api-token"
  }
}'
```

**Binary**

Download the binary for your platform from the [releases page](https://github.com/mmatczuk/jira-mcp/releases) and put it on your `PATH`, then:

```bash
claude mcp add-json jira '{
  "command": "jira-mcp",
  "env": {
    "JIRA_URL": "https://yourcompany.atlassian.net",
    "JIRA_EMAIL": "you@company.com",
    "JIRA_API_TOKEN": "your-api-token"
  }
}'
```

### 3. Verify it works

First, confirm Claude Code picked up the server:

```bash
claude mcp list
```

You should see:

```
Checking MCP server health...

jira: jira-mcp - ✓ Connected
```

Then open a Claude Code session and ask: *"List my Jira projects"*:

```
❯ List my Jira projects

⏺ jira - jira_read (MCP)(resource: "projects")
  ⎿  Found 3 project(s)

⏺ 3 projects:

  - ACME — Acme Corp
  - PLAT — Platform
  - OPS — Operations
```

If you see your projects, you're set. If not, check the server logs for errors and verify your credentials.

### Other MCP clients

Use the same binary and env vars. The server speaks standard MCP over stdio.

## License

MIT
