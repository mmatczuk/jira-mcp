package jiramcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SchemaArgs struct {
	Resource string `json:"resource" jsonschema:"Metadata to discover: fields, transitions, field_options, link_types."`
	IssueKey string `json:"issue_key,omitempty" jsonschema:"Issue key. Required for resource=transitions."`
	FieldID  string `json:"field_id,omitempty" jsonschema:"Field ID. Required for resource=field_options (e.g. customfield_10001)."`
}

var schemaTool = &mcp.Tool{
	Name: "jira_schema",
	Description: `Discover JIRA metadata needed to construct valid jira_write payloads.

Resources:
- fields: List all available fields (standard and custom). Returns field ID, name, and type.
- transitions: List available transitions for an issue. Requires issue_key. Returns transition ID and name — use these IDs with jira_write action=transition.
- field_options: List allowed values for a custom field. Requires field_id (e.g. customfield_10001). Fetches the field's context, then its options.
- link_types: List available issue link types. Returns id, name, inward verb, outward verb. The 'from' field on jira_write links is always the active (outward) side.

Hint: Always check transitions before transitioning an issue. Field IDs from "fields" can be used in jira_write fields_json.`,
}

func (h *handlers) handleSchema(ctx context.Context, _ *mcp.CallToolRequest, args SchemaArgs) (*mcp.CallToolResult, any, error) {
	switch args.Resource {
	case "fields":
		return h.schemaFields(ctx), nil, nil
	case "transitions":
		return h.schemaTransitions(ctx, args), nil, nil
	case "field_options":
		return h.schemaFieldOptions(ctx, args), nil, nil
	case "link_types":
		return h.schemaLinkTypes(ctx), nil, nil
	default:
		return textResult(fmt.Sprintf("Unknown resource %q. Valid: fields, transitions, field_options, link_types.", args.Resource), true), nil, nil
	}
}

func (h *handlers) schemaFields(ctx context.Context) *mcp.CallToolResult {
	fields, err := h.client.GetFields(ctx)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to list fields: %v", err), true)
	}

	var results []map[string]any
	for _, f := range fields {
		entry := map[string]any{
			"id":     f.ID,
			"name":   f.Name,
			"custom": f.Custom,
		}
		if f.Schema.Type != "" {
			entry["schema_type"] = f.Schema.Type
		}
		if f.Schema.Items != "" {
			entry["schema_items"] = f.Schema.Items
		}
		results = append(results, entry)
	}

	data, _ := json.Marshal(results)
	out := fmt.Sprintf("Found %d field(s). Use field IDs in jira_write fields_json for custom fields.\n\n%s", len(results), string(data))

	return textResult(out, false)
}

func (h *handlers) schemaTransitions(ctx context.Context, args SchemaArgs) *mcp.CallToolResult {
	if args.IssueKey == "" {
		return textResult("issue_key is required for resource=transitions.", true)
	}

	transitions, err := h.client.GetTransitions(ctx, args.IssueKey)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to get transitions for %s: %v", args.IssueKey, err), true)
	}

	var results []map[string]any
	for _, t := range transitions {
		entry := map[string]any{
			"id":   t.ID,
			"name": t.Name,
		}
		if t.To.Name != "" {
			entry["to_status"] = t.To.Name
		}
		results = append(results, entry)
	}

	data, _ := json.Marshal(results)
	out := fmt.Sprintf("Found %d transition(s) for %s. Use the transition ID with jira_write action=transition transition_id=<id>.\n\n%s", len(results), args.IssueKey, string(data))

	return textResult(out, false)
}

func (h *handlers) schemaLinkTypes(ctx context.Context) *mcp.CallToolResult {
	types, err := h.client.GetIssueLinkTypes(ctx)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to list link types: %v", err), true)
	}

	results := make([]map[string]any, 0, len(types))
	bullets := make([]string, 0, len(types))
	for _, t := range types {
		results = append(results, map[string]any{
			"id":      t.ID,
			"name":    t.Name,
			"inward":  t.Inward,
			"outward": t.Outward,
		})
		if t.Inward != t.Outward {
			bullets = append(bullets, fmt.Sprintf("  %s: from %s to (inverse: to %s from)", t.Name, t.Outward, t.Inward))
		} else {
			bullets = append(bullets, fmt.Sprintf("  %s: from %s to", t.Name, t.Outward))
		}
	}

	data, _ := json.Marshal(results)
	out := fmt.Sprintf(
		"Found %d link type(s). Use the 'name' field with jira_write links/unlinks. For each type:\n%s\n\n%s",
		len(results), strings.Join(bullets, "\n"), string(data),
	)
	return textResult(out, false)
}

func (h *handlers) schemaFieldOptions(ctx context.Context, args SchemaArgs) *mcp.CallToolResult {
	if args.FieldID == "" {
		return textResult("field_id is required for resource=field_options. Hint: Use jira_schema resource=fields to find field IDs.", true)
	}

	values, err := h.client.GetFieldOptions(ctx, args.FieldID)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to get options for field %s: %v. Hint: Not all fields have enumerable options. This works best with select/multiselect custom fields.", args.FieldID, err), true)
	}

	if len(values) == 0 {
		return textResult(fmt.Sprintf("No options found for field %s. The field may not have a context, or may not be a select/multiselect type.", args.FieldID), false)
	}

	data, _ := json.Marshal(values)
	out := fmt.Sprintf("Found %d option(s) for field %s.\n\n%s", len(values), args.FieldID, string(data))

	return textResult(out, false)
}
