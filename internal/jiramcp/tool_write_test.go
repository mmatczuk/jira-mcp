package jiramcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mmatczuk/jira-mcp/internal/jira"
)

// --- buildIssuePayload ---

func TestBuildIssuePayload_AllFields(t *testing.T) {
	item := WriteItem{
		Project:     "PROJ",
		Summary:     "Test summary",
		IssueType:   "Bug",
		Priority:    "High",
		Assignee:    "abc123",
		Labels:      []string{"backend", "urgent"},
		Description: "Hello **world**",
	}

	payload, format, err := buildIssuePayload(item)
	require.NoError(t, err)
	assert.Equal(t, formatMarkdown, format, "default format must target v3")

	fields := payload["fields"].(map[string]any)
	assert.Equal(t, map[string]any{"key": "PROJ"}, fields["project"])
	assert.Equal(t, "Test summary", fields["summary"])
	assert.Equal(t, map[string]any{"name": "Bug"}, fields["issuetype"])
	assert.Equal(t, map[string]any{"name": "High"}, fields["priority"])
	assert.Equal(t, map[string]any{"accountId": "abc123"}, fields["assignee"])
	assert.Equal(t, []string{"backend", "urgent"}, fields["labels"])

	desc, ok := fields["description"].(map[string]any)
	require.True(t, ok, "description should be ADF map")
	assert.Equal(t, 1, desc["version"])
	assert.Equal(t, "doc", desc["type"])
}

func TestBuildIssuePayload_EmptyItem(t *testing.T) {
	payload, format, err := buildIssuePayload(WriteItem{})
	require.NoError(t, err)
	assert.Equal(t, formatMarkdown, format)

	fields := payload["fields"].(map[string]any)
	assert.Empty(t, fields)
}

func TestBuildIssuePayload_FieldsJSON_Valid(t *testing.T) {
	item := WriteItem{
		Summary:    "s",
		FieldsJSON: `{"customfield_10001": "hello", "customfield_10002": 42}`,
	}

	payload, _, err := buildIssuePayload(item)
	require.NoError(t, err)

	fields := payload["fields"].(map[string]any)
	assert.Equal(t, "hello", fields["customfield_10001"])
	assert.Equal(t, float64(42), fields["customfield_10002"])
	assert.Equal(t, "s", fields["summary"])
}

func TestBuildIssuePayload_FieldsJSON_Invalid(t *testing.T) {
	item := WriteItem{FieldsJSON: "not json"}

	_, _, err := buildIssuePayload(item)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid fields_json")
}

func TestBuildIssuePayload_FieldsJSON_OverridesStandard(t *testing.T) {
	item := WriteItem{
		Summary:    "original",
		FieldsJSON: `{"summary": "overridden"}`,
	}

	payload, _, err := buildIssuePayload(item)
	require.NoError(t, err)

	fields := payload["fields"].(map[string]any)
	assert.Equal(t, "overridden", fields["summary"])
}

// --- buildCommentBody ---

func TestBuildCommentBody_Markdown(t *testing.T) {
	body, format, err := buildCommentBody("Hello **world**", "")
	require.NoError(t, err)
	assert.Equal(t, formatMarkdown, format)
	m, ok := body.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 1, m["version"])
	assert.Equal(t, "doc", m["type"])
}

func TestBuildCommentBody_EmptyFallback(t *testing.T) {
	body, format, err := buildCommentBody("", "")
	require.NoError(t, err)
	assert.Equal(t, formatMarkdown, format)
	m, ok := body.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "doc", m["type"])
	content := m["content"].([]any)
	para := content[0].(map[string]any)
	assert.Equal(t, "paragraph", para["type"])
}

func TestBuildCommentBody_Wiki(t *testing.T) {
	body, format, err := buildCommentBody("{code}x{code}", "wiki")
	require.NoError(t, err)
	assert.Equal(t, formatWiki, format)
	assert.Equal(t, "{code}x{code}", body)
}

// --- writeTool input schema ---

// TestWriteTool_FormatEnums guards the schema patch in
// mustBuildWriteInputSchema: description_format and comment_format must
// surface the markdown/wiki enum so MCP clients can reject invalid values
// before a request is dispatched.
func TestWriteTool_FormatEnums(t *testing.T) {
	schema, ok := writeTool.InputSchema.(*jsonschema.Schema)
	require.True(t, ok, "writeTool.InputSchema must be a *jsonschema.Schema")

	itemSchema := schema.Properties["items"].Items
	require.NotNil(t, itemSchema)

	for _, field := range []string{"description_format", "comment_format"} {
		t.Run(field, func(t *testing.T) {
			prop := itemSchema.Properties[field]
			require.NotNil(t, prop, "missing property")
			assert.Equal(t, []any{formatMarkdown, formatWiki}, prop.Enum)
		})
	}
}

// --- handleWrite dispatch & validation ---

func newWriteHandlers(mc *mockClient) *handlers {
	return &handlers{client: mc}
}

// withCreateMeta sets up create metadata mocks that return the given issue type
// with no extra required fields. Use this for tests that call writeCreate.
func withCreateMeta(mc *mockClient, issueType string) {
	mc.GetCreateMetaIssueTypesFn = func(_ context.Context, _ string) ([]jira.CreateMetaIssueType, error) {
		return []jira.CreateMetaIssueType{{ID: "1", Name: issueType}}, nil
	}
	mc.GetCreateMetaFieldsFn = func(_ context.Context, _, _ string) ([]jira.CreateMetaField, error) {
		return []jira.CreateMetaField{
			{FieldID: "summary", Name: "Summary", Required: true},
			{FieldID: "issuetype", Name: "Issue Type", Required: true},
			{FieldID: "project", Name: "Project", Required: true},
		}, nil
	}
}

func callWrite(t *testing.T, h *handlers, args WriteArgs) (string, bool) {
	t.Helper()
	result, _, err := h.handleWrite(context.Background(), nil, args)
	require.NoError(t, err) // handler never returns Go error
	text := result.Content[0].(*mcp.TextContent).Text
	return text, result.IsError
}

func TestHandleWrite_EmptyItems(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, isErr := callWrite(t, h, WriteArgs{Action: "create", Items: nil})
	assert.True(t, isErr)
	assert.Contains(t, text, "items array is empty")
}

func TestHandleWrite_UnknownAction(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "bogus",
		Items:  []WriteItem{{Key: "X-1"}},
	})
	assert.True(t, isErr)
	assert.Contains(t, text, `Unknown action "bogus"`)
}

// --- create ---

func TestWriteCreate_Success(t *testing.T) {
	mc := &mockClient{
		CreateIssueV3Fn: func(_ context.Context, payload map[string]any) (string, string, error) {
			fields := payload["fields"].(map[string]any)
			assert.Equal(t, "Test", fields["summary"])
			return "PROJ-1", "10001", nil
		},
	}
	withCreateMeta(mc, "Task")
	h := newWriteHandlers(mc)
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "create",
		Items: []WriteItem{{
			Project:   "PROJ",
			Summary:   "Test",
			IssueType: "Task",
		}},
	})
	assert.False(t, isErr)
	assert.Contains(t, text, "Created PROJ-1")
}

func TestWriteCreate_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		item WriteItem
	}{
		{"no project", WriteItem{Summary: "s", IssueType: "Bug"}},
		{"no summary", WriteItem{Project: "P", IssueType: "Bug"}},
		{"no issue_type", WriteItem{Project: "P", Summary: "s"}},
		{"all empty", WriteItem{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newWriteHandlers(&mockClient{})
			text, isErr := callWrite(t, h, WriteArgs{
				Action: "create",
				Items:  []WriteItem{tc.item},
			})
			assert.False(t, isErr) // errors are in the text, not isError
			assert.Contains(t, text, "ERROR")
			assert.Contains(t, text, "create requires")
		})
	}
}

func TestWriteCreate_DryRun(t *testing.T) {
	mc := &mockClient{}
	withCreateMeta(mc, "Task")
	h := newWriteHandlers(mc)
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "create",
		DryRun: true,
		Items: []WriteItem{{
			Project:   "PROJ",
			Summary:   "Test",
			IssueType: "Task",
		}},
	})
	assert.False(t, isErr)
	assert.Contains(t, text, "DRY RUN")
	assert.Contains(t, text, "Would create issue")
}

func TestWriteCreate_ClientError(t *testing.T) {
	mc := &mockClient{
		CreateIssueV3Fn: func(context.Context, map[string]any) (string, string, error) {
			return "", "", fmt.Errorf("permission denied")
		},
	}
	withCreateMeta(mc, "Bug")
	h := newWriteHandlers(mc)
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "create",
		Items:  []WriteItem{{Project: "P", Summary: "s", IssueType: "Bug"}},
	})
	assert.False(t, isErr)
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "permission denied")
}

func TestWriteCreate_WithFieldsJSON(t *testing.T) {
	mc := &mockClient{
		CreateIssueV3Fn: func(_ context.Context, payload map[string]any) (string, string, error) {
			fields := payload["fields"].(map[string]any)
			assert.Equal(t, "custom_val", fields["customfield_10001"])
			return "PROJ-2", "10002", nil
		},
	}
	withCreateMeta(mc, "Task")
	h := newWriteHandlers(mc)
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "create",
		Items: []WriteItem{{
			Project:    "PROJ",
			Summary:    "With custom",
			IssueType:  "Task",
			FieldsJSON: `{"customfield_10001": "custom_val"}`,
		}},
	})
	assert.False(t, isErr)
	assert.Contains(t, text, "Created PROJ-2")
}

func TestWriteCreate_InvalidFieldsJSON(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "create",
		Items: []WriteItem{{
			Project:    "PROJ",
			Summary:    "Bad json",
			IssueType:  "Task",
			FieldsJSON: "{bad}",
		}},
	})
	assert.False(t, isErr)
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "invalid fields_json")
}

// --- update ---

func TestWriteUpdate_Success(t *testing.T) {
	mc := &mockClient{
		UpdateIssueV3Fn: func(_ context.Context, key string, payload map[string]any) error {
			assert.Equal(t, "PROJ-1", key)
			fields := payload["fields"].(map[string]any)
			assert.Equal(t, "Updated title", fields["summary"])
			return nil
		},
	}
	h := newWriteHandlers(mc)
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "update",
		Items:  []WriteItem{{Key: "PROJ-1", Summary: "Updated title"}},
	})
	assert.False(t, isErr)
	assert.Contains(t, text, "Updated PROJ-1")
}

func TestWriteUpdate_MissingKey(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, _ := callWrite(t, h, WriteArgs{
		Action: "update",
		Items:  []WriteItem{{Summary: "no key"}},
	})
	assert.Contains(t, text, "update requires key")
}

func TestWriteUpdate_DryRun(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, _ := callWrite(t, h, WriteArgs{
		Action: "update",
		DryRun: true,
		Items:  []WriteItem{{Key: "PROJ-1", Summary: "s"}},
	})
	assert.Contains(t, text, "DRY RUN")
	assert.Contains(t, text, "Would update PROJ-1")
}

func TestWriteUpdate_ClientError(t *testing.T) {
	mc := &mockClient{
		UpdateIssueV3Fn: func(context.Context, string, map[string]any) error {
			return fmt.Errorf("not found")
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "update",
		Items:  []WriteItem{{Key: "PROJ-1", Summary: "s"}},
	})
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "not found")
}

// --- delete ---

func TestWriteDelete_Success(t *testing.T) {
	mc := &mockClient{
		DeleteIssueFn: func(_ context.Context, key string) error {
			assert.Equal(t, "PROJ-1", key)
			return nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "delete",
		Items:  []WriteItem{{Key: "PROJ-1"}},
	})
	assert.Contains(t, text, "Deleted PROJ-1")
}

func TestWriteDelete_MissingKey(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, _ := callWrite(t, h, WriteArgs{
		Action: "delete",
		Items:  []WriteItem{{}},
	})
	assert.Contains(t, text, "delete requires key")
}

func TestWriteDelete_DryRun(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, _ := callWrite(t, h, WriteArgs{
		Action: "delete",
		DryRun: true,
		Items:  []WriteItem{{Key: "PROJ-1"}},
	})
	assert.Contains(t, text, "DRY RUN")
	assert.Contains(t, text, "Would delete PROJ-1")
}

func TestWriteDelete_ClientError(t *testing.T) {
	mc := &mockClient{
		DeleteIssueFn: func(context.Context, string) error {
			return fmt.Errorf("forbidden")
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "delete",
		Items:  []WriteItem{{Key: "PROJ-1"}},
	})
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "forbidden")
}

// --- transition ---

func TestWriteTransition_Success(t *testing.T) {
	mc := &mockClient{
		DoTransitionFn: func(_ context.Context, key, tid string) error {
			assert.Equal(t, "PROJ-1", key)
			assert.Equal(t, "31", tid)
			return nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "transition",
		Items:  []WriteItem{{Key: "PROJ-1", TransitionID: "31"}},
	})
	assert.Contains(t, text, "Transitioned PROJ-1")
}

func TestWriteTransition_WithComment(t *testing.T) {
	mc := &mockClient{
		DoTransitionFn: func(context.Context, string, string) error { return nil },
		AddCommentFn: func(_ context.Context, key string, body any) (string, error) {
			assert.Equal(t, "PROJ-1", key)
			return "99", nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "transition",
		Items:  []WriteItem{{Key: "PROJ-1", TransitionID: "31", Comment: "Done"}},
	})
	assert.Contains(t, text, "Transitioned PROJ-1")
	assert.Contains(t, text, "Comment added")
}

func TestWriteTransition_CommentFails(t *testing.T) {
	mc := &mockClient{
		DoTransitionFn: func(context.Context, string, string) error { return nil },
		AddCommentFn: func(context.Context, string, any) (string, error) {
			return "", fmt.Errorf("comment boom")
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "transition",
		Items:  []WriteItem{{Key: "PROJ-1", TransitionID: "31", Comment: "oops"}},
	})
	assert.Contains(t, text, "Transitioned PROJ-1")
	assert.Contains(t, text, "comment failed")
}

func TestWriteTransition_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		item WriteItem
	}{
		{"no key", WriteItem{TransitionID: "31"}},
		{"no transition_id", WriteItem{Key: "PROJ-1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newWriteHandlers(&mockClient{})
			text, _ := callWrite(t, h, WriteArgs{
				Action: "transition",
				Items:  []WriteItem{tc.item},
			})
			assert.Contains(t, text, "transition requires key and transition_id")
		})
	}
}

func TestWriteTransition_DryRun(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, _ := callWrite(t, h, WriteArgs{
		Action: "transition",
		DryRun: true,
		Items:  []WriteItem{{Key: "PROJ-1", TransitionID: "31"}},
	})
	assert.Contains(t, text, "DRY RUN")
	assert.Contains(t, text, "Would transition PROJ-1")
}

func TestWriteTransition_DryRunWithComment(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, _ := callWrite(t, h, WriteArgs{
		Action: "transition",
		DryRun: true,
		Items:  []WriteItem{{Key: "PROJ-1", TransitionID: "31", Comment: "note"}},
	})
	assert.Contains(t, text, "Would also add a comment")
}

// --- comment ---

func TestWriteComment_Success(t *testing.T) {
	mc := &mockClient{
		AddCommentFn: func(_ context.Context, key string, body any) (string, error) {
			assert.Equal(t, "PROJ-1", key)
			return "200", nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "comment",
		Items:  []WriteItem{{Key: "PROJ-1", Comment: "Nice work"}},
	})
	assert.Contains(t, text, "Added comment to PROJ-1")
	assert.Contains(t, text, "comment_id=200")
}

func TestWriteComment_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		item WriteItem
	}{
		{"no key", WriteItem{Comment: "text"}},
		{"no comment", WriteItem{Key: "PROJ-1"}},
		{"both empty", WriteItem{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newWriteHandlers(&mockClient{})
			text, _ := callWrite(t, h, WriteArgs{
				Action: "comment",
				Items:  []WriteItem{tc.item},
			})
			assert.Contains(t, text, "comment requires key and comment")
		})
	}
}

func TestWriteComment_DryRun(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, _ := callWrite(t, h, WriteArgs{
		Action: "comment",
		DryRun: true,
		Items:  []WriteItem{{Key: "PROJ-1", Comment: "preview"}},
	})
	assert.Contains(t, text, "DRY RUN")
	assert.Contains(t, text, "Would add comment to PROJ-1")
}

// --- edit_comment ---

func TestWriteEditComment_Success(t *testing.T) {
	mc := &mockClient{
		UpdateCommentFn: func(_ context.Context, key, cid string, body any) error {
			assert.Equal(t, "PROJ-1", key)
			assert.Equal(t, "55", cid)
			return nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "edit_comment",
		Items:  []WriteItem{{Key: "PROJ-1", CommentID: "55", Comment: "edited"}},
	})
	assert.Contains(t, text, "Updated comment 55 on PROJ-1")
}

func TestWriteEditComment_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		item WriteItem
	}{
		{"no key", WriteItem{CommentID: "1", Comment: "x"}},
		{"no comment_id", WriteItem{Key: "P-1", Comment: "x"}},
		{"no comment", WriteItem{Key: "P-1", CommentID: "1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newWriteHandlers(&mockClient{})
			text, _ := callWrite(t, h, WriteArgs{
				Action: "edit_comment",
				Items:  []WriteItem{tc.item},
			})
			assert.Contains(t, text, "edit_comment requires key, comment_id, and comment")
		})
	}
}

func TestWriteEditComment_DryRun(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, _ := callWrite(t, h, WriteArgs{
		Action: "edit_comment",
		DryRun: true,
		Items:  []WriteItem{{Key: "PROJ-1", CommentID: "55", Comment: "new text"}},
	})
	assert.Contains(t, text, "DRY RUN")
	assert.Contains(t, text, "Would edit comment 55")
}

// --- move_to_sprint ---

func TestWriteMoveToSprint_Success(t *testing.T) {
	mc := &mockClient{
		MoveIssuesToSprintFn: func(_ context.Context, sid int, keys []string) error {
			assert.Equal(t, 42, sid)
			assert.Equal(t, []string{"PROJ-1"}, keys)
			return nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "move_to_sprint",
		Items:  []WriteItem{{Key: "PROJ-1", SprintID: 42}},
	})
	assert.Contains(t, text, "Moved")
	assert.Contains(t, text, "PROJ-1")
	assert.Contains(t, text, "sprint 42")
}

func TestWriteMoveToSprint_BatchSameSprint(t *testing.T) {
	var capturedKeys []string
	mc := &mockClient{
		MoveIssuesToSprintFn: func(_ context.Context, sid int, keys []string) error {
			assert.Equal(t, 42, sid)
			capturedKeys = keys
			return nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "move_to_sprint",
		Items: []WriteItem{
			{Key: "PROJ-1", SprintID: 42},
			{Key: "PROJ-2", SprintID: 42},
		},
	})
	// Should make exactly one API call with both keys.
	assert.Equal(t, []string{"PROJ-1", "PROJ-2"}, capturedKeys)
	assert.Contains(t, text, "PROJ-1")
	assert.Contains(t, text, "PROJ-2")
}

func TestWriteMoveToSprint_BatchDifferentSprints(t *testing.T) {
	calls := map[int][]string{}
	mc := &mockClient{
		MoveIssuesToSprintFn: func(_ context.Context, sid int, keys []string) error {
			calls[sid] = keys
			return nil
		},
	}
	h := newWriteHandlers(mc)
	callWrite(t, h, WriteArgs{
		Action: "move_to_sprint",
		Items: []WriteItem{
			{Key: "PROJ-1", SprintID: 10},
			{Key: "PROJ-2", SprintID: 20},
			{Key: "PROJ-3", SprintID: 10},
		},
	})
	assert.Equal(t, []string{"PROJ-1", "PROJ-3"}, calls[10])
	assert.Equal(t, []string{"PROJ-2"}, calls[20])
}

func TestWriteMoveToSprint_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		item WriteItem
	}{
		{"no key", WriteItem{SprintID: 42}},
		{"no sprint_id", WriteItem{Key: "PROJ-1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newWriteHandlers(&mockClient{})
			text, isErr := callWrite(t, h, WriteArgs{
				Action: "move_to_sprint",
				Items:  []WriteItem{tc.item},
			})
			assert.True(t, isErr)
			assert.Contains(t, text, "move_to_sprint requires key and sprint_id")
		})
	}
}

func TestWriteMoveToSprint_DryRun(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, _ := callWrite(t, h, WriteArgs{
		Action: "move_to_sprint",
		DryRun: true,
		Items:  []WriteItem{{Key: "PROJ-1", SprintID: 42}},
	})
	assert.Contains(t, text, "DRY RUN")
	assert.Contains(t, text, "Would move")
	assert.Contains(t, text, "PROJ-1")
	assert.Contains(t, text, "sprint 42")
}

// --- batch ---

func TestHandleWrite_Batch(t *testing.T) {
	createCalls := 0
	mc := &mockClient{
		CreateIssueV3Fn: func(context.Context, map[string]any) (string, string, error) {
			createCalls++
			return fmt.Sprintf("PROJ-%d", createCalls), "id", nil
		},
	}
	withCreateMeta(mc, "Task")
	h := newWriteHandlers(mc)
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "create",
		Items: []WriteItem{
			{Project: "PROJ", Summary: "One", IssueType: "Task"},
			{Project: "PROJ", Summary: "Two", IssueType: "Task"},
			{Project: "PROJ", Summary: "Three", IssueType: "Task"},
		},
	})
	assert.False(t, isErr)
	assert.Equal(t, 3, createCalls)
	assert.Contains(t, text, "[1]")
	assert.Contains(t, text, "[2]")
	assert.Contains(t, text, "[3]")
	assert.Contains(t, text, "3 item(s)")
}

func TestHandleWrite_BatchPartialFailure(t *testing.T) {
	call := 0
	mc := &mockClient{
		CreateIssueV3Fn: func(context.Context, map[string]any) (string, string, error) {
			call++
			if call == 2 {
				return "", "", fmt.Errorf("quota exceeded")
			}
			return fmt.Sprintf("PROJ-%d", call), "id", nil
		},
	}
	withCreateMeta(mc, "Task")
	h := newWriteHandlers(mc)
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "create",
		Items: []WriteItem{
			{Project: "PROJ", Summary: "One", IssueType: "Task"},
			{Project: "PROJ", Summary: "Two", IssueType: "Task"},
			{Project: "PROJ", Summary: "Three", IssueType: "Task"},
		},
	})
	assert.False(t, isErr)
	assert.Contains(t, text, "Created PROJ-1")
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "quota exceeded")
	assert.Contains(t, text, "Created PROJ-3")
}

// --- client errors for remaining actions ---

func TestWriteComment_ClientError(t *testing.T) {
	mc := &mockClient{
		AddCommentFn: func(context.Context, string, any) (string, error) {
			return "", fmt.Errorf("rate limited")
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "comment",
		Items:  []WriteItem{{Key: "PROJ-1", Comment: "hi"}},
	})
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "rate limited")
}

func TestWriteEditComment_ClientError(t *testing.T) {
	mc := &mockClient{
		UpdateCommentFn: func(context.Context, string, string, any) error {
			return fmt.Errorf("not found")
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "edit_comment",
		Items:  []WriteItem{{Key: "PROJ-1", CommentID: "55", Comment: "x"}},
	})
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "not found")
}

func TestWriteMoveToSprint_ClientError(t *testing.T) {
	mc := &mockClient{
		MoveIssuesToSprintFn: func(context.Context, int, []string) error {
			return fmt.Errorf("sprint not found")
		},
	}
	h := newWriteHandlers(mc)
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "move_to_sprint",
		Items:  []WriteItem{{Key: "PROJ-1", SprintID: 99}},
	})
	assert.False(t, isErr) // errors are per-sprint in the result text
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "sprint not found")
}

func TestWriteTransition_ClientError(t *testing.T) {
	mc := &mockClient{
		DoTransitionFn: func(context.Context, string, string) error {
			return fmt.Errorf("invalid transition")
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "transition",
		Items:  []WriteItem{{Key: "PROJ-1", TransitionID: "99"}},
	})
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "invalid transition")
}

// --- output format ---

func TestHandleWrite_OutputFormat(t *testing.T) {
	mc := &mockClient{
		DeleteIssueFn: func(context.Context, string) error { return nil },
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "delete",
		Items:  []WriteItem{{Key: "X-1"}},
	})
	assert.Contains(t, text, "Results (1 item(s), action=delete)")
}

func TestHandleWrite_DryRunLabel(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, _ := callWrite(t, h, WriteArgs{
		Action: "delete",
		DryRun: true,
		Items:  []WriteItem{{Key: "X-1"}},
	})
	assert.Contains(t, text, "DRY RUN")
}

// --- description/comment format opt-in ---

func TestHandleWrite_DescriptionFormat_Markdown(t *testing.T) {
	// Default (empty) format and explicit "markdown" must both produce ADF
	// and call the v3 endpoint, preserving existing behaviour.
	for _, format := range []string{"", "markdown"} {
		t.Run("format="+format, func(t *testing.T) {
			var payloadSeen map[string]any
			mc := &mockClient{
				UpdateIssueV3Fn: func(_ context.Context, key string, payload map[string]any) error {
					assert.Equal(t, "PROJ-1", key)
					payloadSeen = payload
					return nil
				},
			}
			h := newWriteHandlers(mc)
			text, _ := callWrite(t, h, WriteArgs{
				Action: "update",
				Items: []WriteItem{{
					Key:               "PROJ-1",
					Description:       "Hello **world**",
					DescriptionFormat: format,
				}},
			})
			assert.Contains(t, text, "Updated PROJ-1")

			fields := payloadSeen["fields"].(map[string]any)
			desc, ok := fields["description"].(map[string]any)
			require.True(t, ok, "description should be ADF map for markdown")
			assert.Equal(t, "doc", desc["type"])
		})
	}
}

func TestHandleWrite_DescriptionFormat_Wiki(t *testing.T) {
	var wikiPayload map[string]any
	mc := &mockClient{
		UpdateIssueV2Fn: func(_ context.Context, key string, payload map[string]any) error {
			assert.Equal(t, "PROJ-1", key)
			wikiPayload = payload
			return nil
		},
	}
	h := newWriteHandlers(mc)
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "update",
		Items: []WriteItem{{
			Key:               "PROJ-1",
			Description:       "{code:sql}select 1{code}",
			DescriptionFormat: "wiki",
		}},
	})
	assert.False(t, isErr)
	assert.Contains(t, text, "Updated PROJ-1")

	fields := wikiPayload["fields"].(map[string]any)
	// Wiki path must send the raw string, not an ADF map, to the v2 endpoint.
	assert.Equal(t, "{code:sql}select 1{code}", fields["description"])
}

func TestHandleWrite_CreateDescriptionFormat_Wiki(t *testing.T) {
	var wikiPayload map[string]any
	mc := &mockClient{
		CreateIssueV2Fn: func(_ context.Context, payload map[string]any) (string, string, error) {
			wikiPayload = payload
			return "PROJ-42", "42", nil
		},
	}
	withCreateMeta(mc, "Task")
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "create",
		Items: []WriteItem{{
			Project:           "PROJ",
			Summary:           "wiki create",
			IssueType:         "Task",
			Description:       "*bold* and h1. heading",
			DescriptionFormat: "wiki",
		}},
	})
	assert.Contains(t, text, "Created PROJ-42")

	fields := wikiPayload["fields"].(map[string]any)
	assert.Equal(t, "*bold* and h1. heading", fields["description"])
}

func TestHandleWrite_CommentFormat_Wiki(t *testing.T) {
	var capturedBody any
	mc := &mockClient{
		AddCommentV2Fn: func(_ context.Context, key string, body string) (string, error) {
			assert.Equal(t, "PROJ-1", key)
			capturedBody = body
			return "900", nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "comment",
		Items: []WriteItem{{
			Key:           "PROJ-1",
			Comment:       "{quote}hello{quote}",
			CommentFormat: "wiki",
		}},
	})
	assert.Contains(t, text, "Added comment to PROJ-1")
	assert.Contains(t, text, "comment_id=900")
	assert.Equal(t, "{quote}hello{quote}", capturedBody)
}

func TestHandleWrite_EditCommentFormat_Wiki(t *testing.T) {
	var capturedBody string
	mc := &mockClient{
		UpdateCommentV2Fn: func(_ context.Context, key, cid, body string) error {
			assert.Equal(t, "PROJ-1", key)
			assert.Equal(t, "55", cid)
			capturedBody = body
			return nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "edit_comment",
		Items: []WriteItem{{
			Key:           "PROJ-1",
			CommentID:     "55",
			Comment:       "h2. edited",
			CommentFormat: "wiki",
		}},
	})
	assert.Contains(t, text, "Updated comment 55 on PROJ-1")
	assert.Equal(t, "h2. edited", capturedBody)
}

func TestHandleWrite_DescriptionFormat_Unknown(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, _ := callWrite(t, h, WriteArgs{
		Action: "update",
		Items: []WriteItem{{
			Key:               "PROJ-1",
			Description:       "anything",
			DescriptionFormat: "bogus",
		}},
	})
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "description_format")
	assert.Contains(t, text, "bogus")
	assert.Contains(t, text, "markdown")
	assert.Contains(t, text, "wiki")
}

func TestHandleWrite_CommentFormat_Unknown(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, _ := callWrite(t, h, WriteArgs{
		Action: "comment",
		Items: []WriteItem{{
			Key:           "PROJ-1",
			Comment:       "anything",
			CommentFormat: "bogus",
		}},
	})
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "comment_format")
	assert.Contains(t, text, "bogus")
	assert.Contains(t, text, "markdown")
	assert.Contains(t, text, "wiki")
}

// --- wiki-markup rejection ---

// TestHandleWrite_DescriptionWithWikiMarkupIsRejected is the Phase 0 red test
// for the wiki-markup passthrough bug: today, wiki-markup in a description
// silently converts to a plain-text ADF doc that Jira renders as literal
// tokens. After the fix, default markdown writes must reject wiki-markup input
// with an actionable error naming the offending tokens and suggesting
// description_format="wiki" as the opt-out.
func TestHandleWrite_DescriptionWithWikiMarkupIsRejected(t *testing.T) {
	mc := &mockClient{
		// UpdateIssueV3Fn intentionally left unset — the handler must NOT reach
		// the wire. If it does, the mock panics with a clear message.
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "update",
		Items: []WriteItem{{
			Key:         "PROJ-1",
			Description: "{code:sql}select 1{code}\n\n{{inline}}\n\nh1. Heading",
		}},
	})

	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "wiki-markup")
	assert.Contains(t, text, "{code:sql}")
	assert.Contains(t, text, "{{inline}}")
	assert.Contains(t, text, "h1.")
	assert.Contains(t, text, `description_format="wiki"`)
}

// TestHandleWrite_AllowsWikiMarkupWhenFormatWiki verifies the opt-out works:
// the same wiki-markup input that is rejected on the default markdown path
// must succeed when DescriptionFormat="wiki", routing through the v2 endpoint
// with the raw string.
func TestHandleWrite_AllowsWikiMarkupWhenFormatWiki(t *testing.T) {
	var gotPayload map[string]any
	mc := &mockClient{
		UpdateIssueV2Fn: func(_ context.Context, key string, payload map[string]any) error {
			assert.Equal(t, "PROJ-1", key)
			gotPayload = payload
			return nil
		},
	}
	h := newWriteHandlers(mc)
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "update",
		Items: []WriteItem{{
			Key:               "PROJ-1",
			Description:       "{code:sql}select 1{code}\n\n{{inline}}\n\nh1. Heading",
			DescriptionFormat: "wiki",
		}},
	})
	assert.False(t, isErr)
	assert.Contains(t, text, "Updated PROJ-1")

	fields := gotPayload["fields"].(map[string]any)
	assert.Equal(t, "{code:sql}select 1{code}\n\n{{inline}}\n\nh1. Heading", fields["description"])
}

// TestHandleWrite_CommentRejectsWikiMarkupOnDefault mirrors the description
// rejection for the comment path.
func TestHandleWrite_CommentRejectsWikiMarkupOnDefault(t *testing.T) {
	mc := &mockClient{}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "comment",
		Items: []WriteItem{{
			Key:     "PROJ-1",
			Comment: "{quote}wiki{quote}",
		}},
	})
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "wiki-markup")
	assert.Contains(t, text, "{quote}")
	assert.Contains(t, text, `comment_format="wiki"`)
}

// --- description ADF in payload ---

func TestWriteCreate_DescriptionConvertsToADF(t *testing.T) {
	var capturedPayload map[string]any
	mc := &mockClient{
		CreateIssueV3Fn: func(_ context.Context, payload map[string]any) (string, string, error) {
			// Deep copy via JSON round-trip to capture
			b, _ := json.Marshal(payload)
			_ = json.Unmarshal(b, &capturedPayload)
			return "PROJ-1", "1", nil
		},
	}
	withCreateMeta(mc, "Task")
	h := newWriteHandlers(mc)
	callWrite(t, h, WriteArgs{
		Action: "create",
		Items: []WriteItem{{
			Project:     "PROJ",
			Summary:     "ADF test",
			IssueType:   "Task",
			Description: "# Heading\n\nParagraph",
		}},
	})

	fields := capturedPayload["fields"].(map[string]any)
	desc := fields["description"].(map[string]any)
	assert.Equal(t, float64(1), desc["version"])
	assert.Equal(t, "doc", desc["type"])

	content := desc["content"].([]any)
	require.GreaterOrEqual(t, len(content), 2)

	heading := content[0].(map[string]any)
	assert.Equal(t, "heading", heading["type"])
}

// --- preflight required fields ---

func TestWriteCreate_PreflightMissingRequiredFields(t *testing.T) {
	var createCalled bool
	mc := &mockClient{
		GetCreateMetaIssueTypesFn: func(_ context.Context, _ string) ([]jira.CreateMetaIssueType, error) {
			return []jira.CreateMetaIssueType{{ID: "10", Name: "Bug"}}, nil
		},
		GetCreateMetaFieldsFn: func(_ context.Context, _, _ string) ([]jira.CreateMetaField, error) {
			return []jira.CreateMetaField{
				{FieldID: "summary", Name: "Summary", Required: true},
				{FieldID: "issuetype", Name: "Issue Type", Required: true},
				{FieldID: "project", Name: "Project", Required: true},
				{FieldID: "customfield_10104", Name: "Kubernetes Environment", Required: true, AllowedValues: []map[string]any{
					{"value": "Production"}, {"value": "Staging"},
				}},
				{FieldID: "customfield_10101", Name: "Redpanda Version", Required: true},
			}, nil
		},
		CreateIssueV3Fn: func(context.Context, map[string]any) (string, string, error) {
			createCalled = true
			return "", "", fmt.Errorf("request failed. Status code: 400: customfield_10104 is required")
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "create",
		Items:  []WriteItem{{Project: "CON", Summary: "test", IssueType: "Bug"}},
	})
	// Request is sent to Jira despite missing fields; preflight warning is appended.
	assert.True(t, createCalled, "create request should be sent to Jira")
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "Preflight warning")
	assert.Contains(t, text, "missing required fields")
	assert.Contains(t, text, "Kubernetes Environment")
	assert.Contains(t, text, "customfield_10104")
	assert.Contains(t, text, "Production")
	assert.Contains(t, text, "Staging")
	assert.Contains(t, text, "Redpanda Version")
	assert.Contains(t, text, "customfield_10101")
}

func TestWriteCreate_PreflightPassesWithFieldsJSON(t *testing.T) {
	mc := &mockClient{
		GetCreateMetaIssueTypesFn: func(_ context.Context, _ string) ([]jira.CreateMetaIssueType, error) {
			return []jira.CreateMetaIssueType{{ID: "10", Name: "Bug"}}, nil
		},
		GetCreateMetaFieldsFn: func(_ context.Context, _, _ string) ([]jira.CreateMetaField, error) {
			return []jira.CreateMetaField{
				{FieldID: "summary", Name: "Summary", Required: true},
				{FieldID: "customfield_10104", Name: "K8s Env", Required: true},
			}, nil
		},
		CreateIssueV3Fn: func(context.Context, map[string]any) (string, string, error) {
			return "CON-1", "1", nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "create",
		Items: []WriteItem{{
			Project:    "CON",
			Summary:    "test",
			IssueType:  "Bug",
			FieldsJSON: `{"customfield_10104": {"value": "Production"}}`,
		}},
	})
	assert.Contains(t, text, "Created CON-1")
}

func TestWriteCreate_PreflightInvalidIssueType(t *testing.T) {
	var createCalled bool
	mc := &mockClient{
		GetCreateMetaIssueTypesFn: func(_ context.Context, _ string) ([]jira.CreateMetaIssueType, error) {
			return []jira.CreateMetaIssueType{
				{ID: "1", Name: "Task"},
				{ID: "2", Name: "Story"},
			}, nil
		},
		CreateIssueV3Fn: func(context.Context, map[string]any) (string, string, error) {
			createCalled = true
			return "", "", fmt.Errorf("request failed. Status code: 400: valid issue type is required")
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "create",
		Items:  []WriteItem{{Project: "P", Summary: "s", IssueType: "Bug"}},
	})
	// Request is sent to Jira; preflight warning about invalid type is appended.
	assert.True(t, createCalled, "create request should be sent to Jira")
	assert.Contains(t, text, "ERROR")
	assert.Contains(t, text, "Preflight warning")
	assert.Contains(t, text, "issue type \"Bug\" not found")
	assert.Contains(t, text, "Task")
	assert.Contains(t, text, "Story")
}

func TestWriteCreate_PreflightSkipsFieldsWithDefaults(t *testing.T) {
	mc := &mockClient{
		GetCreateMetaIssueTypesFn: func(_ context.Context, _ string) ([]jira.CreateMetaIssueType, error) {
			return []jira.CreateMetaIssueType{{ID: "1", Name: "Task"}}, nil
		},
		GetCreateMetaFieldsFn: func(_ context.Context, _, _ string) ([]jira.CreateMetaField, error) {
			return []jira.CreateMetaField{
				{FieldID: "summary", Name: "Summary", Required: true},
				{FieldID: "customfield_99", Name: "Has Default", Required: true, HasDefaultValue: true},
			}, nil
		},
		CreateIssueV3Fn: func(context.Context, map[string]any) (string, string, error) {
			return "P-1", "1", nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "create",
		Items:  []WriteItem{{Project: "P", Summary: "s", IssueType: "Task"}},
	})
	assert.Contains(t, text, "Created P-1")
}

func TestWriteCreate_PreflightMetaFetchFailsGracefully(t *testing.T) {
	mc := &mockClient{
		GetCreateMetaIssueTypesFn: func(_ context.Context, _ string) ([]jira.CreateMetaIssueType, error) {
			return nil, fmt.Errorf("403 forbidden")
		},
		CreateIssueV3Fn: func(context.Context, map[string]any) (string, string, error) {
			return "P-1", "1", nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "create",
		Items:  []WriteItem{{Project: "P", Summary: "s", IssueType: "Task"}},
	})
	// Should proceed despite metadata failure and succeed at API level.
	assert.Contains(t, text, "Created P-1")
}

func TestWriteCreate_PreflightWarningButJiraSucceeds(t *testing.T) {
	mc := &mockClient{
		GetCreateMetaIssueTypesFn: func(_ context.Context, _ string) ([]jira.CreateMetaIssueType, error) {
			// Stale metadata — type exists in Jira but not in createmeta response.
			return []jira.CreateMetaIssueType{{ID: "1", Name: "Task"}}, nil
		},
		CreateIssueV3Fn: func(context.Context, map[string]any) (string, string, error) {
			return "P-1", "1", nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "create",
		Items:  []WriteItem{{Project: "P", Summary: "s", IssueType: "Epic"}},
	})
	// Jira accepted the request despite preflight warning — success takes priority.
	assert.Contains(t, text, "Created P-1")
	assert.NotContains(t, text, "ERROR")
}

func TestWriteCreate_PreflightWarningInDryRun(t *testing.T) {
	mc := &mockClient{
		GetCreateMetaIssueTypesFn: func(_ context.Context, _ string) ([]jira.CreateMetaIssueType, error) {
			return []jira.CreateMetaIssueType{{ID: "10", Name: "Bug"}}, nil
		},
		GetCreateMetaFieldsFn: func(_ context.Context, _, _ string) ([]jira.CreateMetaField, error) {
			return []jira.CreateMetaField{
				{FieldID: "summary", Name: "Summary", Required: true},
				{FieldID: "customfield_10101", Name: "Redpanda Version", Required: true},
			}, nil
		},
	}
	h := newWriteHandlers(mc)
	text, _ := callWrite(t, h, WriteArgs{
		Action: "create",
		DryRun: true,
		Items:  []WriteItem{{Project: "P", Summary: "s", IssueType: "Bug"}},
	})
	// Dry run should show payload AND preflight warning.
	assert.Contains(t, text, "Would create issue")
	assert.Contains(t, text, "Preflight warning")
	assert.Contains(t, text, "Redpanda Version")
}
