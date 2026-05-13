package jiramcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmatczuk/jira-mcp/internal/jira"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ReadArgs struct {
	Keys []string `json:"keys,omitempty" jsonschema:"Issue keys to fetch (e.g. PROJ-1). Mutually exclusive with jql, resource, attachment_id."`

	JQL string `json:"jql,omitempty" jsonschema:"JQL search query. Mutually exclusive with keys, resource, attachment_id."`

	Resource string `json:"resource,omitempty" jsonschema:"Resource to list: projects, boards, sprints, sprint_issues. Mutually exclusive with keys, jql, attachment_id."`

	AttachmentID string `json:"attachment_id,omitempty" jsonschema:"Attachment id to fetch as text. Text mime types only; 5 MB cap. Mutually exclusive with keys, jql, resource."`

	BoardID     int    `json:"board_id,omitempty" jsonschema:"Board ID. Required for resource=sprints."`
	SprintID    int    `json:"sprint_id,omitempty" jsonschema:"Sprint ID. Required for resource=sprint_issues."`
	ProjectKey  string `json:"project_key,omitempty" jsonschema:"Filter boards by project key."`
	BoardName   string `json:"board_name,omitempty" jsonschema:"Filter boards by name substring."`
	BoardType   string `json:"board_type,omitempty" jsonschema:"Filter boards by type: scrum, kanban."`
	SprintState string `json:"sprint_state,omitempty" jsonschema:"Filter sprints by state: active, closed, future."`

	Fields        string `json:"fields,omitempty" jsonschema:"Comma-separated field names to return (default: all)."`
	Expand        string `json:"expand,omitempty" jsonschema:"Comma-separated expansions (e.g. renderedFields transitions changelog)."`
	Limit         int    `json:"limit,omitempty" jsonschema:"Max results to return. Default 100."`
	StartAt       int    `json:"start_at,omitempty" jsonschema:"Pagination offset for resource listings (boards, sprints). Not used for JQL search."`
	NextPageToken string `json:"next_page_token,omitempty" jsonschema:"Token for fetching the next page of JQL search results. Returned in previous search response."`
}

// readMode names the four mutually exclusive top-level branches of
// jira_read. selectReadMode resolves it from a ReadArgs and reports the
// "exactly one of ..." error when zero or many are set.
type readMode int

const (
	readModeNone readMode = iota
	readModeKeys
	readModeJQL
	readModeResource
	readModeAttachment
)

// selectReadMode returns the picked mode and, on validation failure, a
// user-facing message describing the problem. A non-empty message means the
// caller should surface it as an error response without dispatching.
func selectReadMode(args ReadArgs) (readMode, string) {
	var picked readMode
	count := 0
	if len(args.Keys) > 0 {
		picked = readModeKeys
		count++
	}
	if args.JQL != "" {
		picked = readModeJQL
		count++
	}
	if args.Resource != "" {
		picked = readModeResource
		count++
	}
	if args.AttachmentID != "" {
		picked = readModeAttachment
		count++
	}
	switch count {
	case 0:
		return readModeNone, "Provide exactly one of: keys, jql, resource, or attachment_id. Example: {\"keys\": [\"PROJ-1\"]} or {\"jql\": \"project = PROJ\"} or {\"resource\": \"projects\"} or {\"attachment_id\": \"10100\"}"
	case 1:
		return picked, ""
	default:
		return readModeNone, "Provide exactly one of: keys, jql, resource, or attachment_id — not multiple."
	}
}

var readTool = &mcp.Tool{
	Name: "jira_read",
	Description: `Fetch JIRA data. Four modes (provide exactly one):

1. keys — Fetch issues by key. Pass one or more issue keys like ["PROJ-1", "PROJ-2"].
2. jql — Search issues with JQL query. Supports all JIRA JQL syntax.
3. resource — List a resource type: "projects", "boards", "sprints" (needs board_id), "sprint_issues" (needs sprint_id).
4. attachment_id — Fetch one attachment's content as text. Text mime types only (text/*, application/json/xml/yaml). 5 MB cap.

Common options: fields (comma-separated), expand, limit (default 100), start_at.
When an issue has attachments, their metadata is returned under fields.attachment. Use mode=attachment_id to fetch a body.
Hint: Use jira_schema resource=transitions with an issue_key to find valid transition IDs before transitioning.

Descriptions and comments for older issues are returned in Jira wiki-markup, not Markdown. Do not feed a description/comment string from jira_read straight back into jira_write — the default write path expects Markdown and will reject recognised wiki-markup tokens. Either convert to Markdown, or set description_format="wiki" / comment_format="wiki" on the write to preserve wiki-markup input.`,
}

func (h *handlers) handleRead(ctx context.Context, _ *mcp.CallToolRequest, args ReadArgs) (*mcp.CallToolResult, any, error) {
	if args.Limit == 0 {
		args.Limit = 100
	}

	mode, problem := selectReadMode(args)
	if problem != "" {
		return textResult(problem, true), nil, nil
	}

	switch mode {
	case readModeKeys:
		return h.readByKeys(ctx, args), nil, nil
	case readModeJQL:
		return h.readByJQL(ctx, args), nil, nil
	case readModeResource:
		return h.readResource(ctx, args), nil, nil
	case readModeAttachment:
		return h.readAttachment(ctx, args.AttachmentID), nil, nil
	default:
		return textResult("Internal error: unhandled read mode", true), nil, nil
	}
}

// readAttachment fetches one attachment's body and returns it as text after
// validating the (declared mime, body) pair against the v1 text policy.
// Single-attachment per call — multi-fetch is by design omitted to keep the
// blast radius small (5 MB per call rather than 5 MB × N).
func (h *handlers) readAttachment(ctx context.Context, id string) *mcp.CallToolResult {
	meta, err := h.client.GetAttachmentMeta(ctx, id)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to read attachment %s: %v", id, err), true)
	}
	if meta == nil {
		return textResult(fmt.Sprintf("Failed to read attachment %s: empty metadata", id), true)
	}

	// Fast-path: reject on declared mime so we don't waste a download call.
	if meta.MimeType != "" && !isAllowedTextMime(meta.MimeType) {
		return textResult(fmt.Sprintf(
			"Attachment %s (%s) has mime type %q. text attachments only in v1 — fetch the binary from Jira directly.",
			id, meta.Filename, meta.MimeType,
		), true)
	}

	body, err := h.client.GetAttachmentBody(ctx, id, attachmentMaxBytes)
	if err != nil {
		if errors.Is(err, jira.ErrAttachmentTooLarge) {
			return textResult(fmt.Sprintf(
				"Attachment %s (%s, %d bytes) exceeds the %d-byte cap.",
				id, meta.Filename, meta.Size, attachmentMaxBytes,
			), true)
		}
		return textResult(fmt.Sprintf("Failed to download attachment %s: %v", id, err), true)
	}

	if err := validateTextAttachment(meta.MimeType, body); err != nil {
		return textResult(fmt.Sprintf(
			"Attachment %s (%s) rejected: %v",
			id, meta.Filename, err,
		), true)
	}

	header := fmt.Sprintf("Attachment %s: filename=%s mime=%s size=%d\n---\n", id, meta.Filename, meta.MimeType, meta.Size)
	return textResult(header+string(body), false)
}

func (h *handlers) readByKeys(ctx context.Context, args ReadArgs) *mcp.CallToolResult {
	// For a single key use GetIssue (supports Expand, richer fields).
	// For 2+ keys use a JQL search to reduce API calls.
	if len(args.Keys) == 1 {
		opts := &jira.GetQueryOptions{}
		if args.Fields != "" {
			opts.Fields = args.Fields
		}
		if args.Expand != "" {
			opts.Expand = args.Expand
		}
		issue, err := h.client.GetIssue(ctx, args.Keys[0], opts)
		if err != nil {
			return formatReadResult("Fetched 0 issue(s)", nil, []string{fmt.Sprintf("%s: %v", args.Keys[0], err)})
		}
		return formatReadResult("Fetched 1 issue(s)", []map[string]any{issueToMap(issue)}, nil)
	}

	// Build issueKey in (...) JQL for multi-key fetch.
	quoted := make([]string, len(args.Keys))
	for i, k := range args.Keys {
		quoted[i] = fmt.Sprintf("%q", k)
	}
	jql := fmt.Sprintf("issueKey in (%s)", strings.Join(quoted, ", "))

	opts := &jira.SearchOptionsV3{MaxResults: len(args.Keys)}
	if args.Fields != "" {
		for _, f := range strings.Split(args.Fields, ",") {
			opts.Fields = append(opts.Fields, strings.TrimSpace(f))
		}
	}
	if args.Expand != "" {
		opts.Expand = args.Expand
	}

	sr, err := h.client.SearchIssues(ctx, jql, opts)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to fetch issues %v: %v", args.Keys, err), true)
	}

	var results []map[string]any
	for i := range sr.Issues {
		results = append(results, issueToMap(&sr.Issues[i]))
	}
	return formatReadResult(fmt.Sprintf("Fetched %d issue(s)", len(results)), results, nil)
}

func (h *handlers) readByJQL(ctx context.Context, args ReadArgs) *mcp.CallToolResult {
	opts := &jira.SearchOptionsV3{
		MaxResults:    args.Limit,
		NextPageToken: args.NextPageToken,
	}
	if args.Fields != "" {
		for _, f := range strings.Split(args.Fields, ",") {
			opts.Fields = append(opts.Fields, strings.TrimSpace(f))
		}
	}
	if args.Expand != "" {
		opts.Expand = args.Expand
	}

	sr, err := h.client.SearchIssues(ctx, args.JQL, opts)
	if err != nil {
		return textResult(fmt.Sprintf("JQL search failed: %v\nHint: Check your JQL syntax. Use jira_schema resource=fields to see available field names.", err), true)
	}

	var results []map[string]any
	for i := range sr.Issues {
		results = append(results, issueToMap(&sr.Issues[i]))
	}

	summary := fmt.Sprintf("Found %d issue(s) (total %d). JQL: %s", len(results), sr.Total, args.JQL)
	if sr.NextPageToken != "" {
		summary += fmt.Sprintf("\nHint: More results available. Use next_page_token=%q to get the next page.", sr.NextPageToken)
	}

	return formatReadResult(summary, results, nil)
}

func (h *handlers) readResource(ctx context.Context, args ReadArgs) *mcp.CallToolResult {
	switch args.Resource {
	case "projects":
		return h.readProjects(ctx)
	case "boards":
		return h.readBoards(ctx, args)
	case "sprints":
		return h.readSprints(ctx, args)
	case "sprint_issues":
		return h.readSprintIssues(ctx, args)
	default:
		return textResult(fmt.Sprintf("Unknown resource %q. Valid: projects, boards, sprints, sprint_issues.", args.Resource), true)
	}
}

func (h *handlers) readProjects(ctx context.Context) *mcp.CallToolResult {
	projects, err := h.client.GetAllProjects(ctx)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to list projects: %v", err), true)
	}

	var results []map[string]any
	if projects != nil {
		for _, p := range *projects {
			results = append(results, map[string]any{
				"key":  p.Key,
				"name": p.Name,
				"id":   p.ID,
			})
		}
	}

	return formatReadResult(fmt.Sprintf("Found %d project(s)", len(results)), results, nil)
}

func (h *handlers) readBoards(ctx context.Context, args ReadArgs) *mcp.CallToolResult {
	opts := &jira.BoardListOptions{
		SearchOptions: jira.SearchOptions{
			MaxResults: args.Limit,
			StartAt:    args.StartAt,
		},
	}
	if args.ProjectKey != "" {
		opts.ProjectKeyOrID = args.ProjectKey
	}
	if args.BoardName != "" {
		opts.Name = args.BoardName
	}
	if args.BoardType != "" {
		opts.BoardType = args.BoardType
	}

	boards, isLast, err := h.client.GetAllBoards(ctx, opts)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to list boards: %v", err), true)
	}

	var results []map[string]any
	for _, b := range boards {
		results = append(results, map[string]any{
			"id":   b.ID,
			"name": b.Name,
			"type": b.Type,
		})
	}

	summary := fmt.Sprintf("Found %d board(s)", len(results))
	if !isLast {
		summary += fmt.Sprintf("\nHint: More boards available. Use start_at=%d to get the next page.", args.StartAt+args.Limit)
	}

	return formatReadResult(summary, results, nil)
}

func (h *handlers) readSprints(ctx context.Context, args ReadArgs) *mcp.CallToolResult {
	if args.BoardID == 0 {
		return textResult("board_id is required for resource=sprints. Hint: Use jira_read resource=boards to find board IDs.", true)
	}

	opts := &jira.GetAllSprintsOptions{
		SearchOptions: jira.SearchOptions{
			MaxResults: args.Limit,
			StartAt:    args.StartAt,
		},
	}
	if args.SprintState != "" {
		opts.State = args.SprintState
	}

	sprints, isLast, err := h.client.GetAllSprints(ctx, args.BoardID, opts)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to list sprints for board %d: %v", args.BoardID, err), true)
	}

	var results []map[string]any
	for _, s := range sprints {
		results = append(results, map[string]any{
			"id":    s.ID,
			"name":  s.Name,
			"state": s.State,
		})
	}

	summary := fmt.Sprintf("Found %d sprint(s) for board %d", len(results), args.BoardID)
	if !isLast {
		summary += fmt.Sprintf("\nHint: More sprints available. Use start_at=%d to get the next page.", args.StartAt+args.Limit)
	}

	return formatReadResult(summary, results, nil)
}

func (h *handlers) readSprintIssues(ctx context.Context, args ReadArgs) *mcp.CallToolResult {
	if args.SprintID == 0 {
		return textResult("sprint_id is required for resource=sprint_issues. Hint: Use jira_read resource=sprints board_id=<id> to find sprint IDs.", true)
	}

	issues, err := h.client.GetSprintIssues(ctx, args.SprintID)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to get issues for sprint %d: %v", args.SprintID, err), true)
	}

	var results []map[string]any
	for i := range issues {
		results = append(results, issueToMap(&issues[i]))
	}

	summary := fmt.Sprintf("Found %d issue(s) in sprint %d", len(results), args.SprintID)
	summary += "\nNote: Sprint issues endpoint returns a single page. For large sprints, use jira_read with jql=\"sprint = <sprint_id>\" for full pagination."

	return formatReadResult(summary, results, nil)
}

func userToMap(u *jira.User) map[string]any {
	return map[string]any{
		"displayName": u.DisplayName,
		"accountId":   u.AccountID,
	}
}

func issueToMap(issue *jira.Issue) map[string]any {
	m := map[string]any{
		"key":  issue.Key,
		"id":   issue.ID,
		"self": issue.Self,
	}

	if issue.Fields != nil {
		fields := map[string]any{
			"summary": issue.Fields.Summary,
		}
		if issue.Fields.Status != nil {
			fields["status"] = issue.Fields.Status.Name
		}
		if issue.Fields.Type.Name != "" {
			fields["type"] = issue.Fields.Type.Name
		}
		if issue.Fields.Assignee != nil {
			fields["assignee"] = userToMap(issue.Fields.Assignee)
		}
		if issue.Fields.Priority != nil {
			fields["priority"] = issue.Fields.Priority.Name
		}
		if issue.Fields.Description != "" {
			fields["description"] = issue.Fields.Description
		}
		if issue.Fields.Labels != nil {
			fields["labels"] = issue.Fields.Labels
		}
		if !time.Time(issue.Fields.Created).IsZero() {
			fields["created"] = time.Time(issue.Fields.Created).Format(time.RFC3339)
		}
		if !time.Time(issue.Fields.Updated).IsZero() {
			fields["updated"] = time.Time(issue.Fields.Updated).Format(time.RFC3339)
		}
		if len(issue.Fields.Attachments) > 0 {
			atts := make([]map[string]any, 0, len(issue.Fields.Attachments))
			for _, a := range issue.Fields.Attachments {
				if a == nil {
					continue
				}
				atts = append(atts, attachmentToMap(a))
			}
			if len(atts) > 0 {
				fields["attachment"] = atts
			}
		}
		m["fields"] = fields
	}

	return m
}

// attachmentToMap renders a single Jira attachment as the response shape
// callers see. Created is emitted as RFC3339 when parseable; otherwise the
// raw Jira-issued string passes through.
func attachmentToMap(a *jira.Attachment) map[string]any {
	out := map[string]any{
		"id":        a.ID,
		"filename":  a.Filename,
		"size":      a.Size,
		"mime_type": a.MimeType,
		"created":   formatJiraTimeString(a.Created),
	}
	if a.Author != nil {
		out["author"] = userToMap(a.Author)
	}
	return out
}

// formatJiraTimeString parses Jira's wire layout for timestamp strings and
// re-emits them as RFC3339 for consistency with the rest of issueToMap.
// Unparseable input is returned unchanged so we never drop information.
func formatJiraTimeString(s string) string {
	if s == "" {
		return s
	}
	t, err := time.Parse("2006-01-02T15:04:05.999-0700", s)
	if err != nil {
		return s
	}
	return t.Format(time.RFC3339)
}

func formatReadResult(summary string, results []map[string]any, errors []string) *mcp.CallToolResult {
	out := summary + "\n\n"

	if len(errors) > 0 {
		out += "Errors:\n"
		for _, e := range errors {
			out += "  - " + e + "\n"
		}
		out += "\n"
	}

	if len(results) > 0 {
		data, err := json.Marshal(results)
		if err != nil {
			out += fmt.Sprintf("Failed to serialize results: %v", err)
		} else {
			out += string(data)
		}
	}

	return textResult(out, false)
}

func textResult(msg string, isError bool) *mcp.CallToolResult {
	r := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
	if isError {
		r.IsError = true
	}
	return r
}
