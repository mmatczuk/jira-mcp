package jiramcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmatczuk/jira-mcp/internal/mdconv"
)

type WriteItem struct {
	Key         string   `json:"key,omitempty" jsonschema:"Issue key (e.g. PROJ-1). Required for update/delete/transition/comment/edit_comment."`
	Project     string   `json:"project,omitempty" jsonschema:"Project key for create action."`
	Summary     string   `json:"summary,omitempty" jsonschema:"Issue summary/title."`
	IssueType   string   `json:"issue_type,omitempty" jsonschema:"Issue type name (e.g. Bug, Task, Story, Epic)."`
	Priority    string   `json:"priority,omitempty" jsonschema:"Priority name (e.g. High, Medium, Low)."`
	Assignee    string   `json:"assignee,omitempty" jsonschema:"Assignee account ID."`
	Description string   `json:"description,omitempty" jsonschema:"Issue description in Markdown. Auto-converted to Atlassian Document Format."`
	Labels      []string `json:"labels,omitempty" jsonschema:"Issue labels."`

	TransitionID string `json:"transition_id,omitempty" jsonschema:"Transition ID. Use jira_schema resource=transitions issue_key=X to find valid IDs."`

	Comment   string `json:"comment,omitempty" jsonschema:"Comment body in Markdown. Used for comment/edit_comment and optionally with transition."`
	CommentID string `json:"comment_id,omitempty" jsonschema:"Comment ID for edit_comment action."`

	SprintID int `json:"sprint_id,omitempty" jsonschema:"Sprint ID for move_to_sprint action."`

	FieldsJSON string `json:"fields_json,omitempty" jsonschema:"Raw JSON object merged into issue fields. Escape hatch for custom fields."`
}

type WriteArgs struct {
	Action string      `json:"action" jsonschema:"Action: create, update, delete, transition, comment, edit_comment, move_to_sprint."`
	Items  []WriteItem `json:"items" jsonschema:"Array of items to process. Even a single operation should be wrapped in an array."`
	DryRun bool        `json:"dry_run,omitempty" jsonschema:"Preview changes without applying them. Default false."`
}

var writeTool = &mcp.Tool{
	Name: "jira_write",
	Description: `Modify JIRA data. Batch-first: pass an array of items even for single operations.

Actions:
- create: Create issues. Each item needs: project, summary, issue_type. Optional: description (Markdown), assignee, priority, labels, fields_json.
- update: Update issues. Each item needs: key. Provide fields to change: summary, description, assignee, priority, labels, fields_json.
- delete: Delete issues. Each item needs: key.
- transition: Transition issues. Each item needs: key, transition_id. Optional: comment (Markdown). Hint: Use jira_schema resource=transitions to find IDs.
- comment: Add comments. Each item needs: key, comment (Markdown).
- edit_comment: Edit comments. Each item needs: key, comment_id, comment (Markdown).
- move_to_sprint: Move issues to a sprint. Each item needs: key, sprint_id.

Before creating issues:
1. Pick the correct project key from the available projects listed.
2. Use jira_schema resource=fields to discover required and custom fields for the target project/issue type.
3. Use jira_schema resource=field_options field_id=<id> to find allowed values for select/multiselect custom fields.
4. Pass custom/required fields via fields_json (e.g. fields_json="{\"customfield_10104\": {\"value\": \"Production\"}}").

All actions support dry_run=true to preview without executing. Descriptions and comments accept Markdown.`,
}

func (h *handlers) handleWrite(ctx context.Context, _ *mcp.CallToolRequest, args WriteArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Items) == 0 {
		return textResult("items array is empty. Provide at least one item.", true), nil, nil
	}

	if args.Action == "move_to_sprint" {
		return h.handleMoveToSprint(ctx, args), nil, nil
	}

	var results []string

	for i, item := range args.Items {
		prefix := fmt.Sprintf("[%d] ", i+1)
		var msg string
		var err error

		switch args.Action {
		case "create":
			msg, err = h.writeCreate(ctx, item, args.DryRun)
		case "update":
			msg, err = h.writeUpdate(ctx, item, args.DryRun)
		case "delete":
			msg, err = h.writeDelete(ctx, item, args.DryRun)
		case "transition":
			msg, err = h.writeTransition(ctx, item, args.DryRun)
		case "comment":
			msg, err = h.writeComment(ctx, item, args.DryRun)
		case "edit_comment":
			msg, err = h.writeEditComment(ctx, item, args.DryRun)
		default:
			return textResult(fmt.Sprintf("Unknown action %q. Valid: create, update, delete, transition, comment, edit_comment, move_to_sprint.", args.Action), true), nil, nil
		}

		if err != nil {
			results = append(results, prefix+"ERROR: "+err.Error())
		} else {
			results = append(results, prefix+msg)
		}
	}

	label := "Results"
	if args.DryRun {
		label = "DRY RUN — no changes made"
	}

	out := fmt.Sprintf("%s (%d item(s), action=%s):\n\n%s", label, len(args.Items), args.Action, strings.Join(results, "\n\n"))

	return textResult(out, false), nil, nil
}

// handleMoveToSprint groups items by sprint_id and calls MoveIssuesToSprint once per sprint.
func (h *handlers) handleMoveToSprint(ctx context.Context, args WriteArgs) *mcp.CallToolResult {
	// Validate all items first.
	for i, item := range args.Items {
		if item.Key == "" || item.SprintID == 0 {
			return textResult(fmt.Sprintf("[%d] move_to_sprint requires key and sprint_id. Hint: Use jira_read resource=sprints board_id=<id> to find sprint IDs", i+1), true)
		}
	}

	// Group keys by sprint_id, preserving insertion order.
	type sprintGroup struct {
		sprintID int
		keys     []string
		indices  []int
	}
	order := []int{}
	groups := map[int]*sprintGroup{}
	for i, item := range args.Items {
		if _, ok := groups[item.SprintID]; !ok {
			groups[item.SprintID] = &sprintGroup{sprintID: item.SprintID}
			order = append(order, item.SprintID)
		}
		g := groups[item.SprintID]
		g.keys = append(g.keys, item.Key)
		g.indices = append(g.indices, i+1)
	}

	label := "Results"
	if args.DryRun {
		label = "DRY RUN — no changes made"
	}

	var results []string
	for _, sprintID := range order {
		g := groups[sprintID]
		prefix := fmt.Sprintf("%v", g.indices)
		if args.DryRun {
			results = append(results, fmt.Sprintf("%s Would move %v to sprint %d.", prefix, g.keys, sprintID))
			continue
		}
		if err := h.client.MoveIssuesToSprint(ctx, sprintID, g.keys); err != nil {
			results = append(results, fmt.Sprintf("%s ERROR: failed to move %v to sprint %d: %v", prefix, g.keys, sprintID, err))
		} else {
			results = append(results, fmt.Sprintf("%s Moved %v to sprint %d.", prefix, g.keys, sprintID))
		}
	}

	out := fmt.Sprintf("%s (%d item(s), action=move_to_sprint):\n\n%s", label, len(args.Items), strings.Join(results, "\n\n"))
	return textResult(out, false)
}

// buildIssuePayload constructs a v3 API payload with ADF description.
func buildIssuePayload(item WriteItem) (map[string]any, error) {
	fields := map[string]any{}

	if item.Project != "" {
		fields["project"] = map[string]any{"key": item.Project}
	}
	if item.Summary != "" {
		fields["summary"] = item.Summary
	}
	if item.IssueType != "" {
		fields["issuetype"] = map[string]any{"name": item.IssueType}
	}
	if item.Priority != "" {
		fields["priority"] = map[string]any{"name": item.Priority}
	}
	if item.Assignee != "" {
		fields["assignee"] = map[string]any{"accountId": item.Assignee}
	}
	if item.Labels != nil {
		fields["labels"] = item.Labels
	}
	if item.Description != "" {
		adf := mdconv.ToADF(item.Description)
		if adf != nil {
			fields["description"] = adf
		}
	}
	if item.FieldsJSON != "" {
		var extra map[string]any
		if err := json.Unmarshal([]byte(item.FieldsJSON), &extra); err != nil {
			return nil, fmt.Errorf("invalid fields_json: %w. Hint: Provide a valid JSON object like {\"customfield_10001\": \"value\"}", err)
		}
		for k, v := range extra {
			fields[k] = v
		}
	}

	return map[string]any{"fields": fields}, nil
}

// buildCommentBody converts markdown to an ADF body or falls back to plain text ADF.
func buildCommentBody(markdown string) any {
	adf := mdconv.ToADF(markdown)
	if adf != nil {
		return adf
	}
	// Fallback: wrap plain text in minimal ADF.
	return map[string]any{
		"version": 1,
		"type":    "doc",
		"content": []any{
			map[string]any{
				"type": "paragraph",
				"content": []any{
					map[string]any{"type": "text", "text": markdown},
				},
			},
		},
	}
}

func (h *handlers) writeCreate(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.Project == "" || item.Summary == "" || item.IssueType == "" {
		return "", fmt.Errorf("create requires project, summary, and issue_type. Got project=%q summary=%q issue_type=%q", item.Project, item.Summary, item.IssueType)
	}

	payload, err := buildIssuePayload(item)
	if err != nil {
		return "", err
	}

	if dryRun {
		data, _ := json.MarshalIndent(payload, "", "  ")
		return fmt.Sprintf("Would create issue in project %s with type %s:\n%s\nNote: Required custom fields are not validated locally. Use jira_schema resource=fields to check required fields before submitting.", item.Project, item.IssueType, string(data)), nil
	}

	key, _, err := h.client.CreateIssueV3(ctx, payload)
	if err != nil {
		return "", fmt.Errorf("failed to create issue in %s: %w; %s", item.Project, err, createErrorHints(err))
	}

	return fmt.Sprintf("Created %s — %s (project=%s, type=%s). Hint: Use jira_read keys=[\"%s\"] to see the full issue.", key, item.Summary, item.Project, item.IssueType, key), nil
}

func (h *handlers) writeUpdate(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.Key == "" {
		return "", fmt.Errorf("update requires key")
	}

	payload, err := buildIssuePayload(item)
	if err != nil {
		return "", err
	}

	if dryRun {
		data, _ := json.MarshalIndent(payload, "", "  ")
		return fmt.Sprintf("Would update %s with:\n%s", item.Key, string(data)), nil
	}

	if err := h.client.UpdateIssueV3(ctx, item.Key, payload); err != nil {
		return "", fmt.Errorf("failed to update %s: %w", item.Key, err)
	}

	return fmt.Sprintf("Updated %s successfully.", item.Key), nil
}

func (h *handlers) writeDelete(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.Key == "" {
		return "", fmt.Errorf("delete requires key")
	}

	if dryRun {
		return fmt.Sprintf("Would delete %s. This action is irreversible.", item.Key), nil
	}

	if err := h.client.DeleteIssue(ctx, item.Key); err != nil {
		return "", fmt.Errorf("failed to delete %s: %w", item.Key, err)
	}

	return fmt.Sprintf("Deleted %s.", item.Key), nil
}

func (h *handlers) writeTransition(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.Key == "" || item.TransitionID == "" {
		return "", fmt.Errorf("transition requires key and transition_id. Hint: Use jira_schema resource=transitions issue_key=%s to find valid transition IDs", item.Key)
	}

	if dryRun {
		msg := fmt.Sprintf("Would transition %s using transition_id=%s.", item.Key, item.TransitionID)
		if item.Comment != "" {
			msg += " Would also add a comment."
		}
		return msg, nil
	}

	if err := h.client.DoTransition(ctx, item.Key, item.TransitionID); err != nil {
		return "", fmt.Errorf("failed to transition %s: %w. Hint: Use jira_schema resource=transitions issue_key=%s to see available transitions", item.Key, err, item.Key)
	}

	msg := fmt.Sprintf("Transitioned %s with transition_id=%s.", item.Key, item.TransitionID)

	if item.Comment != "" {
		body := buildCommentBody(item.Comment)
		if _, err := h.client.AddComment(ctx, item.Key, body); err != nil {
			msg += fmt.Sprintf(" Warning: transition succeeded but comment failed: %v", err)
		} else {
			msg += " Comment added."
		}
	}

	return msg, nil
}

func (h *handlers) writeComment(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.Key == "" || item.Comment == "" {
		return "", fmt.Errorf("comment requires key and comment")
	}

	if dryRun {
		return fmt.Sprintf("Would add comment to %s:\n%s", item.Key, item.Comment), nil
	}

	body := buildCommentBody(item.Comment)
	commentID, err := h.client.AddComment(ctx, item.Key, body)
	if err != nil {
		return "", fmt.Errorf("failed to add comment to %s: %w", item.Key, err)
	}

	return fmt.Sprintf("Added comment to %s (comment_id=%s).", item.Key, commentID), nil
}

func (h *handlers) writeEditComment(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.Key == "" || item.CommentID == "" || item.Comment == "" {
		return "", fmt.Errorf("edit_comment requires key, comment_id, and comment")
	}

	if dryRun {
		return fmt.Sprintf("Would edit comment %s on %s:\n%s", item.CommentID, item.Key, item.Comment), nil
	}

	body := buildCommentBody(item.Comment)
	if err := h.client.UpdateComment(ctx, item.Key, item.CommentID, body); err != nil {
		return "", fmt.Errorf("failed to edit comment %s on %s: %w", item.CommentID, item.Key, err)
	}

	return fmt.Sprintf("Updated comment %s on %s.", item.CommentID, item.Key), nil
}

var customFieldRe = regexp.MustCompile(`customfield_\d+`)

// createErrorHints parses a Jira create/update error and returns actionable
// hints about how to resolve field-level validation failures.
func createErrorHints(err error) string {
	msg := err.Error()

	fieldIDs := customFieldRe.FindAllString(msg, -1)
	if len(fieldIDs) == 0 {
		if strings.Contains(msg, "project") {
			return "Hint: Use the correct project key."
		}
		return "Hint: Check project key and issue type name are valid. Use jira_schema resource=fields to see available fields."
	}

	seen := map[string]bool{}
	var hints []string
	for _, id := range fieldIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		hints = append(hints, fmt.Sprintf(
			"Use jira_schema resource=field_options field_id=%s to find valid values, then pass via fields_json.", id,
		))
	}

	return "Hint: Required custom fields are missing. " + strings.Join(hints, " ")
}
