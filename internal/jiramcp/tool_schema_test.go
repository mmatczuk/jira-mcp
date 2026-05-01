package jiramcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/mmatczuk/jira-mcp/internal/jira"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func callSchema(t *testing.T, h *handlers, args SchemaArgs) (string, bool) {
	t.Helper()
	result, _, err := h.handleSchema(context.Background(), nil, args)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	return text, result.IsError
}

// --- dispatch ---

func TestHandleSchema_UnknownResource(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	text, isErr := callSchema(t, h, SchemaArgs{Resource: "bogus"})
	assert.True(t, isErr)
	assert.Contains(t, text, `Unknown resource "bogus"`)
}

// --- fields ---

func TestSchemaFields_Success(t *testing.T) {
	mc := &mockClient{
		GetFieldsFn: func(context.Context) ([]jira.Field, error) {
			return []jira.Field{
				{
					ID:     "summary",
					Name:   "Summary",
					Custom: false,
					Schema: jira.FieldSchema{Type: "string"},
				},
				{
					ID:     "customfield_10001",
					Name:   "Story Points",
					Custom: true,
					Schema: jira.FieldSchema{Type: "number"},
				},
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callSchema(t, h, SchemaArgs{Resource: "fields"})
	assert.False(t, isErr)
	assert.Contains(t, text, "Found 2 field(s)")
	assert.Contains(t, text, "summary")
	assert.Contains(t, text, "customfield_10001")
	assert.Contains(t, text, "Story Points")
}

func TestSchemaFields_Error(t *testing.T) {
	mc := &mockClient{
		GetFieldsFn: func(context.Context) ([]jira.Field, error) {
			return nil, fmt.Errorf("auth expired")
		},
	}
	h := &handlers{client: mc}
	text, isErr := callSchema(t, h, SchemaArgs{Resource: "fields"})
	assert.True(t, isErr)
	assert.Contains(t, text, "auth expired")
}

func TestSchemaFields_SchemaItems(t *testing.T) {
	mc := &mockClient{
		GetFieldsFn: func(context.Context) ([]jira.Field, error) {
			return []jira.Field{
				{
					ID:   "labels",
					Name: "Labels",
					Schema: jira.FieldSchema{
						Type:  "array",
						Items: "string",
					},
				},
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, _ := callSchema(t, h, SchemaArgs{Resource: "fields"})
	assert.Contains(t, text, "schema_items")
	assert.Contains(t, text, "string")
}

// --- transitions ---

func TestSchemaTransitions_Success(t *testing.T) {
	mc := &mockClient{
		GetTransitionsFn: func(_ context.Context, key string) ([]jira.Transition, error) {
			assert.Equal(t, "PROJ-1", key)
			return []jira.Transition{
				{ID: "11", Name: "Start Progress", To: jira.Status{Name: "In Progress"}},
				{ID: "21", Name: "Done", To: jira.Status{Name: "Done"}},
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callSchema(t, h, SchemaArgs{Resource: "transitions", IssueKey: "PROJ-1"})
	assert.False(t, isErr)
	assert.Contains(t, text, "Found 2 transition(s)")
	assert.Contains(t, text, "Start Progress")
	assert.Contains(t, text, "In Progress")
}

func TestSchemaTransitions_NoIssueKey(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	text, isErr := callSchema(t, h, SchemaArgs{Resource: "transitions"})
	assert.True(t, isErr)
	assert.Contains(t, text, "issue_key is required")
}

func TestSchemaTransitions_Error(t *testing.T) {
	mc := &mockClient{
		GetTransitionsFn: func(context.Context, string) ([]jira.Transition, error) {
			return nil, fmt.Errorf("issue not found")
		},
	}
	h := &handlers{client: mc}
	text, isErr := callSchema(t, h, SchemaArgs{Resource: "transitions", IssueKey: "BAD-1"})
	assert.True(t, isErr)
	assert.Contains(t, text, "issue not found")
}

// --- link_types ---

func TestSchemaLinkTypes_Success(t *testing.T) {
	mc := &mockClient{
		GetIssueLinkTypesFn: func(context.Context) ([]jira.IssueLinkType, error) {
			return []jira.IssueLinkType{
				{ID: "10000", Name: "Blocks", Inward: "is blocked by", Outward: "blocks"},
				{ID: "10001", Name: "Relates", Inward: "relates to", Outward: "relates to"},
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callSchema(t, h, SchemaArgs{Resource: "link_types"})
	assert.False(t, isErr)
	assert.Contains(t, text, "Found 2 link type(s)")
	assert.Contains(t, text, "Blocks")
	assert.Contains(t, text, "blocks")
	assert.Contains(t, text, "is blocked by")
	assert.Contains(t, text, "Relates")
}

func TestSchemaLinkTypes_Error(t *testing.T) {
	mc := &mockClient{
		GetIssueLinkTypesFn: func(context.Context) ([]jira.IssueLinkType, error) {
			return nil, fmt.Errorf("auth expired")
		},
	}
	h := &handlers{client: mc}
	text, isErr := callSchema(t, h, SchemaArgs{Resource: "link_types"})
	assert.True(t, isErr)
	assert.Contains(t, text, "auth expired")
}

func TestHandleSchema_UnknownResource_MentionsLinkTypes(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	text, isErr := callSchema(t, h, SchemaArgs{Resource: "bogus"})
	assert.True(t, isErr)
	assert.Contains(t, text, "link_types")
}

// --- field_options ---

func TestSchemaFieldOptions_Success(t *testing.T) {
	mc := &mockClient{
		GetFieldOptionsFn: func(_ context.Context, fieldID string) ([]json.RawMessage, error) {
			assert.Equal(t, "customfield_10001", fieldID)
			return []json.RawMessage{
				json.RawMessage(`{"id":"1","value":"Option A"}`),
				json.RawMessage(`{"id":"2","value":"Option B"}`),
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callSchema(t, h, SchemaArgs{Resource: "field_options", FieldID: "customfield_10001"})
	assert.False(t, isErr)
	assert.Contains(t, text, "Found 2 option(s)")
	assert.Contains(t, text, "Option A")
}

func TestSchemaFieldOptions_NoFieldID(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	text, isErr := callSchema(t, h, SchemaArgs{Resource: "field_options"})
	assert.True(t, isErr)
	assert.Contains(t, text, "field_id is required")
}

func TestSchemaFieldOptions_Empty(t *testing.T) {
	mc := &mockClient{
		GetFieldOptionsFn: func(context.Context, string) ([]json.RawMessage, error) {
			return nil, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callSchema(t, h, SchemaArgs{Resource: "field_options", FieldID: "cf_999"})
	assert.False(t, isErr)
	assert.Contains(t, text, "No options found")
}

func TestSchemaFieldOptions_Error(t *testing.T) {
	mc := &mockClient{
		GetFieldOptionsFn: func(context.Context, string) ([]json.RawMessage, error) {
			return nil, fmt.Errorf("permission denied")
		},
	}
	h := &handlers{client: mc}
	text, isErr := callSchema(t, h, SchemaArgs{Resource: "field_options", FieldID: "cf_1"})
	assert.True(t, isErr)
	assert.Contains(t, text, "permission denied")
}
