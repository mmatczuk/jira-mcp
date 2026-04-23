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
