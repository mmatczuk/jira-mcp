// Package  wraps the go-jira client with retry logic at the call level.
package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/andygrunwald/go-jira"
)

// Config holds JIRA connection settings.
type Config struct {
	URL        string
	Email      string
	APIToken   string
	MaxRetries int           // Default: 3
	BaseDelay  time.Duration // Default: 1s
}

// Client wraps go-jira with retry on 429 at the call level.
type Client struct {
	J   *jira.Client
	cfg Config
}

// New creates a new JIRA client.
func New(cfg Config) (*Client, error) {
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.BaseDelay == 0 {
		cfg.BaseDelay = time.Second
	}

	tp := jira.BasicAuthTransport{
		Username: cfg.Email,
		Password: cfg.APIToken,
	}

	j, err := jira.NewClient(tp.Client(), cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("create jira client: %w", err)
	}

	return &Client{J: j, cfg: cfg}, nil
}

// GetIssue fetches an issue by key.
func (c *Client) GetIssue(ctx context.Context, key string, opts *jira.GetQueryOptions) (*jira.Issue, error) {
	var issue *jira.Issue
	err := c.retry(ctx, func() (*jira.Response, error) {
		var resp *jira.Response
		var err error
		issue, resp, err = c.J.Issue.GetWithContext(ctx, key, opts)
		return resp, err
	})
	return issue, err
}

// SearchOptionsV3 configures a JQL search via the v3 search/jql endpoint.
type SearchOptionsV3 struct {
	MaxResults    int
	NextPageToken string
	Fields        []string
	Expand        string
}

// SearchResultV3 holds the response from a JQL search.
type SearchResultV3 struct {
	Issues        []jira.Issue
	Total         int
	NextPageToken string
}

// SearchIssues runs a JQL query using the v3 search/jql endpoint.
func (c *Client) SearchIssues(ctx context.Context, jql string, opts *SearchOptionsV3) (*SearchResultV3, error) {
	var sr SearchResultV3
	err := c.retry(ctx, func() (*jira.Response, error) {
		body := map[string]any{"jql": jql}
		if opts != nil {
			if opts.MaxResults > 0 {
				body["maxResults"] = opts.MaxResults
			}
			if opts.NextPageToken != "" {
				body["nextPageToken"] = opts.NextPageToken
			}
			if len(opts.Fields) > 0 {
				body["fields"] = opts.Fields
			}
			if opts.Expand != "" {
				body["expand"] = opts.Expand
			}
		}

		req, err := c.J.NewRequestWithContext(ctx, "POST", "rest/api/3/search/jql", body)
		if err != nil {
			return nil, err
		}

		var result struct {
			Issues        []jira.Issue `json:"issues"`
			Total         int          `json:"total"`
			NextPageToken string       `json:"nextPageToken"`
		}
		resp, err := c.J.Do(req, &result)
		sr = SearchResultV3{
			Issues:        result.Issues,
			Total:         result.Total,
			NextPageToken: result.NextPageToken,
		}
		return resp, err
	})
	return &sr, err
}

// DeleteIssue deletes an issue by key.
func (c *Client) DeleteIssue(ctx context.Context, key string) error {
	return c.retry(ctx, func() (*jira.Response, error) {
		resp, err := c.J.Issue.DeleteWithContext(ctx, key)
		return resp, err
	})
}

// GetTransitions returns available transitions for an issue.
func (c *Client) GetTransitions(ctx context.Context, key string) ([]jira.Transition, error) {
	var transitions []jira.Transition
	err := c.retry(ctx, func() (*jira.Response, error) {
		var resp *jira.Response
		var err error
		transitions, resp, err = c.J.Issue.GetTransitionsWithContext(ctx, key)
		return resp, err
	})
	return transitions, err
}

// DoTransition performs a transition on an issue.
func (c *Client) DoTransition(ctx context.Context, key, transitionID string) error {
	return c.retry(ctx, func() (*jira.Response, error) {
		resp, err := c.J.Issue.DoTransitionWithContext(ctx, key, transitionID)
		return resp, err
	})
}

// AddComment adds a comment to an issue using REST API v3 (ADF body).
// The body should be an ADF document map or plain text string.
func (c *Client) AddComment(ctx context.Context, key string, body any) (string, error) {
	var commentID string
	err := c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/3/issue/%s/comment", key)
		payload := map[string]any{"body": body}
		req, err := c.J.NewRequestWithContext(ctx, "POST", path, payload)
		if err != nil {
			return nil, err
		}
		var result struct {
			ID string `json:"id"`
		}
		resp, err := c.J.Do(req, &result)
		commentID = result.ID
		return resp, err
	})
	return commentID, err
}

// UpdateComment updates a comment using REST API v3 (ADF body).
func (c *Client) UpdateComment(ctx context.Context, key, commentID string, body any) error {
	return c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/3/issue/%s/comment/%s", key, commentID)
		payload := map[string]any{"body": body}
		req, err := c.J.NewRequestWithContext(ctx, "PUT", path, payload)
		if err != nil {
			return nil, err
		}
		resp, err := c.J.Do(req, nil)
		return resp, err
	})
}

// GetAllBoards returns boards, optionally filtered.
func (c *Client) GetAllBoards(ctx context.Context, opts *jira.BoardListOptions) ([]jira.Board, bool, error) {
	var boards []jira.Board
	var isLast bool
	err := c.retry(ctx, func() (*jira.Response, error) {
		result, resp, err := c.J.Board.GetAllBoardsWithContext(ctx, opts)
		if result != nil {
			boards = result.Values
			isLast = result.IsLast
		}
		return resp, err
	})
	return boards, isLast, err
}

// GetAllSprints returns sprints for a board.
func (c *Client) GetAllSprints(ctx context.Context, boardID int, opts *jira.GetAllSprintsOptions) ([]jira.Sprint, bool, error) {
	var sprints []jira.Sprint
	var isLast bool
	err := c.retry(ctx, func() (*jira.Response, error) {
		result, resp, err := c.J.Board.GetAllSprintsWithOptionsWithContext(ctx, boardID, opts)
		if result != nil {
			sprints = result.Values
			isLast = result.IsLast
		}
		return resp, err
	})
	return sprints, isLast, err
}

// GetSprintIssues returns issues in a sprint.
func (c *Client) GetSprintIssues(ctx context.Context, sprintID int) ([]jira.Issue, error) {
	var issues []jira.Issue
	err := c.retry(ctx, func() (*jira.Response, error) {
		var resp *jira.Response
		var err error
		issues, resp, err = c.J.Sprint.GetIssuesForSprintWithContext(ctx, sprintID)
		return resp, err
	})
	return issues, err
}

// MoveIssuesToSprint moves issues to a sprint.
func (c *Client) MoveIssuesToSprint(ctx context.Context, sprintID int, issueKeys []string) error {
	return c.retry(ctx, func() (*jira.Response, error) {
		resp, err := c.J.Sprint.MoveIssuesToSprintWithContext(ctx, sprintID, issueKeys)
		return resp, err
	})
}

// GetAllProjects returns all projects.
func (c *Client) GetAllProjects(ctx context.Context) (*jira.ProjectList, error) {
	var projects *jira.ProjectList
	err := c.retry(ctx, func() (*jira.Response, error) {
		var resp *jira.Response
		var err error
		projects, resp, err = c.J.Project.ListWithOptionsWithContext(ctx, &jira.GetQueryOptions{})
		return resp, err
	})
	return projects, err
}

// GetFields returns all fields.
func (c *Client) GetFields(ctx context.Context) ([]jira.Field, error) {
	var fields []jira.Field
	err := c.retry(ctx, func() (*jira.Response, error) {
		var resp *jira.Response
		var err error
		fields, resp, err = c.J.Field.GetListWithContext(ctx)
		return resp, err
	})
	return fields, err
}

// CreateIssueV3 creates an issue using REST API v3 with raw JSON payload.
// This allows passing ADF description as a proper JSON object.
func (c *Client) CreateIssueV3(ctx context.Context, payload map[string]any) (string, string, error) {
	var key, id string
	err := c.retry(ctx, func() (*jira.Response, error) {
		req, err := c.J.NewRequestWithContext(ctx, "POST", "rest/api/3/issue", payload)
		if err != nil {
			return nil, err
		}
		var result struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		}
		resp, err := c.J.Do(req, &result)
		key = result.Key
		id = result.ID
		return resp, err
	})
	return key, id, err
}

// UpdateIssueV3 updates an issue using REST API v3 with raw JSON payload.
func (c *Client) UpdateIssueV3(ctx context.Context, key string, payload map[string]any) error {
	return c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/3/issue/%s", key)
		req, err := c.J.NewRequestWithContext(ctx, "PUT", path, payload)
		if err != nil {
			return nil, err
		}
		resp, err := c.J.Do(req, nil)
		return resp, err
	})
}

// GetFieldOptions returns options for a custom field.
func (c *Client) GetFieldOptions(ctx context.Context, fieldID string) ([]json.RawMessage, error) {
	var values []json.RawMessage
	err := c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/3/field/%s/context", fieldID)
		req, err := c.J.NewRequestWithContext(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		var ctxResult struct {
			Values []struct {
				ID string `json:"id"`
			} `json:"values"`
		}
		resp, err := c.J.Do(req, &ctxResult)
		if err != nil {
			return resp, err
		}
		// Get options from the first context.
		if len(ctxResult.Values) == 0 {
			return resp, nil
		}
		contextID := ctxResult.Values[0].ID

		optPath := fmt.Sprintf("rest/api/3/field/%s/context/%s/option", fieldID, contextID)
		optReq, err := c.J.NewRequestWithContext(ctx, "GET", optPath, nil)
		if err != nil {
			return nil, err
		}
		var optResult struct {
			Values []json.RawMessage `json:"values"`
		}
		optResp, err := c.J.Do(optReq, &optResult)
		values = optResult.Values
		return optResp, err
	})
	return values, err
}

func (c *Client) shouldRetry(resp *jira.Response) (time.Duration, bool) {
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			return time.Duration(secs) * time.Second, true
		}
	}
	return 0, true
}

func (c *Client) backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	return c.cfg.BaseDelay * time.Duration(math.Pow(2, float64(attempt)))
}

// enrichError reads the JIRA response body and wraps the original error with
// the API error details. This is needed because go-jira's CheckResponse only
// includes the status code, discarding the body that contains the actual error
// messages from JIRA.
func enrichError(resp *jira.Response, original error) error {
	if resp == nil || resp.Body == nil {
		return original
	}

	// Try to parse as JIRA error JSON.
	var jiraErr jira.Error
	if err := json.NewDecoder(resp.Body).Decode(&jiraErr); err != nil {
		return original
	}

	var parts []string
	parts = append(parts, jiraErr.ErrorMessages...)
	for field, msg := range jiraErr.Errors {
		parts = append(parts, fmt.Sprintf("%s: %s", field, msg))
	}
	if len(parts) == 0 {
		return original
	}

	return fmt.Errorf("%w: %s", original, strings.Join(parts, "; "))
}

// closeResp safely closes a JIRA response body.
func closeResp(resp *jira.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func (c *Client) retry(ctx context.Context, fn func() (*jira.Response, error)) error {
	for attempt := range c.cfg.MaxRetries + 1 {
		resp, err := fn()
		if err == nil {
			closeResp(resp)
			return nil
		}

		retryAfter, shouldRetry := c.shouldRetry(resp)
		if !shouldRetry || attempt == c.cfg.MaxRetries {
			enriched := enrichError(resp, err)
			closeResp(resp)
			return enriched
		}

		closeResp(resp)
		delay := c.backoff(attempt, retryAfter)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil
}
