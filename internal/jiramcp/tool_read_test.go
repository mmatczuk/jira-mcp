package jiramcp

import (
	"context"
	"fmt"
	"testing"
	"time"

	jira "github.com/andygrunwald/go-jira"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		GetIssueFn: func(_ context.Context, key string, _ *jira.GetQueryOptions) (*jira.Issue, error) {
			return &jira.Issue{
				Key:    key,
				Fields: &jira.IssueFields{Summary: "Issue " + key},
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{Keys: []string{"A-1", "B-2"}})
	assert.False(t, isErr)
	assert.Contains(t, text, "Fetched 2 issue(s)")
}

func TestReadByKeys_PartialError(t *testing.T) {
	mc := &mockClient{
		GetIssueFn: func(_ context.Context, key string, _ *jira.GetQueryOptions) (*jira.Issue, error) {
			if key == "BAD-1" {
				return nil, fmt.Errorf("not found")
			}
			return &jira.Issue{
				Key:    key,
				Fields: &jira.IssueFields{Summary: "ok"},
			}, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{Keys: []string{"GOOD-1", "BAD-1"}})
	assert.False(t, isErr)
	assert.Contains(t, text, "Fetched 1 issue(s)")
	assert.Contains(t, text, "BAD-1")
	assert.Contains(t, text, "not found")
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
		SearchIssuesFn: func(_ context.Context, jql string, opts *jira.SearchOptions) ([]jira.Issue, int, error) {
			assert.Equal(t, "project = PROJ", jql)
			assert.Equal(t, 100, opts.MaxResults) // default
			return []jira.Issue{
				{Key: "PROJ-1", Fields: &jira.IssueFields{Summary: "One"}},
				{Key: "PROJ-2", Fields: &jira.IssueFields{Summary: "Two"}},
			}, 2, nil
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
		SearchIssuesFn: func(_ context.Context, _ string, opts *jira.SearchOptions) ([]jira.Issue, int, error) {
			assert.Equal(t, 10, opts.MaxResults)
			assert.Equal(t, 0, opts.StartAt)
			return []jira.Issue{
				{Key: "P-1", Fields: &jira.IssueFields{Summary: "x"}},
			}, 50, nil
		},
	}
	h := &handlers{client: mc}
	text, _ := callRead(t, h, ReadArgs{JQL: "project = P", Limit: 10})
	assert.Contains(t, text, "start_at=10") // pagination hint
}

func TestReadByJQL_WithFields(t *testing.T) {
	mc := &mockClient{
		SearchIssuesFn: func(_ context.Context, _ string, opts *jira.SearchOptions) ([]jira.Issue, int, error) {
			assert.Equal(t, []string{"summary", "status"}, opts.Fields)
			return nil, 0, nil
		},
	}
	h := &handlers{client: mc}
	callRead(t, h, ReadArgs{JQL: "x", Fields: "summary,status"})
}

func TestReadByJQL_ClientError(t *testing.T) {
	mc := &mockClient{
		SearchIssuesFn: func(context.Context, string, *jira.SearchOptions) ([]jira.Issue, int, error) {
			return nil, 0, fmt.Errorf("invalid JQL")
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
		Self: "https://jira.example.com/rest/api/2/issue/10001",
		Fields: &jira.IssueFields{
			Summary:     "Test issue",
			Status:      &jira.Status{Name: "In Progress"},
			Type:        jira.IssueType{Name: "Bug"},
			Assignee:    &jira.User{DisplayName: "Alice"},
			Priority:    &jira.Priority{Name: "High"},
			Description: "A description",
			Labels:      []string{"backend"},
			Created:     jira.Time(created),
			Updated:     jira.Time(updated),
		},
	}

	m := issueToMap(issue)
	assert.Equal(t, "PROJ-1", m["key"])
	assert.Equal(t, "10001", m["id"])

	fields := m["fields"].(map[string]any)
	assert.Equal(t, "Test issue", fields["summary"])
	assert.Equal(t, "In Progress", fields["status"])
	assert.Equal(t, "Bug", fields["type"])
	assert.Equal(t, "Alice", fields["assignee"])
	assert.Equal(t, "High", fields["priority"])
	assert.Equal(t, "A description", fields["description"])
	assert.Equal(t, []string{"backend"}, fields["labels"])
	assert.Equal(t, created.Format(time.RFC3339), fields["created"])
	assert.Equal(t, updated.Format(time.RFC3339), fields["updated"])
}

func TestIssueToMap_NilFields(t *testing.T) {
	issue := &jira.Issue{Key: "X-1", ID: "1"}
	m := issueToMap(issue)
	assert.Equal(t, "X-1", m["key"])
	_, hasFields := m["fields"]
	assert.False(t, hasFields)
}

func TestIssueToMap_MinimalFields(t *testing.T) {
	issue := &jira.Issue{
		Key: "X-1",
		Fields: &jira.IssueFields{
			Summary: "Only summary",
		},
	}
	m := issueToMap(issue)
	fields := m["fields"].(map[string]any)
	assert.Equal(t, "Only summary", fields["summary"])
	_, hasStatus := fields["status"]
	assert.False(t, hasStatus)
	_, hasAssignee := fields["assignee"]
	assert.False(t, hasAssignee)
}

// --- default limit ---

func TestHandleRead_DefaultLimit(t *testing.T) {
	mc := &mockClient{
		SearchIssuesFn: func(_ context.Context, _ string, opts *jira.SearchOptions) ([]jira.Issue, int, error) {
			assert.Equal(t, 100, opts.MaxResults)
			return nil, 0, nil
		},
	}
	h := &handlers{client: mc}
	callRead(t, h, ReadArgs{JQL: "x"})
}
