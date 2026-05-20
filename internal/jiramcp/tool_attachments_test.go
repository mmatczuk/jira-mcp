package jiramcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func issueWithAttachments(atts []*jira.Attachment) *jira.Issue {
	return &jira.Issue{
		Fields: &jira.IssueFields{Attachments: atts},
	}
}

func TestAttachments_List(t *testing.T) {
	t.Run("two attachments", func(t *testing.T) {
		mc := &mockClient{
			GetIssueFn: func(_ context.Context, key string, opts *jira.GetQueryOptions) (*jira.Issue, error) {
				assert.Equal(t, "PROJ-1", key)
				require.NotNil(t, opts)
				assert.Equal(t, "attachment", opts.Fields)
				return issueWithAttachments([]*jira.Attachment{
					{ID: "10100", Filename: "a.log", MimeType: "text/plain", Size: 5, Created: "2025-03-12T10:23:45.000-0700", Author: &jira.User{DisplayName: "Alice"}},
					{ID: "10101", Filename: "b.json", MimeType: "application/json", Size: 9},
				}), nil
			},
		}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{Action: "list", Key: "PROJ-1"})
		require.False(t, isErr)

		var got []attachmentMeta
		require.NoError(t, json.Unmarshal([]byte(text), &got))
		require.Len(t, got, 2)
		assert.Equal(t, "10100", got[0].ID)
		assert.Equal(t, "a.log", got[0].Filename)
		assert.Equal(t, "text/plain", got[0].MimeType)
		assert.Equal(t, 5, got[0].Size)
		assert.Equal(t, "2025-03-12T10:23:45.000-0700", got[0].Created)
		assert.Equal(t, "Alice", got[0].Author)
		assert.Equal(t, "10101", got[1].ID)
		assert.Empty(t, got[1].Author)
	})

	t.Run("no attachments returns empty array", func(t *testing.T) {
		mc := &mockClient{
			GetIssueFn: func(_ context.Context, _ string, _ *jira.GetQueryOptions) (*jira.Issue, error) {
				return issueWithAttachments(nil), nil
			},
		}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{Action: "list", Key: "PROJ-1"})
		require.False(t, isErr)
		assert.Equal(t, "[]", text)
	})

	t.Run("404 propagated", func(t *testing.T) {
		mc := &mockClient{
			GetIssueFn: func(_ context.Context, _ string, _ *jira.GetQueryOptions) (*jira.Issue, error) {
				return nil, errors.New("404 not found")
			},
		}
		h := &handlers{client: mc}
		text, isErr := callAttachments(t, h, AttachmentsArgs{Action: "list", Key: "MISSING-1"})
		require.True(t, isErr)
		assert.Contains(t, text, "404")
	})
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

		var got attachmentMeta
		require.NoError(t, json.Unmarshal([]byte(text), &got))
		assert.Equal(t, "20001", got.ID)
		assert.Equal(t, "report.txt", got.Filename)
		assert.Equal(t, "text/plain", got.MimeType)
		assert.Equal(t, 5, got.Size)
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
// full-loop test; it lets a single mockClient stitch upload/list/download/delete
// together so a downstream call can see the effects of an upstream one.
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
	var uploaded attachmentMeta
	require.NoError(t, json.Unmarshal([]byte(uploadText), &uploaded))
	require.NotEmpty(t, uploaded.ID)

	listText, isErr := callAttachments(t, h, AttachmentsArgs{Action: "list", Key: "PROJ-1"})
	require.False(t, isErr)
	var listed []attachmentMeta
	require.NoError(t, json.Unmarshal([]byte(listText), &listed))
	require.Len(t, listed, 1)
	assert.Equal(t, uploaded.ID, listed[0].ID)

	downloadText, isErr := callAttachments(t, h, AttachmentsArgs{
		Action: "download", AttachmentID: uploaded.ID,
	})
	require.False(t, isErr)
	assert.Equal(t, "hello", downloadText)

	deleteText, isErr := callAttachments(t, h, AttachmentsArgs{
		Action: "delete", AttachmentID: uploaded.ID,
	})
	require.False(t, isErr)
	assert.Contains(t, deleteText, uploaded.ID)

	listText, isErr = callAttachments(t, h, AttachmentsArgs{Action: "list", Key: "PROJ-1"})
	require.False(t, isErr)
	require.NoError(t, json.Unmarshal([]byte(listText), &listed))
	assert.Empty(t, listed)
}

func TestAttachments_ActionValidation(t *testing.T) {
	cases := []struct {
		name        string
		args        AttachmentsArgs
		errContains string
	}{
		{"missing action", AttachmentsArgs{}, "action"},
		{"unknown action", AttachmentsArgs{Action: "ship"}, "action"},
		{"list missing key", AttachmentsArgs{Action: "list"}, "key"},
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
