package jiramcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmatczuk/jira-mcp/internal/jira"
	"github.com/mmatczuk/jira-mcp/internal/mdconv"
)

// LinkItem is one entry in WriteItem.Links or WriteItem.Unlinks. Add
// requires Type + From + To. Remove requires LinkID OR Type + From + To.
// Comment is optional and only meaningful on add (Links). The link
// endpoint only accepts ADF, so comments here are markdown-only — for
// wiki-markup, use a separate comment action after the link is created.
type LinkItem struct {
	Type    string `json:"type,omitempty" jsonschema:"Link type name (e.g. Blocks, Relates, Duplicates). Use jira_schema resource=link_types to discover names and verb directions."`
	From    string `json:"from,omitempty" jsonschema:"Issue key on the active side of the link. For type=Blocks, 'from' is the issue that does the blocking."`
	To      string `json:"to,omitempty" jsonschema:"Issue key on the passive side of the link. For type=Blocks, 'to' is the issue that is blocked."`
	LinkID  string `json:"link_id,omitempty" jsonschema:"Existing link ID. Optional in unlinks for unambiguous removal — read from the issuelinks field of jira_read."`
	Comment string `json:"comment,omitempty" jsonschema:"Optional Markdown comment posted on the inward issue at link creation time. Wiki-markup is not supported here; for wiki, post a separate comment action after the link is created. Ignored on unlinks."`
}

type WriteItem struct {
	Key               string   `json:"key,omitempty" jsonschema:"Issue key (e.g. PROJ-1). Required for update/delete/transition/comment/edit_comment."`
	Project           string   `json:"project,omitempty" jsonschema:"Project key for create action."`
	Summary           string   `json:"summary,omitempty" jsonschema:"Issue summary/title."`
	IssueType         string   `json:"issue_type,omitempty" jsonschema:"Issue type name (e.g. Bug, Task, Story, Epic)."`
	Priority          string   `json:"priority,omitempty" jsonschema:"Priority name (e.g. High, Medium, Low)."`
	Assignee          string   `json:"assignee,omitempty" jsonschema:"Assignee account ID. Use jira_user_search to find account IDs by name or email."`
	Description       string   `json:"description,omitempty" jsonschema:"Issue description. Format controlled by description_format (default Markdown → ADF)."`
	DescriptionFormat string   `json:"description_format,omitempty" jsonschema:"How to interpret description. markdown (default): converted to ADF and sent via v3. wiki: sent verbatim as legacy Jira wiki-markup via v2. Allowed: markdown, wiki."`
	Labels            []string `json:"labels,omitempty" jsonschema:"Issue labels."`
	ParentKey         string   `json:"parent_key,omitempty" jsonschema:"Parent issue key (e.g. an Epic for a Story sub-task). Settable on create and update. Jira enforces type compatibility — typically only Epics can be parents in a given configuration. Pass an empty string to leave the parent unchanged on update."`

	TransitionID string `json:"transition_id,omitempty" jsonschema:"Transition ID. Use jira_schema resource=transitions issue_key=X to find valid IDs."`

	Comment       string `json:"comment,omitempty" jsonschema:"Comment body. Format controlled by comment_format (default Markdown → ADF). Used for comment/edit_comment and optionally with transition."`
	CommentFormat string `json:"comment_format,omitempty" jsonschema:"How to interpret comment. markdown (default): converted to ADF and sent via v3. wiki: sent verbatim as legacy Jira wiki-markup via v2. Allowed: markdown, wiki."`
	CommentID     string `json:"comment_id,omitempty" jsonschema:"Comment ID for edit_comment action."`

	SprintID int `json:"sprint_id,omitempty" jsonschema:"Sprint ID for move_to_sprint action."`

	FieldsJSON string `json:"fields_json,omitempty" jsonschema:"Raw JSON object merged into issue fields. Escape hatch for custom fields."`

	Links   []LinkItem `json:"links,omitempty" jsonschema:"Issue links to add. Each entry needs type, from, to. Use jira_schema resource=link_types to discover type names. Optional comment posts a comment on the inward issue at link time (markdown only)."`
	Unlinks []LinkItem `json:"unlinks,omitempty" jsonschema:"Issue links to remove. Each entry needs link_id OR (type, from, to) — when the triple is given, the server resolves the link by reading issuelinks on the active issue. comment is ignored on unlinks."`
}

type WriteArgs struct {
	Action string      `json:"action" jsonschema:"Action: create, update, delete, transition, comment, edit_comment, move_to_sprint."`
	Items  []WriteItem `json:"items" jsonschema:"Array of items to process. Even a single operation should be wrapped in an array."`
	DryRun bool        `json:"dry_run,omitempty" jsonschema:"Preview changes without applying them. Default false."`
}

var writeTool = &mcp.Tool{
	Name:        "jira_write",
	InputSchema: mustBuildWriteInputSchema(),
	Description: `Modify JIRA data. Batch-first: pass an array of items even for single operations.

Actions:
- create: Create issues. Each item needs: project, summary, issue_type. Optional: description (Markdown), assignee, priority, labels, parent_key, links, fields_json.
- update: Update issues. Each item needs: key. Provide fields to change: summary, description, assignee, priority, labels, parent_key, links, unlinks, fields_json.
- delete: Delete issues. Each item needs: key.
- transition: Transition issues. Each item needs: key, transition_id. Optional: comment (Markdown). Hint: Use jira_schema resource=transitions to find IDs.
- comment: Add comments. Each item needs: key, comment (Markdown).
- edit_comment: Edit comments. Each item needs: key, comment_id, comment (Markdown).
- move_to_sprint: Move issues to a sprint. Each item needs: key, sprint_id.

Creating issues:
- Required custom fields are automatically validated before submission. If any are missing, the error lists each field by name with allowed values.
- Pass custom fields via fields_json (e.g. fields_json="{\"customfield_10104\": {\"value\": \"Production\"}}").
- If the issue type is invalid for the project, the error lists available types.

Linking issues:
- links creates new issue links. Each entry needs type, from, to. Use jira_schema resource=link_types to discover type names and verb directions. 'from' is always the active side (e.g. for type=Blocks, 'from' blocks 'to'). Optional Markdown comment posts a comment on the inward issue at link time; for wiki-markup, post a separate comment action after the link is created.
- unlinks removes links. Each entry needs link_id OR (type, from, to). When the triple is provided, the server resolves the link by reading issuelinks on the active issue.
- parent_key sets the parent issue (e.g. an Epic for a sub-task). Available on create and update. Jira enforces parent-type compatibility per project configuration.
- Labels still use the read-then-write replace-list pattern; link operations are targeted.

All actions support dry_run=true to preview without executing.

Descriptions and comments expect Markdown by default and are converted to ADF via the v3 API. Do not round-trip a jira_read result straight into jira_write — old issues return legacy Jira wiki-markup, which is not Markdown. Wiki-markup tokens ({code}, {{inline}}, h1., etc.) are detected and rejected on the default path. To send wiki-markup deliberately, set description_format="wiki" or comment_format="wiki" — the write is then routed through the v2 API with the raw string.`,
}

// linkDirection encodes which side of a link the active issue plays.
//   - linkOutward: active key sits on the From (outward, active) side.
//   - linkInward:  active key sits on the To (inward, passive) side.
type linkDirection int

const (
	linkOutward linkDirection = iota
	linkInward
)

// arrow renders the link as "from → to (type)" — used in success and
// error messages on the links path.
func (li LinkItem) arrow() string {
	return fmt.Sprintf("%s → %s (%s)", li.From, li.To, li.Type)
}

// triple renders the link as "(from / type / to)" — used in unlink
// messages where directionality is implied by surrounding text.
func (li LinkItem) triple() string {
	return fmt.Sprintf("(%s / %s / %s)", li.From, li.Type, li.To)
}

// resolveUnlinkDirection determines which side of li the activeKey plays.
// Returns the direction and the "other" key. ok is false when activeKey
// matches neither From nor To, in which case the caller should surface a
// pass-link_id-explicitly error.
func resolveUnlinkDirection(activeKey string, li LinkItem) (dir linkDirection, otherKey string, ok bool) {
	switch activeKey {
	case li.From:
		return linkOutward, li.To, true
	case li.To:
		return linkInward, li.From, true
	default:
		return 0, "", false
	}
}

// resolveLinkID walks the issuelinks of activeKey to find the unique link
// matching (linkType, otherKey, direction). Returns an error mentioning
// link_id if zero or more than one match.
func (h *handlers) resolveLinkID(ctx context.Context, activeKey, linkType, otherKey string, dir linkDirection) (string, error) {
	issue, err := h.client.GetIssue(ctx, activeKey, &jira.GetQueryOptions{Fields: "issuelinks"})
	if err != nil {
		return "", fmt.Errorf("failed to read %s to resolve link id: %w", activeKey, err)
	}
	if issue == nil || issue.Fields == nil {
		return "", fmt.Errorf("no link found from %s of type %s to %s — already removed?", activeKey, linkType, otherKey)
	}

	var matches []string
	for _, l := range issue.Fields.IssueLinks {
		if l == nil || !strings.EqualFold(l.Type.Name, linkType) {
			continue
		}
		switch dir {
		case linkOutward:
			if l.InwardIssue != nil && l.InwardIssue.Key == otherKey {
				matches = append(matches, l.ID)
			}
		case linkInward:
			if l.OutwardIssue != nil && l.OutwardIssue.Key == otherKey {
				matches = append(matches, l.ID)
			}
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no %s link found between %s and %s — already removed? Pass link_id explicitly to remove a specific link", linkType, activeKey, otherKey)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple %s links between %s and %s (%d matches) — pass link_id explicitly to disambiguate", linkType, activeKey, otherKey, len(matches))
	}
}

// applyUnlinks removes each link in items. When LinkID is empty, the
// helper resolves it from the (Type, From, To) triple by reading the
// active issue's issuelinks. activeKey must equal exactly one of From/To
// to determine direction. Comment is ignored with a warning. dryRun
// makes zero state-reading calls.
func (h *handlers) applyUnlinks(ctx context.Context, activeKey string, items []LinkItem, dryRun bool) []string {
	out := make([]string, 0, len(items))
	for _, li := range items {
		if dryRun {
			if li.LinkID != "" {
				out = append(out, fmt.Sprintf("  Would unlink link %s.", li.LinkID))
			} else {
				out = append(out, fmt.Sprintf("  Would unlink %s — link_id resolved at apply time.", li.triple()))
			}
			continue
		}

		linkID := li.LinkID
		if linkID == "" {
			dir, otherKey, ok := resolveUnlinkDirection(activeKey, li)
			if !ok {
				out = append(out, fmt.Sprintf("  ERROR: unlink %s: active issue %s is on neither side of the link — pass link_id explicitly", li.triple(), activeKey))
				continue
			}
			id, err := h.resolveLinkID(ctx, activeKey, li.Type, otherKey, dir)
			if err != nil {
				out = append(out, fmt.Sprintf("  ERROR: unlink %s: %v", li.triple(), err))
				continue
			}
			linkID = id
		}

		if err := h.client.DeleteIssueLink(ctx, linkID); err != nil {
			out = append(out, fmt.Sprintf("  ERROR: unlink link %s: %v", linkID, err))
			continue
		}

		line := fmt.Sprintf("  Unlinked link %s.", linkID)
		if li.Comment != "" {
			line += " (comment ignored on unlinks)"
		}
		out = append(out, line)
	}
	return out
}

// applyLinks creates each link in items in order. Per-entry errors are
// collected and formatted as result lines; never short-circuits.
// Each LinkItem maps to a CreateIssueLinkInput as From=outward, To=inward.
func (h *handlers) applyLinks(ctx context.Context, items []LinkItem, dryRun bool) []string {
	out := make([]string, 0, len(items))
	for _, li := range items {
		arrow := li.arrow()
		if dryRun {
			out = append(out, fmt.Sprintf("  Would link %s.", arrow))
			continue
		}
		in := jira.CreateIssueLinkInput{
			Type:         li.Type,
			OutwardIssue: li.From,
			InwardIssue:  li.To,
		}
		if li.Comment != "" {
			body, _, err := buildCommentBody(li.Comment, formatMarkdown)
			if err != nil {
				out = append(out, fmt.Sprintf("  ERROR: link %s rejected: %v", arrow, err))
				continue
			}
			in.Comment = &jira.IssueLinkComment{Body: body}
		}
		if err := h.client.CreateIssueLink(ctx, in); err != nil {
			out = append(out, fmt.Sprintf("  ERROR: link failed %s: %v", arrow, err))
			continue
		}
		out = append(out, fmt.Sprintf("  Linked %s.", arrow))
	}
	return out
}

// validateLinkItems performs static checks on a slice of LinkItem entries.
// isUnlink toggles the rule set: links require Type+From+To, unlinks require
// LinkID OR Type+From+To. Self-links are always rejected.
func validateLinkItems(items []LinkItem, isUnlink bool) error {
	what := "links"
	if isUnlink {
		what = "unlinks"
	}
	for i, li := range items {
		if isUnlink {
			if li.LinkID == "" && (li.Type == "" || li.From == "" || li.To == "") {
				return fmt.Errorf("%s[%d]: provide link_id or all of (type, from, to) to identify the link", what, i)
			}
		} else {
			if li.Type == "" {
				return fmt.Errorf("%s[%d]: type is required (e.g. Blocks). Use jira_schema resource=link_types to discover valid names", what, i)
			}
			if li.From == "" || li.To == "" {
				return fmt.Errorf("%s[%d]: from and to are required and must be issue keys", what, i)
			}
		}
		if li.From != "" && li.To != "" && li.From == li.To {
			return fmt.Errorf("%s[%d]: cannot link an issue to itself (from=%s to=%s)", what, i, li.From, li.To)
		}
	}
	return nil
}

// createMetaCache caches create-metadata API responses within a single
// handleWrite call to avoid redundant requests for batch creates.
type createMetaCache struct {
	issueTypes map[string][]jira.CreateMetaIssueType        // project → issue types
	fields     map[string]map[string][]jira.CreateMetaField // project → issueTypeID → fields
}

func newCreateMetaCache() *createMetaCache {
	return &createMetaCache{
		issueTypes: make(map[string][]jira.CreateMetaIssueType),
		fields:     make(map[string]map[string][]jira.CreateMetaField),
	}
}

func (h *handlers) handleWrite(ctx context.Context, _ *mcp.CallToolRequest, args WriteArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Items) == 0 {
		return textResult("items array is empty. Provide at least one item.", true), nil, nil
	}

	if args.Action == "move_to_sprint" {
		return h.handleMoveToSprint(ctx, args), nil, nil
	}

	cache := newCreateMetaCache()
	var results []string

	for i, item := range args.Items {
		prefix := fmt.Sprintf("[%d] ", i+1)
		var msg string
		var err error

		switch args.Action {
		case "create":
			msg, err = h.writeCreate(ctx, item, args.DryRun, cache)
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

// wikiMarkupError constructs the rejection message for default (markdown)
// writes that contain detected wiki-markup. field is the user-facing parameter
// name ("description" or "comment"); optHint is the exact flag wording the
// caller can use to opt in (e.g. `description_format="wiki"`).
func wikiMarkupError(field, optHint string, hits []mdconv.WikiMarkupHit) error {
	const maxHits = 5
	n := len(hits)
	if n > maxHits {
		n = maxHits
	}
	examples := make([]string, n)
	for i := 0; i < n; i++ {
		examples[i] = fmt.Sprintf("%s (line %d)", hits[i].Token, hits[i].LineNumber+1)
	}
	return fmt.Errorf(
		"%s appears to be Jira wiki-markup, not Markdown. Found tokens: %s. "+
			"If you intended wiki-markup, set %s; otherwise convert to Markdown "+
			"(```lang ... ``` for code, **bold** for bold, etc)",
		field, strings.Join(examples, ", "), optHint,
	)
}

// Body format identifiers for description and comment fields.
const (
	formatMarkdown = "markdown"
	formatWiki     = "wiki"
)

// validBodyFormats enumerates the accepted description_format / comment_format
// values. The ADF-description plan may extend this set with "adf" — keep the
// literal here so both plans only touch a single map on merge.
var validBodyFormats = map[string]bool{
	formatMarkdown: true,
	formatWiki:     true,
}

// bodyFormatEnum is the JSON Schema enum surfaced on description_format and
// comment_format. Keep in sync with validBodyFormats.
var bodyFormatEnum = []any{formatMarkdown, formatWiki}

// resolveBodyFormat defaults an empty format string to markdown and returns an
// error naming the accepted values if the caller supplied anything else.
func resolveBodyFormat(paramName, value string) (string, error) {
	if value == "" {
		return formatMarkdown, nil
	}
	if !validBodyFormats[value] {
		return "", fmt.Errorf("%s %q is not supported. Valid: %s, %s", paramName, value, formatMarkdown, formatWiki)
	}
	return value, nil
}

// mustBuildWriteInputSchema derives the WriteArgs schema via reflection and
// patches the format fields with a JSON Schema enum. The tag-based inference
// only supports a `description`, so the enum has to be grafted on explicitly.
func mustBuildWriteInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[WriteArgs](&jsonschema.ForOptions{})
	if err != nil {
		panic(fmt.Sprintf("jira_write input schema: %v", err))
	}
	itemSchema := schema.Properties["items"].Items
	itemSchema.Properties["description_format"].Enum = bodyFormatEnum
	itemSchema.Properties["comment_format"].Enum = bodyFormatEnum
	return schema
}

// buildIssuePayload constructs an issue payload and returns the resolved
// description format. Callers dispatch on format == formatWiki to choose
// between the v2 (raw wiki-markup string) and v3 (ADF) endpoints.
func buildIssuePayload(item WriteItem) (payload map[string]any, format string, err error) {
	format, err = resolveBodyFormat("description_format", item.DescriptionFormat)
	if err != nil {
		return nil, "", err
	}

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
	if item.ParentKey != "" {
		fields["parent"] = map[string]any{"key": item.ParentKey}
	}
	if item.Description != "" {
		switch format {
		case formatWiki:
			fields["description"] = item.Description
		default: // markdown
			if hits := mdconv.DetectWikiMarkup(item.Description); len(hits) > 0 {
				return nil, "", wikiMarkupError("description", `description_format="wiki"`, hits)
			}
			adf := mdconv.ToADF(item.Description)
			if adf != nil {
				fields["description"] = adf
			}
		}
	}
	if item.FieldsJSON != "" {
		var extra map[string]any
		if err := json.Unmarshal([]byte(item.FieldsJSON), &extra); err != nil {
			return nil, "", fmt.Errorf("invalid fields_json: %w. Hint: Provide a valid JSON object like {\"customfield_10001\": \"value\"}", err)
		}
		for k, v := range extra {
			fields[k] = v
		}
	}

	return map[string]any{"fields": fields}, format, nil
}

// buildCommentBody prepares a comment payload and returns the resolved format.
// For markdown it returns an ADF doc destined for v3; for wiki it returns the
// raw string destined for v2. Callers dispatch on format == formatWiki.
func buildCommentBody(body, rawFormat string) (out any, format string, err error) {
	format, err = resolveBodyFormat("comment_format", rawFormat)
	if err != nil {
		return nil, "", err
	}
	if format == formatWiki {
		return body, format, nil
	}

	if hits := mdconv.DetectWikiMarkup(body); len(hits) > 0 {
		return nil, "", wikiMarkupError("comment", `comment_format="wiki"`, hits)
	}

	adf := mdconv.ToADF(body)
	if adf != nil {
		return adf, format, nil
	}
	// Fallback: wrap plain text in minimal ADF.
	return map[string]any{
		"version": 1,
		"type":    "doc",
		"content": []any{
			map[string]any{
				"type": "paragraph",
				"content": []any{
					map[string]any{"type": "text", "text": body},
				},
			},
		},
	}, format, nil
}

// standardFields are field IDs that buildIssuePayload maps from WriteItem
// struct fields. They don't need to appear in fields_json.
var standardFields = map[string]bool{
	"project": true, "summary": true, "issuetype": true,
	"priority": true, "assignee": true, "description": true, "labels": true,
}

func (h *handlers) writeCreate(ctx context.Context, item WriteItem, dryRun bool, cache *createMetaCache) (string, error) {
	if item.Project == "" || item.Summary == "" || item.IssueType == "" {
		return "", fmt.Errorf("create requires project, summary, and issue_type. Got project=%q summary=%q issue_type=%q", item.Project, item.Summary, item.IssueType)
	}
	if len(item.Unlinks) > 0 {
		return "", fmt.Errorf("unlinks not valid on action=create — nothing to unlink. Use action=update on an existing issue")
	}
	if err := validateLinkItems(item.Links, false); err != nil {
		return "", err
	}

	payload, format, err := buildIssuePayload(item)
	if err != nil {
		return "", err
	}

	// Preflight: advisory check for required fields via create metadata.
	// Never blocks — hints are appended to the response so the LLM can iterate.
	var preflightHint string
	missingMsg, err := h.preflightRequiredFields(ctx, item, payload, cache)
	if err != nil {
		// Metadata fetch failed; proceed without hints.
		preflightHint = ""
	} else if missingMsg != "" {
		preflightHint = fmt.Sprintf("\nPreflight warning: %s", missingMsg)
	}

	if dryRun {
		data, _ := json.MarshalIndent(payload, "", "  ")
		head := fmt.Sprintf("Would create issue in project %s with type %s:\n%s%s", item.Project, item.IssueType, string(data), preflightHint)
		if linkLines := h.applyLinks(ctx, item.Links, true); len(linkLines) > 0 {
			head += "\n" + strings.Join(linkLines, "\n")
		}
		return head, nil
	}

	var key string
	if format == formatWiki {
		key, _, err = h.client.CreateIssueV2(ctx, payload)
	} else {
		key, _, err = h.client.CreateIssueV3(ctx, payload)
	}
	if err != nil {
		return "", fmt.Errorf("failed to create issue in %s: %w; %s%s", item.Project, err, createErrorHints(err), preflightHint)
	}

	msg := fmt.Sprintf("Created %s — %s (project=%s, type=%s). Hint: Use jira_read keys=[\"%s\"] to see the full issue.", key, item.Summary, item.Project, item.IssueType, key)
	if linkLines := h.applyLinks(ctx, item.Links, false); len(linkLines) > 0 {
		msg += "\n" + strings.Join(linkLines, "\n")
	}
	return msg, nil
}

// preflightRequiredFields fetches create metadata and returns an error message
// listing any required fields that are missing from the payload. Returns ""
// if all required fields are present.
func (h *handlers) preflightRequiredFields(ctx context.Context, item WriteItem, payload map[string]any, cache *createMetaCache) (string, error) {
	// Resolve issue type name → ID, using cache to avoid redundant API calls in batches.
	issueTypes, ok := cache.issueTypes[item.Project]
	if !ok {
		var err error
		issueTypes, err = h.client.GetCreateMetaIssueTypes(ctx, item.Project)
		if err != nil {
			return "", err
		}
		cache.issueTypes[item.Project] = issueTypes
	}

	var issueTypeID string
	for _, it := range issueTypes {
		if strings.EqualFold(it.Name, item.IssueType) {
			issueTypeID = it.ID
			break
		}
	}
	if issueTypeID == "" {
		names := make([]string, len(issueTypes))
		for i, it := range issueTypes {
			names[i] = it.Name
		}
		return fmt.Sprintf("issue type %q not found in project %s. Available types: %s",
			item.IssueType, item.Project, strings.Join(names, ", ")), nil
	}

	// Fetch required fields for this project + issue type, using cache.
	if cache.fields[item.Project] == nil {
		cache.fields[item.Project] = make(map[string][]jira.CreateMetaField)
	}
	metaFields, ok2 := cache.fields[item.Project][issueTypeID]
	if !ok2 {
		var err error
		metaFields, err = h.client.GetCreateMetaFields(ctx, item.Project, issueTypeID)
		if err != nil {
			return "", err
		}
		cache.fields[item.Project][issueTypeID] = metaFields
	}

	// Determine which fields are present in the payload.
	// fields_json values are already merged into payloadFields by buildIssuePayload,
	// so no separate extraJSON check is needed.
	payloadFields := payload["fields"].(map[string]any)

	var missing []string
	for _, f := range metaFields {
		if !f.Required || f.HasDefaultValue {
			continue
		}
		if standardFields[f.FieldID] {
			continue
		}
		if _, ok := payloadFields[f.FieldID]; ok {
			continue
		}

		hint := fmt.Sprintf("- %s (%s): required", f.Name, f.FieldID)
		if len(f.AllowedValues) > 0 {
			var vals []string
			for _, v := range f.AllowedValues {
				if name, ok := v["value"].(string); ok {
					vals = append(vals, name)
				} else if name, ok := v["name"].(string); ok {
					vals = append(vals, name)
				}
			}
			if len(vals) > 0 {
				hint += fmt.Sprintf(". Allowed values: %s", strings.Join(vals, ", "))
			}
		}
		missing = append(missing, hint)
	}

	if len(missing) == 0 {
		return "", nil
	}

	return fmt.Sprintf("missing required fields. Pass them via fields_json:\n%s", strings.Join(missing, "\n")), nil
}

func (h *handlers) writeUpdate(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.Key == "" {
		return "", fmt.Errorf("update requires key")
	}
	if err := validateLinkItems(item.Links, false); err != nil {
		return "", err
	}
	if err := validateLinkItems(item.Unlinks, true); err != nil {
		return "", err
	}

	payload, format, err := buildIssuePayload(item)
	if err != nil {
		return "", err
	}

	fields, _ := payload["fields"].(map[string]any)
	hasFieldUpdates := len(fields) > 0

	if dryRun {
		var head string
		if hasFieldUpdates {
			data, _ := json.MarshalIndent(payload, "", "  ")
			head = fmt.Sprintf("Would update %s with:\n%s", item.Key, string(data))
		} else {
			head = fmt.Sprintf("No field updates on %s.", item.Key)
		}
		var extra []string
		extra = append(extra, h.applyUnlinks(ctx, item.Key, item.Unlinks, true)...)
		extra = append(extra, h.applyLinks(ctx, item.Links, true)...)
		if len(extra) > 0 {
			head += "\n" + strings.Join(extra, "\n")
		}
		return head, nil
	}

	var msg string
	if hasFieldUpdates {
		if format == formatWiki {
			err = h.client.UpdateIssueV2(ctx, item.Key, payload)
		} else {
			err = h.client.UpdateIssueV3(ctx, item.Key, payload)
		}
		if err != nil {
			return "", fmt.Errorf("failed to update %s: %w", item.Key, err)
		}
		msg = fmt.Sprintf("Updated %s successfully.", item.Key)
	} else {
		msg = fmt.Sprintf("No field updates on %s.", item.Key)
	}

	var extra []string
	extra = append(extra, h.applyUnlinks(ctx, item.Key, item.Unlinks, false)...)
	extra = append(extra, h.applyLinks(ctx, item.Links, false)...)
	if len(extra) > 0 {
		msg += "\n" + strings.Join(extra, "\n")
	}
	return msg, nil
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
		body, format, err := buildCommentBody(item.Comment, item.CommentFormat)
		if err != nil {
			msg += fmt.Sprintf(" Warning: transition succeeded but comment rejected: %v", err)
		} else if _, err := addComment(ctx, h.client, item.Key, body, format); err != nil {
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

	body, format, err := buildCommentBody(item.Comment, item.CommentFormat)
	if err != nil {
		return "", err
	}
	commentID, err := addComment(ctx, h.client, item.Key, body, format)
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

	body, format, err := buildCommentBody(item.Comment, item.CommentFormat)
	if err != nil {
		return "", err
	}
	if err := updateComment(ctx, h.client, item.Key, item.CommentID, body, format); err != nil {
		return "", fmt.Errorf("failed to edit comment %s on %s: %w", item.CommentID, item.Key, err)
	}

	return fmt.Sprintf("Updated comment %s on %s.", item.CommentID, item.Key), nil
}

// addComment dispatches to AddCommentV2 (wiki-markup string) or AddComment
// (ADF) based on format. buildCommentBody returns string vs map[string]any
// accordingly, so an unsafe cast would be wrong — branch on format instead.
func addComment(ctx context.Context, client JiraClient, key string, body any, format string) (string, error) {
	if format == formatWiki {
		return client.AddCommentV2(ctx, key, body.(string))
	}
	return client.AddComment(ctx, key, body)
}

// updateComment mirrors addComment for edits.
func updateComment(ctx context.Context, client JiraClient, key, commentID string, body any, format string) error {
	if format == formatWiki {
		return client.UpdateCommentV2(ctx, key, commentID, body.(string))
	}
	return client.UpdateComment(ctx, key, commentID, body)
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
