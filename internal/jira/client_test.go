package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gojira "github.com/andygrunwald/go-jira"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeBody(s string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(s))
}

// newTestClient creates a Client pointed at the given test server URL.
func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	cfg := Config{
		URL:        serverURL,
		Email:      "test@example.com",
		APIToken:   "token",
		MaxRetries: 3,
		BaseDelay:  time.Millisecond, // fast tests
	}
	c, err := New(cfg)
	require.NoError(t, err)
	return c
}

// --- shouldRetry ---

func TestShouldRetry_429(t *testing.T) {
	c := &Client{cfg: Config{}}
	resp := &gojira.Response{Response: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
	}}
	delay, ok := c.shouldRetry(resp)
	assert.True(t, ok)
	assert.Equal(t, time.Duration(0), delay)
}

func TestShouldRetry_429_WithRetryAfter(t *testing.T) {
	c := &Client{cfg: Config{}}
	h := http.Header{}
	h.Set("Retry-After", "5")
	resp := &gojira.Response{Response: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     h,
	}}
	delay, ok := c.shouldRetry(resp)
	assert.True(t, ok)
	assert.Equal(t, 5*time.Second, delay)
}

func TestShouldRetry_502(t *testing.T) {
	c := &Client{cfg: Config{}}
	resp := &gojira.Response{Response: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{},
	}}
	_, ok := c.shouldRetry(resp)
	assert.True(t, ok)
}

func TestShouldRetry_503(t *testing.T) {
	c := &Client{cfg: Config{}}
	resp := &gojira.Response{Response: &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{},
	}}
	_, ok := c.shouldRetry(resp)
	assert.True(t, ok)
}

func TestShouldRetry_200(t *testing.T) {
	c := &Client{cfg: Config{}}
	resp := &gojira.Response{Response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
	}}
	_, ok := c.shouldRetry(resp)
	assert.False(t, ok)
}

func TestShouldRetry_500(t *testing.T) {
	c := &Client{cfg: Config{}}
	resp := &gojira.Response{Response: &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     http.Header{},
	}}
	_, ok := c.shouldRetry(resp)
	assert.False(t, ok)
}

func TestShouldRetry_Nil(t *testing.T) {
	c := &Client{cfg: Config{}}
	_, ok := c.shouldRetry(nil)
	assert.False(t, ok)
}

// --- backoff ---

func TestBackoff_UsesRetryAfter(t *testing.T) {
	c := &Client{cfg: Config{BaseDelay: time.Second}}
	d := c.backoff(0, 10*time.Second)
	assert.Equal(t, 10*time.Second, d)
}

func TestBackoff_Exponential(t *testing.T) {
	c := &Client{cfg: Config{BaseDelay: 100 * time.Millisecond}}
	assert.Equal(t, 100*time.Millisecond, c.backoff(0, 0))
	assert.Equal(t, 200*time.Millisecond, c.backoff(1, 0))
	assert.Equal(t, 400*time.Millisecond, c.backoff(2, 0))
}

// --- enrichError ---

func TestEnrichError_NilResponse(t *testing.T) {
	orig := fmt.Errorf("original")
	err := enrichError(nil, orig)
	assert.Equal(t, orig, err)
}

func TestEnrichError_WithJIRABody(t *testing.T) {
	body := `{"errorMessages":["Issue does not exist"],"errors":{"project":"required"}}`
	resp := &gojira.Response{Response: &http.Response{
		Body: http.NoBody,
	}}
	// Manually set a body reader.
	resp.Body = makeBody(body)

	orig := fmt.Errorf("400 Bad Request")
	err := enrichError(resp, orig)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Issue does not exist")
	assert.Contains(t, err.Error(), "project: required")
}

func TestEnrichError_NonJSONBody(t *testing.T) {
	resp := &gojira.Response{Response: &http.Response{}}
	resp.Body = makeBody("not json")
	orig := fmt.Errorf("original")
	err := enrichError(resp, orig)
	assert.Equal(t, orig, err)
}

func TestEnrichError_EmptyJIRAError(t *testing.T) {
	resp := &gojira.Response{Response: &http.Response{}}
	resp.Body = makeBody(`{"errorMessages":[],"errors":{}}`)
	orig := fmt.Errorf("original")
	err := enrichError(resp, orig)
	assert.Equal(t, orig, err)
}

// --- retry integration via httptest ---

func TestRetry_SucceedsOn429ThenOK(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "10001", "key": "PROJ-1"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	key, _, err := c.CreateIssueV3(context.Background(), map[string]any{
		"fields": map[string]any{"summary": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, "PROJ-1", key)
	assert.Equal(t, 3, calls)
}

func TestRetry_ExhaustsMaxRetries(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	c.cfg.MaxRetries = 2
	_, _, err := c.CreateIssueV3(context.Background(), map[string]any{})
	require.Error(t, err)
	assert.Equal(t, 3, calls) // initial + 2 retries
}

func TestRetry_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	c.cfg.BaseDelay = 60 * time.Second // would block forever without cancel

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, _, err := c.CreateIssueV3(ctx, map[string]any{})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRetry_EnrichesErrorWithFieldDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["something went wrong"],"errors":{"description":"INVALID_INPUT"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, _, err := c.CreateIssueV3(context.Background(), map[string]any{
		"fields": map[string]any{"summary": "test"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "something went wrong")
	assert.Contains(t, err.Error(), "description: INVALID_INPUT")
}

func TestRetry_DoesNotRetry500(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, _, err := c.CreateIssueV3(context.Background(), map[string]any{})
	require.Error(t, err)
	assert.Equal(t, 1, calls) // no retry
}

func TestRetry_RetriesOn503(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "1", "key": "P-1"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	key, _, err := c.CreateIssueV3(context.Background(), map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "P-1", key)
	assert.Equal(t, 2, calls)
}

// --- v2 (wiki-markup) endpoints ---

func TestCreateIssueV2_HappyPath(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "10001", "key": "PROJ-7"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	key, id, err := c.CreateIssueV2(context.Background(), map[string]any{
		"fields": map[string]any{"summary": "s", "description": "*wiki*"},
	})
	require.NoError(t, err)
	assert.Equal(t, "PROJ-7", key)
	assert.Equal(t, "10001", id)
	assert.Equal(t, "/rest/api/2/issue", gotPath)
	assert.Equal(t, "POST", gotMethod)

	fields := gotBody["fields"].(map[string]any)
	assert.Equal(t, "*wiki*", fields["description"])
}

func TestUpdateIssueV2_HappyPath(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.UpdateIssueV2(context.Background(), "PROJ-1", map[string]any{
		"fields": map[string]any{"description": "h1. wiki"},
	})
	require.NoError(t, err)
	assert.Equal(t, "/rest/api/2/issue/PROJ-1", gotPath)
	assert.Equal(t, "PUT", gotMethod)
}

func TestUpdateIssueV2_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.UpdateIssueV2(context.Background(), "MISSING-1", map[string]any{})
	require.Error(t, err)
}

func TestAddCommentV2_HappyPath(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "555"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	id, err := c.AddCommentV2(context.Background(), "PROJ-1", "{code}wiki{code}")
	require.NoError(t, err)
	assert.Equal(t, "555", id)
	assert.Equal(t, "/rest/api/2/issue/PROJ-1/comment", gotPath)
	assert.Equal(t, "POST", gotMethod)
	assert.Equal(t, "{code}wiki{code}", gotBody["body"])
}

func TestUpdateCommentV2_HappyPath(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "99"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.UpdateCommentV2(context.Background(), "PROJ-1", "99", "h2. edited")
	require.NoError(t, err)
	assert.Equal(t, "/rest/api/2/issue/PROJ-1/comment/99", gotPath)
	assert.Equal(t, "PUT", gotMethod)
	assert.Equal(t, "h2. edited", gotBody["body"])
}

// --- Issue link types ---

func TestIssueLinkType_JSONUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		json string
		want IssueLinkType
	}{
		{
			name: "blocks",
			json: `{"id":"10000","name":"Blocks","inward":"is blocked by","outward":"blocks"}`,
			want: IssueLinkType{ID: "10000", Name: "Blocks", Inward: "is blocked by", Outward: "blocks"},
		},
		{
			name: "relates",
			json: `{"id":"10001","name":"Relates","inward":"relates to","outward":"relates to"}`,
			want: IssueLinkType{ID: "10001", Name: "Relates", Inward: "relates to", Outward: "relates to"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got IssueLinkType
			require.NoError(t, json.Unmarshal([]byte(tc.json), &got))
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- GetIssueLinkTypes ---

func TestGetIssueLinkTypes_Success(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issueLinkTypes":[
			{"id":"10000","name":"Blocks","inward":"is blocked by","outward":"blocks"},
			{"id":"10001","name":"Relates","inward":"relates to","outward":"relates to"}
		]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	got, err := c.GetIssueLinkTypes(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "/rest/api/3/issueLinkType", gotPath)
	assert.Equal(t, "GET", gotMethod)
	require.Len(t, got, 2)
	assert.Equal(t, IssueLinkType{ID: "10000", Name: "Blocks", Inward: "is blocked by", Outward: "blocks"}, got[0])
	assert.Equal(t, "Relates", got[1].Name)
}

func TestGetIssueLinkTypes_Error_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.GetIssueLinkTypes(context.Background())
	require.Error(t, err)
}

// --- CreateIssueLink ---

func TestCreateIssueLink_NoComment(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.CreateIssueLink(context.Background(), CreateIssueLinkInput{
		Type:         "Blocks",
		InwardIssue:  "PROJ-1",
		OutwardIssue: "PROJ-2",
	})
	require.NoError(t, err)
	assert.Equal(t, "/rest/api/3/issueLink", gotPath)
	assert.Equal(t, "POST", gotMethod)

	require.Equal(t, "Blocks", gotBody["type"].(map[string]any)["name"])
	require.Equal(t, "PROJ-1", gotBody["inwardIssue"].(map[string]any)["key"])
	require.Equal(t, "PROJ-2", gotBody["outwardIssue"].(map[string]any)["key"])
	_, hasComment := gotBody["comment"]
	assert.False(t, hasComment, "comment key must be absent when input.Comment is nil")
}

func TestCreateIssueLink_WithADFComment(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	adf := map[string]any{
		"version": 1, "type": "doc",
		"content": []any{map[string]any{"type": "paragraph"}},
	}

	c := newTestClient(t, srv.URL)
	err := c.CreateIssueLink(context.Background(), CreateIssueLinkInput{
		Type:         "Blocks",
		InwardIssue:  "PROJ-1",
		OutwardIssue: "PROJ-2",
		Comment:      &IssueLinkComment{Body: adf},
	})
	require.NoError(t, err)

	commentMap, ok := gotBody["comment"].(map[string]any)
	require.True(t, ok, "comment should be an object")
	assert.Equal(t, "doc", commentMap["body"].(map[string]any)["type"])
}

func TestCreateIssueLink_Error_400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorMessages":["Issue does not exist"]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.CreateIssueLink(context.Background(), CreateIssueLinkInput{
		Type: "Blocks", InwardIssue: "X-1", OutwardIssue: "X-2",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Issue does not exist")
}

// --- DeleteIssueLink ---

func TestDeleteIssueLink_Success(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.DeleteIssueLink(context.Background(), "10042")
	require.NoError(t, err)
	assert.Equal(t, "/rest/api/3/issueLink/10042", gotPath)
	assert.Equal(t, "DELETE", gotMethod)
}

func TestDeleteIssueLink_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errorMessages":["No link with id 9999 exists"]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.DeleteIssueLink(context.Background(), "9999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No link with id 9999 exists")
}

// --- GetFieldOptions multi-context ---

func TestGetFieldOptions_MultipleContexts(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/rest/api/3/field/cf_1/context":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{{"id": "ctx1"}, {"id": "ctx2"}},
			})
		case "/rest/api/3/field/cf_1/context/ctx1/option":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{{"id": "opt1", "value": "A"}, {"id": "opt2", "value": "B"}},
			})
		case "/rest/api/3/field/cf_1/context/ctx2/option":
			_ = json.NewEncoder(w).Encode(map[string]any{
				// opt2 appears in both contexts — should be deduplicated.
				"values": []map[string]any{{"id": "opt2", "value": "B"}, {"id": "opt3", "value": "C"}},
			})
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	opts, err := c.GetFieldOptions(context.Background(), "cf_1")
	require.NoError(t, err)
	assert.Len(t, opts, 3) // opt1, opt2 (deduped), opt3
	assert.Equal(t, 3, calls)
}

func TestGetFieldOptions_NoContexts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"values": []any{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	opts, err := c.GetFieldOptions(context.Background(), "cf_1")
	require.NoError(t, err)
	assert.Empty(t, opts)
}
