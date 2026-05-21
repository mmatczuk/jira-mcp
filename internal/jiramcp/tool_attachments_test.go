package jiramcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mmatczuk/jira-mcp/internal/jira"
)

func callAttachments(t *testing.T, h *handlers, args AttachmentsArgs) (string, bool) {
	t.Helper()
	res, _, err := h.handleAttachments(context.Background(), &mcp.CallToolRequest{}, args)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Content, 1)
	tc, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected TextContent, got %T", res.Content[0])
	return tc.Text, res.IsError
}

func assertNoUploadCalls(t *testing.T, mc *mockClient) {
	t.Helper()
	assert.Zero(t, mc.PostAttachmentTextCount, "expected PostAttachmentText not to be called")
}

func TestAttachments_Upload(t *testing.T) {
	t.Run("valid text", func(t *testing.T) {
		var gotKey, gotFilename, gotBody string
		mc := &mockClient{
			PostAttachmentTextFn: func(_ context.Context, key, filename, body string) (*jira.Attachment, error) {
				gotKey, gotFilename, gotBody = key, filename, body
				return &jira.Attachment{ID: "20001", Filename: filename, MimeType: "text/plain", Size: len(body)}, nil
			},
		}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{
			Action: "upload", Key: "PROJ-1", Filename: "report.txt", Content: "hello",
		})
		require.False(t, isErr)
		assert.Equal(t, "PROJ-1", gotKey)
		assert.Equal(t, "report.txt", gotFilename)
		assert.Equal(t, "hello", gotBody)

		var got map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &got))
		assert.Equal(t, "20001", got["id"])
		assert.Equal(t, "report.txt", got["filename"])
		assert.Equal(t, "text/plain", got["mime_type"])
		assert.EqualValues(t, 5, got["size"])
	})

	t.Run("binary filename rejected before call", func(t *testing.T) {
		mc := &mockClient{}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{
			Action: "upload", Key: "PROJ-1", Filename: "image.png", Content: "irrelevant",
		})
		require.True(t, isErr)
		assert.Contains(t, text, "image/png")
		assertNoUploadCalls(t, mc)
	})

	t.Run("NUL byte rejected before call", func(t *testing.T) {
		mc := &mockClient{}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{
			Action: "upload", Key: "PROJ-1", Filename: "report.txt", Content: "hello\x00world",
		})
		require.True(t, isErr)
		assert.Contains(t, text, "binary content")
		assertNoUploadCalls(t, mc)
	})

	t.Run("body sniffs to binary rejected before call", func(t *testing.T) {
		mc := &mockClient{}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{
			Action: "upload", Key: "PROJ-1", Filename: "report.txt",
			Content: string([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}),
		})
		require.True(t, isErr)
		assert.Contains(t, text, "binary content")
		assertNoUploadCalls(t, mc)
	})

	t.Run("upstream error propagated", func(t *testing.T) {
		mc := &mockClient{
			PostAttachmentTextFn: func(_ context.Context, _, _, _ string) (*jira.Attachment, error) {
				return nil, errors.New("boom")
			},
		}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{
			Action: "upload", Key: "PROJ-1", Filename: "report.txt", Content: "hello",
		})
		require.True(t, isErr)
		assert.Contains(t, text, "boom")
	})
}

func assertNoBodyFetch(t *testing.T, mc *mockClient) {
	t.Helper()
	assert.Zero(t, mc.GetAttachmentBodyCount, "expected GetAttachmentBody not to be called")
}

func TestAttachments_Download(t *testing.T) {
	t.Run("text in-cap", func(t *testing.T) {
		mc := &mockClient{
			GetAttachmentMetaFn: func(_ context.Context, id string) (*jira.Attachment, error) {
				assert.Equal(t, "10100", id)
				return &jira.Attachment{ID: id, Filename: "a.txt", MimeType: "text/plain", Size: 5}, nil
			},
			GetAttachmentBodyFn: func(_ context.Context, id string, maxBytes int64) ([]byte, error) {
				assert.Equal(t, "10100", id)
				assert.Equal(t, attachmentMaxBytes, maxBytes)
				return []byte("hello"), nil
			},
		}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{Action: "download", AttachmentID: "10100"})
		require.False(t, isErr)
		assert.Equal(t, "hello", text)
	})

	t.Run("binary mime rejected before body fetch", func(t *testing.T) {
		mc := &mockClient{
			GetAttachmentMetaFn: func(_ context.Context, _ string) (*jira.Attachment, error) {
				return &jira.Attachment{ID: "10100", Filename: "x.png", MimeType: "image/png", Size: 5}, nil
			},
		}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{Action: "download", AttachmentID: "10100"})
		require.True(t, isErr)
		assert.Contains(t, text, "image/png")
		assertNoBodyFetch(t, mc)
	})

	t.Run("declared text but body lies", func(t *testing.T) {
		pngHeader := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
		mc := &mockClient{
			GetAttachmentMetaFn: func(_ context.Context, _ string) (*jira.Attachment, error) {
				return &jira.Attachment{ID: "10100", Filename: "x.txt", MimeType: "text/plain", Size: 8}, nil
			},
			GetAttachmentBodyFn: func(_ context.Context, _ string, _ int64) ([]byte, error) {
				return pngHeader, nil
			},
		}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{Action: "download", AttachmentID: "10100"})
		require.True(t, isErr)
		assert.Contains(t, text, "binary content")
	})

	t.Run("over cap names sentinel", func(t *testing.T) {
		mc := &mockClient{
			GetAttachmentMetaFn: func(_ context.Context, _ string) (*jira.Attachment, error) {
				return &jira.Attachment{ID: "10100", Filename: "big.txt", MimeType: "text/plain", Size: 9999999}, nil
			},
			GetAttachmentBodyFn: func(_ context.Context, id string, maxBytes int64) ([]byte, error) {
				return nil, fmt.Errorf("attachment %s exceeds cap of %d bytes: %w", id, maxBytes, jira.ErrAttachmentTooLarge)
			},
		}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{Action: "download", AttachmentID: "10100"})
		require.True(t, isErr)
		assert.Contains(t, text, "exceeds")
	})

	t.Run("meta error propagated", func(t *testing.T) {
		mc := &mockClient{
			GetAttachmentMetaFn: func(_ context.Context, _ string) (*jira.Attachment, error) {
				return nil, errors.New("404 not found")
			},
		}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{Action: "download", AttachmentID: "missing"})
		require.True(t, isErr)
		assert.Contains(t, text, "404")
		assertNoBodyFetch(t, mc)
	})
}

func TestAttachments_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var gotID string
		mc := &mockClient{
			DeleteAttachmentFn: func(_ context.Context, id string) error {
				gotID = id
				return nil
			},
		}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{Action: "delete", AttachmentID: "10100"})
		require.False(t, isErr)
		assert.Equal(t, "10100", gotID)
		assert.Contains(t, text, "10100")
	})

	t.Run("error propagated", func(t *testing.T) {
		mc := &mockClient{
			DeleteAttachmentFn: func(_ context.Context, _ string) error {
				return errors.New("boom")
			},
		}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{Action: "delete", AttachmentID: "10100"})
		require.True(t, isErr)
		assert.Contains(t, text, "boom")
	})
}

// statefulAttachmentStore is an in-memory backing store used only by the
// full-loop test; it lets a single mockClient stitch upload/download/delete
// together (and the jira_read attachment surfacing) so a downstream call
// can see the effects of an upstream one.
type statefulAttachmentStore struct {
	atts   map[string]*jira.Attachment
	bodies map[string][]byte
	issue  string
	nextID int
}

func newStatefulAttachmentStore(issue string) *statefulAttachmentStore {
	return &statefulAttachmentStore{
		atts:   map[string]*jira.Attachment{},
		bodies: map[string][]byte{},
		issue:  issue,
		nextID: 30000,
	}
}

func (s *statefulAttachmentStore) install(mc *mockClient) {
	mc.GetIssueFn = func(_ context.Context, key string, _ *jira.GetQueryOptions) (*jira.Issue, error) {
		if key != s.issue {
			return nil, fmt.Errorf("issue %s not found", key)
		}
		atts := make([]*jira.Attachment, 0, len(s.atts))
		for _, a := range s.atts {
			atts = append(atts, a)
		}
		return &jira.Issue{Fields: &jira.IssueFields{Attachments: atts}}, nil
	}
	mc.PostAttachmentTextFn = func(_ context.Context, key, filename, body string) (*jira.Attachment, error) {
		if key != s.issue {
			return nil, fmt.Errorf("issue %s not found", key)
		}
		s.nextID++
		id := fmt.Sprintf("%d", s.nextID)
		att := &jira.Attachment{ID: id, Filename: filename, MimeType: "text/plain", Size: len(body)}
		s.atts[id] = att
		s.bodies[id] = []byte(body)
		return att, nil
	}
	mc.GetAttachmentMetaFn = func(_ context.Context, id string) (*jira.Attachment, error) {
		att, ok := s.atts[id]
		if !ok {
			return nil, fmt.Errorf("attachment %s not found", id)
		}
		return att, nil
	}
	mc.GetAttachmentBodyFn = func(_ context.Context, id string, _ int64) ([]byte, error) {
		b, ok := s.bodies[id]
		if !ok {
			return nil, fmt.Errorf("attachment %s not found", id)
		}
		return b, nil
	}
	mc.DeleteAttachmentFn = func(_ context.Context, id string) error {
		if _, ok := s.atts[id]; !ok {
			return fmt.Errorf("attachment %s not found", id)
		}
		delete(s.atts, id)
		delete(s.bodies, id)
		return nil
	}
}

func TestAttachments_FullLoop(t *testing.T) {
	store := newStatefulAttachmentStore("PROJ-1")
	mc := &mockClient{}
	store.install(mc)
	h := &handlers{client: mc}

	uploadText, isErr := callAttachments(t, h, AttachmentsArgs{
		Action: "upload", Key: "PROJ-1", Filename: "report.txt", Content: "hello",
	})
	require.False(t, isErr, "upload: %s", uploadText)
	var uploaded map[string]any
	require.NoError(t, json.Unmarshal([]byte(uploadText), &uploaded))
	uploadedID, _ := uploaded["id"].(string)
	require.NotEmpty(t, uploadedID)

	atts := readIssueAttachments(t, h, "PROJ-1")
	require.Len(t, atts, 1)
	assert.Equal(t, uploadedID, atts[0]["id"])

	downloadText, isErr := callAttachments(t, h, AttachmentsArgs{
		Action: "download", AttachmentID: uploadedID,
	})
	require.False(t, isErr)
	assert.Equal(t, "hello", downloadText)

	deleteText, isErr := callAttachments(t, h, AttachmentsArgs{
		Action: "delete", AttachmentID: uploadedID,
	})
	require.False(t, isErr)
	assert.Contains(t, deleteText, uploadedID)

	atts = readIssueAttachments(t, h, "PROJ-1")
	assert.Empty(t, atts)
}

// readIssueAttachments runs jira_read for a single key and pulls the
// attachments array off the projected issue, returning nil when absent.
func readIssueAttachments(t *testing.T, h *handlers, key string) []map[string]any {
	t.Helper()
	text, isErr := callRead(t, h, ReadArgs{Keys: []string{key}})
	require.False(t, isErr, "jira_read: %s", text)
	idx := strings.Index(text, "[")
	require.GreaterOrEqual(t, idx, 0, "no JSON payload in jira_read output: %s", text)
	var issues []map[string]any
	require.NoError(t, json.Unmarshal([]byte(text[idx:]), &issues))
	require.Len(t, issues, 1)
	fields, _ := issues[0]["fields"].(map[string]any)
	raw, ok := fields["attachments"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, v := range raw {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func TestAttachments_ActionValidation(t *testing.T) {
	cases := []struct {
		name        string
		args        AttachmentsArgs
		errContains string
	}{
		{"missing action", AttachmentsArgs{}, "action"},
		{"unknown action", AttachmentsArgs{Action: "ship"}, "action"},
		{"upload missing key", AttachmentsArgs{Action: "upload", Filename: "f.txt", Content: "x"}, "key"},
		{"upload missing filename", AttachmentsArgs{Action: "upload", Key: "P-1", Content: "x"}, "filename"},
		{"upload missing content", AttachmentsArgs{Action: "upload", Key: "P-1", Filename: "f.txt"}, "content"},
		{"download missing attachment_id", AttachmentsArgs{Action: "download"}, "attachment_id"},
		{"delete missing attachment_id", AttachmentsArgs{Action: "delete"}, "attachment_id"},
	}
	h := &handlers{client: &mockClient{}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, isErr := callAttachments(t, h, tc.args)
			assert.True(t, isErr, "expected isError=true")
			assert.Contains(t, text, tc.errContains)
		})
	}
}
