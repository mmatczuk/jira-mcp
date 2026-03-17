package jiramcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	payload, err := buildIssuePayload(item)
	require.NoError(t, err)

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
	payload, err := buildIssuePayload(WriteItem{})
	require.NoError(t, err)

	fields := payload["fields"].(map[string]any)
	assert.Empty(t, fields)
}

func TestBuildIssuePayload_FieldsJSON_Valid(t *testing.T) {
	item := WriteItem{
		Summary:    "s",
		FieldsJSON: `{"customfield_10001": "hello", "customfield_10002": 42}`,
	}

	payload, err := buildIssuePayload(item)
	require.NoError(t, err)

	fields := payload["fields"].(map[string]any)
	assert.Equal(t, "hello", fields["customfield_10001"])
	assert.Equal(t, float64(42), fields["customfield_10002"])
	assert.Equal(t, "s", fields["summary"])
}

func TestBuildIssuePayload_FieldsJSON_Invalid(t *testing.T) {
	item := WriteItem{FieldsJSON: "not json"}

	_, err := buildIssuePayload(item)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid fields_json")
}

func TestBuildIssuePayload_FieldsJSON_OverridesStandard(t *testing.T) {
	item := WriteItem{
		Summary:    "original",
		FieldsJSON: `{"summary": "overridden"}`,
	}

	payload, err := buildIssuePayload(item)
	require.NoError(t, err)

	fields := payload["fields"].(map[string]any)
	assert.Equal(t, "overridden", fields["summary"])
}

// --- buildCommentBody ---

func TestBuildCommentBody_Markdown(t *testing.T) {
	body := buildCommentBody("Hello **world**")
	m, ok := body.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 1, m["version"])
	assert.Equal(t, "doc", m["type"])
}

func TestBuildCommentBody_EmptyFallback(t *testing.T) {
	body := buildCommentBody("")
	m, ok := body.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "doc", m["type"])
	content := m["content"].([]any)
	para := content[0].(map[string]any)
	assert.Equal(t, "paragraph", para["type"])
}

// --- handleWrite dispatch & validation ---

func newWriteHandlers(mc *mockClient) *handlers {
	return &handlers{client: mc}
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
	assert.False(t, isErr)
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
	h := newWriteHandlers(&mockClient{})
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
	assert.Contains(t, text, "Moved PROJ-1 to sprint 42")
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
			text, _ := callWrite(t, h, WriteArgs{
				Action: "move_to_sprint",
				Items:  []WriteItem{tc.item},
			})
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
	assert.Contains(t, text, "Would move PROJ-1 to sprint 42")
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
	text, _ := callWrite(t, h, WriteArgs{
		Action: "move_to_sprint",
		Items:  []WriteItem{{Key: "PROJ-1", SprintID: 99}},
	})
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
