package jiramcp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mmatczuk/jira-mcp/internal/jira"
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

	m := issueToMap(issue)
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

func TestIssueToMap_Attachments(t *testing.T) {
	issue := &jira.Issue{
		Key: "PROJ-1",
		Fields: &jira.IssueFields{
			Summary: "x",
			Attachments: []*jira.Attachment{
				{
					ID:       "10100",
					Filename: "build.log",
					Size:     1234,
					MimeType: "text/plain",
					Created:  "2025-03-12T10:23:45.000-0700",
					Author:   &jira.User{DisplayName: "Alice", AccountID: "u-1"},
				},
				{
					ID:       "10101",
					Filename: "report.csv",
					Size:     56,
					MimeType: "text/csv",
					Created:  "not-a-real-timestamp",
				},
			},
		},
	}

	m := issueToMap(issue)
	fields := m["fields"].(map[string]any)

	atts, ok := fields["attachment"].([]map[string]any)
	require.True(t, ok, "attachment should be a []map[string]any")
	require.Len(t, atts, 2)

	assert.Equal(t, "10100", atts[0]["id"])
	assert.Equal(t, "build.log", atts[0]["filename"])
	assert.Equal(t, 1234, atts[0]["size"])
	assert.Equal(t, "text/plain", atts[0]["mime_type"])
	// Parseable Jira timestamp → RFC3339.
	assert.Equal(t, "2025-03-12T10:23:45-07:00", atts[0]["created"])
	assert.Equal(t, map[string]any{"displayName": "Alice", "accountId": "u-1"}, atts[0]["author"])

	// Unparseable timestamp passes through verbatim; author absent.
	assert.Equal(t, "not-a-real-timestamp", atts[1]["created"])
	_, hasAuthor := atts[1]["author"]
	assert.False(t, hasAuthor)
}

func TestIssueToMap_NoAttachments_KeyOmitted(t *testing.T) {
	issue := &jira.Issue{
		Key:    "PROJ-2",
		Fields: &jira.IssueFields{Summary: "x"},
	}
	m := issueToMap(issue)
	fields := m["fields"].(map[string]any)
	_, hasAttachments := fields["attachment"]
	assert.False(t, hasAttachments)
}

func TestIssueToMap_EmptyAttachmentsSlice_KeyOmitted(t *testing.T) {
	issue := &jira.Issue{
		Key: "PROJ-3",
		Fields: &jira.IssueFields{
			Summary:     "x",
			Attachments: []*jira.Attachment{},
		},
	}
	m := issueToMap(issue)
	fields := m["fields"].(map[string]any)
	_, hasAttachments := fields["attachment"]
	assert.False(t, hasAttachments)
}

// --- readAttachment ---

func TestReadAttachment_HappyPath(t *testing.T) {
	cases := []struct {
		name         string
		filename     string
		mime         string
		body         []byte
		bodyContains string
	}{
		{"text/plain", "notes.txt", "text/plain", []byte("hello, world\n"), "hello, world"},
		{"application/json", "x.json", "application/json", []byte(`{"ok":true}`), `"ok":true`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const id = "10100"
			mc := &mockClient{
				GetAttachmentMetaFn: func(_ context.Context, gotID string) (*jira.Attachment, error) {
					assert.Equal(t, id, gotID)
					return &jira.Attachment{ID: id, Filename: tc.filename, MimeType: tc.mime, Size: len(tc.body)}, nil
				},
				GetAttachmentBodyFn: func(_ context.Context, gotID string, max int64) ([]byte, error) {
					assert.Equal(t, id, gotID)
					assert.EqualValues(t, attachmentMaxBytes, max)
					return tc.body, nil
				},
			}
			h := &handlers{client: mc}
			text, isErr := callRead(t, h, ReadArgs{AttachmentID: id})
			assert.False(t, isErr)
			assert.Contains(t, text, tc.filename)
			assert.Contains(t, text, tc.mime)
			assert.Contains(t, text, tc.bodyContains)
		})
	}
}

func TestReadAttachment_RejectsDeclaredBinaryMime_NoBodyCall(t *testing.T) {
	bodyCalled := false
	mc := &mockClient{
		GetAttachmentMetaFn: func(context.Context, string) (*jira.Attachment, error) {
			return &jira.Attachment{ID: "1", Filename: "shot.png", MimeType: "image/png", Size: 1024}, nil
		},
		GetAttachmentBodyFn: func(context.Context, string, int64) ([]byte, error) {
			bodyCalled = true
			return nil, nil
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{AttachmentID: "1"})
	assert.True(t, isErr)
	assert.False(t, bodyCalled, "GetAttachmentBody must not be called when declared mime is non-text")
	assert.Contains(t, text, "image/png")
	assert.Contains(t, text, "text attachments only")
}

func TestReadAttachment_RejectsBinaryBody(t *testing.T) {
	// Declared mime passes the allowlist but the bytes are binary. Both
	// the sniff check (PNG signature) and the NUL-byte check should fire.
	pngHeader := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}
	cases := []struct {
		name     string
		filename string
		body     []byte
	}{
		{"PNG signature in body", "actually.png", pngHeader},
		{"NUL byte in body", "weird.log", []byte("hello\x00world")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mc := &mockClient{
				GetAttachmentMetaFn: func(context.Context, string) (*jira.Attachment, error) {
					return &jira.Attachment{ID: "1", Filename: tc.filename, MimeType: "text/plain", Size: len(tc.body)}, nil
				},
				GetAttachmentBodyFn: func(context.Context, string, int64) ([]byte, error) { return tc.body, nil },
			}
			h := &handlers{client: mc}
			text, isErr := callRead(t, h, ReadArgs{AttachmentID: "1"})
			assert.True(t, isErr)
			assert.Contains(t, text, "binary content")
		})
	}
}

func TestReadAttachment_OverCap_PropagatesSentinel(t *testing.T) {
	mc := &mockClient{
		GetAttachmentMetaFn: func(context.Context, string) (*jira.Attachment, error) {
			return &jira.Attachment{ID: "1", Filename: "huge.log", MimeType: "text/plain", Size: 99999999}, nil
		},
		GetAttachmentBodyFn: func(context.Context, string, int64) ([]byte, error) {
			return nil, fmt.Errorf("attachment 1: %w", jira.ErrAttachmentTooLarge)
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{AttachmentID: "1"})
	assert.True(t, isErr)
	assert.Contains(t, text, "exceeds")
	assert.Contains(t, text, "huge.log")
}

func TestReadAttachment_MutualExclusion(t *testing.T) {
	cases := []struct {
		name string
		args ReadArgs
	}{
		{"with keys", ReadArgs{Keys: []string{"X-1"}, AttachmentID: "1"}},
		{"with jql", ReadArgs{JQL: "project = X", AttachmentID: "1"}},
		{"with resource", ReadArgs{Resource: "projects", AttachmentID: "1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &handlers{client: &mockClient{}}
			text, isErr := callRead(t, h, tc.args)
			assert.True(t, isErr)
			assert.Contains(t, text, "not multiple")
		})
	}
}

func TestReadAttachment_MetaErrorPropagates(t *testing.T) {
	mc := &mockClient{
		GetAttachmentMetaFn: func(context.Context, string) (*jira.Attachment, error) {
			return nil, fmt.Errorf("404 not found")
		},
	}
	h := &handlers{client: mc}
	text, isErr := callRead(t, h, ReadArgs{AttachmentID: "missing"})
	assert.True(t, isErr)
	assert.Contains(t, text, "404 not found")
}

// --- validateTextAttachment ---

func TestValidateTextAttachment_Accepts(t *testing.T) {
	cases := []struct {
		name string
		mime string
		body []byte
	}{
		{"text/plain", "text/plain", []byte("hello")},
		{"text/plain with charset", "text/plain; charset=utf-8", []byte("hello")},
		{"application/json", "application/json", []byte(`{"a":1}`)},
		{"application/xml", "application/xml", []byte("<root/>")},
		{"empty mime, bytes look text", "", []byte("just plain text")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, validateTextAttachment(tc.mime, tc.body))
		})
	}
}

func TestValidateTextAttachment_Rejects(t *testing.T) {
	pngHeader := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}
	cases := []struct {
		name           string
		mime           string
		body           []byte
		errContains    string
	}{
		{"declared binary mime", "image/png", []byte("ignored"), "image/png"},
		{"declared text but NUL byte in body", "text/plain", []byte("hello\x00world"), "binary content"},
		{"declared text but PNG signature in body", "text/plain", pngHeader, "binary content"},
		{"empty mime, bytes look binary", "", pngHeader[:8], "binary content"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTextAttachment(tc.mime, tc.body)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
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
