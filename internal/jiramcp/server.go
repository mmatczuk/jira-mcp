// Package jiramcp implements the MCP server with JIRA tools.
package jiramcp

import (
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmatczuk/jira-mcp/internal/jira"
)

// NewServer creates a configured MCP server with all JIRA tools registered.
// The currentUser parameter is used to include the authenticated user's
// identity in the server instructions. The projects parameter lists all
// available JIRA projects so LLMs know valid project keys upfront.
func NewServer(client JiraClient, currentUser *jira.User, projects *jira.ProjectList) *mcp.Server {
	inst := serverInstructions
	if currentUser != nil {
		inst += fmt.Sprintf("\n\nCurrent user: %s (accountId: %s)", currentUser.DisplayName, currentUser.AccountID)
	}
	if projects != nil && len(*projects) > 0 {
		inst += "\n\nAvailable projects:\n" + formatProjectList(*projects)
	}

	s := mcp.NewServer(
		&mcp.Implementation{
			Name:    "jira-mcp",
			Version: "0.1.0",
		},
		&mcp.ServerOptions{
			Instructions: inst,
		},
	)

	h := &handlers{client: client}

	mcp.AddTool(s, readTool, h.handleRead)
	mcp.AddTool(s, writeTool, h.handleWrite)
	mcp.AddTool(s, schemaTool, h.handleSchema)

	return s
}

const serverInstructions = `Jira MCP Server — interact with JIRA Cloud via three tools:

- jira_read: Fetch issues by key, search by JQL, or list resources (projects, boards, sprints, sprint issues).
- jira_write: Create, update, delete, transition issues; add/edit comments; move issues to sprints. Supports batch (array of items). Always has dry_run option.
- jira_schema: Discover fields, transitions, field options — metadata needed to construct valid jira_write payloads.

Workflow tips:
1. Use jira_schema to discover available fields and transitions before writing.
2. Use jira_read with JQL for flexible queries.
3. All jira_write actions support dry_run=true to preview changes without applying them.
4. Descriptions and comments accept Markdown — they are auto-converted to Atlassian Document Format.`

func formatProjectList(projects jira.ProjectList) string {
	var b strings.Builder
	for _, p := range projects {
		fmt.Fprintf(&b, "- %s — %s\n", p.Key, p.Name)
	}
	return b.String()
}
