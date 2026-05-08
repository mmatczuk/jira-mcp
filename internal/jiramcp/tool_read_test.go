package jiramcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmatczuk/jira-mcp/internal/jira"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/trivago/tgo/tcontainer"
)

func callRead(t *testing.T, h *handlers, args ReadArgs) (string, bool) {
	t.Helper()
	result, _, err := h.handleRead(context.Background(), nil, args)
	require.NoError(t, err)
	text := result.Content[0].(*mcp.TextContent).Text
	return text, result.IsError
}

// --- mode validation ---

func TestHandleRead_NoMode(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	text, isErr := callRead(t, h, ReadArgs{})
	assert.True(t, isErr)
	assert.Contains(t, text, "Provide exactly one of")
}

func TestHandleRead_MultipleModes(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	text, isErr := callRead(t, h, ReadArgs{
		Keys: []string{"X-1"},
		JQL:  "project = X",
	})
	assert.True(t, isErr)
	assert.Contains(t, text, "not multiple")
}

// --- readByKeys ---

func TestReadByKeys_Success(t *testing.T) {
	mc := &mockClient{
		GetIssueFn: func(_ context.Context, key string, _ *jira.GetQueryOptions) (*jira.Issue, error) {
			return &jira.Issue{
				Key: key,
				ID:  "10001",
				Fields: &jira.IssueFields{
					Summary: "Test issue",
					Status:  &jira.Status{Name: "Open"},
					Type:    jira.IssueType{Name: "Bug"},
				},
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{Keys: []string{"PROJ-1"}})
	assert.False(t, isErr)
	assert.Contains(t, text, "Fetched 1 issue(s)")
	assert.Contains(t, text, "PROJ-1")
	assert.Contains(t, text, "Test issue")
}

func TestReadByKeys_PassesFieldsAndExpand(t *testing.T) {
	mc := &mockClient{
		GetIssueFn: func(_ context.Context, _ string, opts *jira.GetQueryOptions) (*jira.Issue, error) {
			assert.Equal(t, "summary,status", opts.Fields)
			assert.Equal(t, "changelog", opts.Expand)
			return &jira.Issue{Key: "P-1", Fields: &jira.IssueFields{Summary: "x"}}, nil
		},
	}
	h := &handlers{client: mc}
	callRead(t, h, ReadArgs{Keys: []string{"P-1"}, Fields: "summary,status", Expand: "changelog"})
}

func TestReadByKeys_MultipleKeys(t *testing.T) {
	mc := &mockClient{
		SearchIssuesFn: func(_ context.Context, jql string, opts *jira.SearchOptionsV3) (*jira.SearchResultV3, error) {
			assert.Contains(t, jql, "issueKey in")
			assert.Contains(t, jql, "A-1")
			assert.Contains(t, jql, "B-2")
			return &jira.SearchResultV3{
				Issues: []jira.Issue{
					{Key: "A-1", Fields: &jira.IssueFields{Summary: "Issue A-1"}},
					{Key: "B-2", Fields: &jira.IssueFields{Summary: "Issue B-2"}},
				},
				Total: 2,
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{Keys: []string{"A-1", "B-2"}})
	assert.False(t, isErr)
	assert.Contains(t, text, "Fetched 2 issue(s)")
}

func TestReadByKeys_PartialError(t *testing.T) {
	// With 2 keys, the JQL path is used; JIRA simply omits unknown keys.
	mc := &mockClient{
		SearchIssuesFn: func(_ context.Context, _ string, _ *jira.SearchOptionsV3) (*jira.SearchResultV3, error) {
			// JIRA returns only the issues that exist; BAD-1 is silently absent.
			return &jira.SearchResultV3{
				Issues: []jira.Issue{
					{Key: "GOOD-1", Fields: &jira.IssueFields{Summary: "ok"}},
				},
				Total: 1,
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{Keys: []string{"GOOD-1", "BAD-1"}})
	assert.False(t, isErr)
	assert.Contains(t, text, "Fetched 1 issue(s)")
	assert.Contains(t, text, "GOOD-1")
}

func TestReadByKeys_AllError(t *testing.T) {
	mc := &mockClient{
		GetIssueFn: func(context.Context, string, *jira.GetQueryOptions) (*jira.Issue, error) {
			return nil, fmt.Errorf("server error")
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{Keys: []string{"X-1"}})
	assert.False(t, isErr)
	assert.Contains(t, text, "Fetched 0 issue(s)")
	assert.Contains(t, text, "server error")
}

// --- readByJQL ---

func TestReadByJQL_Success(t *testing.T) {
	mc := &mockClient{
		SearchIssuesFn: func(_ context.Context, jql string, opts *jira.SearchOptionsV3) (*jira.SearchResultV3, error) {
			assert.Equal(t, "project = PROJ", jql)
			assert.Equal(t, 100, opts.MaxResults) // default
			return &jira.SearchResultV3{
				Issues: []jira.Issue{
					{Key: "PROJ-1", Fields: &jira.IssueFields{Summary: "One"}},
					{Key: "PROJ-2", Fields: &jira.IssueFields{Summary: "Two"}},
				},
				Total: 2,
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{JQL: "project = PROJ"})
	assert.False(t, isErr)
	assert.Contains(t, text, "Found 2 issue(s)")
	assert.Contains(t, text, "PROJ-1")
}

func TestReadByJQL_Pagination(t *testing.T) {
	mc := &mockClient{
		SearchIssuesFn: func(_ context.Context, _ string, opts *jira.SearchOptionsV3) (*jira.SearchResultV3, error) {
			assert.Equal(t, 10, opts.MaxResults)
			return &jira.SearchResultV3{
				Issues: []jira.Issue{
					{Key: "P-1", Fields: &jira.IssueFields{Summary: "x"}},
				},
				Total:         50,
				NextPageToken: "abc123",
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, _ := callRead(t, h, ReadArgs{JQL: "project = P", Limit: 10})
	assert.Contains(t, text, "next_page_token=")
	assert.Contains(t, text, "abc123")
}

func TestReadByJQL_WithFields(t *testing.T) {
	mc := &mockClient{
		SearchIssuesFn: func(_ context.Context, _ string, opts *jira.SearchOptionsV3) (*jira.SearchResultV3, error) {
			assert.Equal(t, []string{"summary", "status"}, opts.Fields)
			return &jira.SearchResultV3{}, nil
		},
	}
	h := &handlers{client: mc}
	callRead(t, h, ReadArgs{JQL: "x", Fields: "summary,status"})
}

func TestReadByJQL_WithNextPageToken(t *testing.T) {
	mc := &mockClient{
		SearchIssuesFn: func(_ context.Context, _ string, opts *jira.SearchOptionsV3) (*jira.SearchResultV3, error) {
			assert.Equal(t, "tok123", opts.NextPageToken)
			return &jira.SearchResultV3{}, nil
		},
	}
	h := &handlers{client: mc}
	callRead(t, h, ReadArgs{JQL: "project = X", NextPageToken: "tok123"})
}

func TestReadByJQL_ClientError(t *testing.T) {
	mc := &mockClient{
		SearchIssuesFn: func(context.Context, string, *jira.SearchOptionsV3) (*jira.SearchResultV3, error) {
			return nil, fmt.Errorf("invalid JQL")
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{JQL: "bad query"})
	assert.True(t, isErr)
	assert.Contains(t, text, "JQL search failed")
	assert.Contains(t, text, "invalid JQL")
}

// --- readResource: projects ---

func TestReadProjects_Success(t *testing.T) {
	pl := jira.ProjectList{
		{Key: "PROJ", Name: "Project One", ID: "1"},
		{Key: "OTHER", Name: "Other", ID: "2"},
	}
	mc := &mockClient{
		GetAllProjectsFn: func(context.Context) (*jira.ProjectList, error) {
			return &pl, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{Resource: "projects"})
	assert.False(t, isErr)
	assert.Contains(t, text, "Found 2 project(s)")
	assert.Contains(t, text, "PROJ")
}

func TestReadProjects_Error(t *testing.T) {
	mc := &mockClient{
		GetAllProjectsFn: func(context.Context) (*jira.ProjectList, error) {
			return nil, fmt.Errorf("auth fail")
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{Resource: "projects"})
	assert.True(t, isErr)
	assert.Contains(t, text, "auth fail")
}

// --- readResource: boards ---

func TestReadBoards_Success(t *testing.T) {
	mc := &mockClient{
		GetAllBoardsFn: func(_ context.Context, opts *jira.BoardListOptions) ([]jira.Board, bool, error) {
			return []jira.Board{
				{ID: 1, Name: "Sprint Board", Type: "scrum"},
			}, true, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{Resource: "boards"})
	assert.False(t, isErr)
	assert.Contains(t, text, "Found 1 board(s)")
	assert.Contains(t, text, "Sprint Board")
}

func TestReadBoards_Filters(t *testing.T) {
	mc := &mockClient{
		GetAllBoardsFn: func(_ context.Context, opts *jira.BoardListOptions) ([]jira.Board, bool, error) {
			assert.Equal(t, "PROJ", opts.ProjectKeyOrID)
			assert.Equal(t, "My Board", opts.Name)
			assert.Equal(t, "scrum", opts.BoardType)
			return nil, true, nil
		},
	}
	h := &handlers{client: mc}
	callRead(t, h, ReadArgs{
		Resource:   "boards",
		ProjectKey: "PROJ",
		BoardName:  "My Board",
		BoardType:  "scrum",
	})
}

func TestReadBoards_MorePages(t *testing.T) {
	mc := &mockClient{
		GetAllBoardsFn: func(context.Context, *jira.BoardListOptions) ([]jira.Board, bool, error) {
			return []jira.Board{{ID: 1, Name: "B"}}, false, nil
		},
	}
	h := &handlers{client: mc}
	text, _ := callRead(t, h, ReadArgs{Resource: "boards", Limit: 10})
	assert.Contains(t, text, "start_at=10")
}

// --- readResource: sprints ---

func TestReadSprints_Success(t *testing.T) {
	mc := &mockClient{
		GetAllSprintsFn: func(_ context.Context, boardID int, opts *jira.GetAllSprintsOptions) ([]jira.Sprint, bool, error) {
			assert.Equal(t, 5, boardID)
			return []jira.Sprint{
				{ID: 10, Name: "Sprint 1", State: "active"},
			}, true, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{Resource: "sprints", BoardID: 5})
	assert.False(t, isErr)
	assert.Contains(t, text, "Found 1 sprint(s)")
	assert.Contains(t, text, "Sprint 1")
}

func TestReadSprints_NoBoardID(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	text, isErr := callRead(t, h, ReadArgs{Resource: "sprints"})
	assert.True(t, isErr)
	assert.Contains(t, text, "board_id is required")
}

func TestReadSprints_WithStateFilter(t *testing.T) {
	mc := &mockClient{
		GetAllSprintsFn: func(_ context.Context, _ int, opts *jira.GetAllSprintsOptions) ([]jira.Sprint, bool, error) {
			assert.Equal(t, "active", opts.State)
			return nil, true, nil
		},
	}
	h := &handlers{client: mc}
	callRead(t, h, ReadArgs{Resource: "sprints", BoardID: 1, SprintState: "active"})
}

// --- readResource: sprint_issues ---

func TestReadSprintIssues_Success(t *testing.T) {
	mc := &mockClient{
		GetSprintIssuesFn: func(_ context.Context, sprintID int) ([]jira.Issue, error) {
			assert.Equal(t, 10, sprintID)
			return []jira.Issue{
				{Key: "P-1", Fields: &jira.IssueFields{Summary: "task"}},
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{Resource: "sprint_issues", SprintID: 10})
	assert.False(t, isErr)
	assert.Contains(t, text, "Found 1 issue(s) in sprint 10")
}

func TestReadSprintIssues_NoSprintID(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	text, isErr := callRead(t, h, ReadArgs{Resource: "sprint_issues"})
	assert.True(t, isErr)
	assert.Contains(t, text, "sprint_id is required")
}

// --- readResource: unknown ---

func TestReadResource_Unknown(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	text, isErr := callRead(t, h, ReadArgs{Resource: "widgets"})
	assert.True(t, isErr)
	assert.Contains(t, text, `Unknown resource "widgets"`)
}

// --- issueToMap ---

func TestIssueToMap_AllFields(t *testing.T) {
	created := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2025, 1, 16, 12, 0, 0, 0, time.UTC)

	issue := &jira.Issue{
		Key:  "PROJ-1",
		ID:   "10001",
		Self: "https://jirahttp.example.com/rest/api/2/issue/10001",
		Fields: &jira.IssueFields{
			Summary:     "Test issue",
			Status:      &jira.Status{Name: "In Progress"},
			Type:        jira.IssueType{Name: "Bug"},
			Assignee:    &jira.User{DisplayName: "Alice", AccountID: "abc123"},
			Priority:    &jira.Priority{Name: "High"},
			Description: "A description",
			Labels:      []string{"backend"},
			Created:     jira.Time(created),
			Updated:     jira.Time(updated),
		},
	}

	m := issueToMap(issue, nil)
	assert.Equal(t, "PROJ-1", m["key"])
	assert.Equal(t, "10001", m["id"])

	fields := m["fields"].(map[string]any)
	assert.Equal(t, "Test issue", fields["summary"])
	assert.Equal(t, "In Progress", fields["status"])
	assert.Equal(t, "Bug", fields["type"])
	assert.Equal(t, map[string]any{"displayName": "Alice", "accountId": "abc123"}, fields["assignee"])
	assert.Equal(t, "High", fields["priority"])
	assert.Equal(t, "A description", fields["description"])
	assert.Equal(t, []string{"backend"}, fields["labels"])
	assert.Equal(t, created.Format(time.RFC3339), fields["created"])
	assert.Equal(t, updated.Format(time.RFC3339), fields["updated"])
}

func TestIssueToMap_NilFields(t *testing.T) {
	issue := &jira.Issue{Key: "X-1", ID: "1"}
	m := issueToMap(issue, nil)
	assert.Equal(t, "X-1", m["key"])
	_, hasFields := m["fields"]
	assert.False(t, hasFields)
}

// sampleADFDoc returns a minimal ADF doc carrying the given paragraph text.
// Shared across tests that exercise customfield_* values shaped like ADF.
func sampleADFDoc(text string) map[string]any {
	return map[string]any{
		"version": float64(1),
		"type":    "doc",
		"content": []any{
			map[string]any{
				"type":    "paragraph",
				"content": []any{map[string]any{"type": "text", "text": text}},
			},
		},
	}
}

func TestIssueToMap_IncludesCustomFields(t *testing.T) {
	adfDoc := sampleADFDoc("hello")

	cases := []struct {
		name        string
		unknowns    tcontainer.MarshalMap
		wantPresent map[string]any
		wantAbsent  []string
	}{
		{
			name:        "ADF object passes through verbatim",
			unknowns:    tcontainer.MarshalMap{"customfield_10001": adfDoc},
			wantPresent: map[string]any{"customfield_10001": adfDoc},
		},
		{
			name:        "primitive value passes through verbatim",
			unknowns:    tcontainer.MarshalMap{"customfield_10042": "ABC-123"},
			wantPresent: map[string]any{"customfield_10042": "ABC-123"},
		},
		{
			name:       "non-customfield_ keys are not surfaced",
			unknowns:   tcontainer.MarshalMap{"someInternalKey": "leak"},
			wantAbsent: []string{"someInternalKey"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue := &jira.Issue{
				Key:    "PROJ-1",
				Fields: &jira.IssueFields{Summary: "s", Unknowns: tc.unknowns},
			}
			fields := issueToMap(issue, nil)["fields"].(map[string]any)
			for k, v := range tc.wantPresent {
				assert.Equal(t, v, fields[k])
			}
			for _, k := range tc.wantAbsent {
				_, present := fields[k]
				assert.False(t, present, "expected key %q to be absent", k)
			}
		})
	}
}

func TestReadByKeys_MultiKey_IncludesCustomFields(t *testing.T) {
	mc := &mockClient{
		SearchIssuesFn: func(context.Context, string, *jira.SearchOptionsV3) (*jira.SearchResultV3, error) {
			return &jira.SearchResultV3{
				Issues: []jira.Issue{
					{Key: "A-1", Fields: &jira.IssueFields{
						Summary:  "a",
						Unknowns: tcontainer.MarshalMap{"customfield_10001": "value-a"},
					}},
					{Key: "B-2", Fields: &jira.IssueFields{
						Summary:  "b",
						Unknowns: tcontainer.MarshalMap{"customfield_10001": "value-b"},
					}},
				},
				Total: 2,
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{Keys: []string{"A-1", "B-2"}})
	assert.False(t, isErr)
	assert.Contains(t, text, `"customfield_10001":"value-a"`)
	assert.Contains(t, text, `"customfield_10001":"value-b"`)
}

func TestIssueToMap_MinimalFields(t *testing.T) {
	issue := &jira.Issue{
		Key: "X-1",
		Fields: &jira.IssueFields{
			Summary: "Only summary",
		},
	}
	m := issueToMap(issue, nil)
	fields := m["fields"].(map[string]any)
	assert.Equal(t, "Only summary", fields["summary"])
	_, hasStatus := fields["status"]
	assert.False(t, hasStatus)
	_, hasAssignee := fields["assignee"]
	assert.False(t, hasAssignee)
}

// --- field_format ---

func TestRead_FieldFormat_HandlesADFAndPassThrough(t *testing.T) {
	const fieldID = "customfield_10001"
	const textfieldCustom = "com.atlassian.jira.plugin.system.customfieldtypes:textfield"
	adfDoc := sampleADFDoc("hello")

	cases := []struct {
		name        string
		schema      jira.FieldSchema
		fieldsKnown bool // whether GetFields lists the field
		value       any
		want        any
	}{
		{
			name:        "converts textarea ADF doc to Markdown",
			schema:      jira.FieldSchema{Custom: textareaTypeKey},
			fieldsKnown: true,
			value:       adfDoc,
			want:        "hello",
		},
		{
			name:        "preserves non-textarea custom-field value",
			schema:      jira.FieldSchema{Custom: textfieldCustom},
			fieldsKnown: true,
			value:       "ABC-123",
			want:        "ABC-123",
		},
		{
			name:        "preserves textarea field whose value is not an ADF doc",
			schema:      jira.FieldSchema{Custom: textareaTypeKey},
			fieldsKnown: true,
			value:       "raw string somehow",
			want:        "raw string somehow",
		},
		{
			name:        "passes through fields absent from the catalogue",
			schema:      jira.FieldSchema{},
			fieldsKnown: false,
			value:       adfDoc,
			want:        adfDoc,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mc := &mockClient{
				GetFieldsFn: func(context.Context) ([]jira.Field, error) {
					if !tc.fieldsKnown {
						return []jira.Field{}, nil
					}
					return []jira.Field{{ID: fieldID, Schema: tc.schema}}, nil
				},
				GetIssueFn: func(context.Context, string, *jira.GetQueryOptions) (*jira.Issue, error) {
					return &jira.Issue{
						Key: "K-1",
						Fields: &jira.IssueFields{
							Summary:  "s",
							Unknowns: tcontainer.MarshalMap{fieldID: tc.value},
						},
					}, nil
				},
			}
			h := &handlers{client: mc}
			text, isErr := callRead(t, h, ReadArgs{Keys: []string{"K-1"}, FieldFormat: "markdown"})
			require.False(t, isErr)

			// Decode the JSON tail of the response and find our field.
			idx := strings.Index(text, "[")
			require.NotEqual(t, -1, idx)
			var entries []map[string]any
			require.NoError(t, json.Unmarshal([]byte(text[idx:]), &entries))
			require.Len(t, entries, 1)
			fields := entries[0]["fields"].(map[string]any)
			assert.Equal(t, tc.want, fields[fieldID])
		})
	}
}

func TestRead_FieldFormatRaw_MakesNoSchemaLookups(t *testing.T) {
	var fieldsCalls atomic.Int32
	mc := &mockClient{
		GetFieldsFn: func(context.Context) ([]jira.Field, error) {
			fieldsCalls.Add(1)
			return nil, nil
		},
		GetIssueFn: func(context.Context, string, *jira.GetQueryOptions) (*jira.Issue, error) {
			return &jira.Issue{
				Key:    "K-1",
				Fields: &jira.IssueFields{Summary: "s"},
			}, nil
		},
	}
	h := &handlers{client: mc}
	for _, format := range []string{"", "raw"} {
		t.Run("format="+format, func(t *testing.T) {
			fieldsCalls.Store(0)
			_, isErr := callRead(t, h, ReadArgs{Keys: []string{"K-1"}, FieldFormat: format})
			require.False(t, isErr)
			assert.EqualValues(t, 0, fieldsCalls.Load(), "raw mode must not call GetFields")
		})
	}
}

func TestRead_FieldFormat_InvalidValueReturnsError(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	text, isErr := callRead(t, h, ReadArgs{Keys: []string{"K-1"}, FieldFormat: "wiki"})
	assert.True(t, isErr)
	assert.Contains(t, text, "field_format")
	assert.Contains(t, text, "wiki")
}

func TestRead_FieldFormatMarkdown_BatchSharesCache(t *testing.T) {
	const fieldID = "customfield_10001"
	var fieldsCalls atomic.Int32
	mc := &mockClient{
		GetFieldsFn: func(context.Context) ([]jira.Field, error) {
			fieldsCalls.Add(1)
			return []jira.Field{{ID: fieldID, Schema: jira.FieldSchema{Custom: textareaTypeKey}}}, nil
		},
		SearchIssuesFn: func(context.Context, string, *jira.SearchOptionsV3) (*jira.SearchResultV3, error) {
			return &jira.SearchResultV3{
				Issues: []jira.Issue{
					{Key: "A-1", Fields: &jira.IssueFields{Summary: "a", Unknowns: tcontainer.MarshalMap{fieldID: sampleADFDoc("first")}}},
					{Key: "B-2", Fields: &jira.IssueFields{Summary: "b", Unknowns: tcontainer.MarshalMap{fieldID: sampleADFDoc("second")}}},
				},
				Total: 2,
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{Keys: []string{"A-1", "B-2"}, FieldFormat: "markdown"})
	require.False(t, isErr)
	assert.Contains(t, text, `"customfield_10001":"first"`)
	assert.Contains(t, text, `"customfield_10001":"second"`)
	assert.EqualValues(t, 1, fieldsCalls.Load(), "GetFields must be called once across the batch")
}

// --- default limit ---

func TestHandleRead_DefaultLimit(t *testing.T) {
	mc := &mockClient{
		SearchIssuesFn: func(_ context.Context, _ string, opts *jira.SearchOptionsV3) (*jira.SearchResultV3, error) {
			assert.Equal(t, 100, opts.MaxResults)
			return &jira.SearchResultV3{}, nil
		},
	}
	h := &handlers{client: mc}
	callRead(t, h, ReadArgs{JQL: "x"})
}
