// Package  wraps the go-jira client with retry logic at the call level.
package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/andygrunwald/go-jira"
)

// ErrAttachmentTooLarge is returned by GetAttachmentBody when the body
// exceeds the requested cap.
var ErrAttachmentTooLarge = errors.New("attachment exceeds size cap")

// APIError wraps a Jira REST call that failed with a non-2xx status, carrying
// that status so callers can branch on it without matching error text. Errors
// raised client-side on an otherwise successful call (a decode failure, an
// over-cap attachment body) are deliberately left unwrapped, so the presence of
// an APIError means the status itself was the failure.
type APIError struct {
	StatusCode int
	Err        error
}

func (e *APIError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("jira api error: status %d", e.StatusCode)
	}
	return e.Err.Error()
}

func (e *APIError) Unwrap() error { return e.Err }

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
	j   *jira.Client
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

	return &Client{j: j, cfg: cfg}, nil
}

// GetMyself returns the currently authenticated user.
func (c *Client) GetMyself(ctx context.Context) (*jira.User, error) {
	var user *jira.User
	err := c.retry(ctx, func() (*jira.Response, error) {
		req, err := c.j.NewRequestWithContext(ctx, "GET", "rest/api/3/myself", nil)
		if err != nil {
			return nil, err
		}
		user = new(jira.User)
		resp, err := c.j.Do(req, user)
		return resp, err
	})
	return user, err
}

// SearchUsers searches for Jira users by display name, email, or other attributes.
func (c *Client) SearchUsers(ctx context.Context, query string) ([]jira.User, error) {
	var users []jira.User
	err := c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/3/user/search?query=%s&maxResults=10", url.QueryEscape(query))
		req, err := c.j.NewRequestWithContext(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.j.Do(req, &users)
		return resp, err
	})
	return users, err
}

// CreateMetaIssueType represents an issue type returned by the create metadata endpoint.
type CreateMetaIssueType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CreateMetaField represents a field returned by the create metadata endpoint.
type CreateMetaField struct {
	FieldID         string           `json:"fieldId"`
	Name            string           `json:"name"`
	Required        bool             `json:"required"`
	HasDefaultValue bool             `json:"hasDefaultValue"`
	AllowedValues   []map[string]any `json:"allowedValues"`
}

// GetCreateMetaIssueTypes returns the available issue types for creating issues in a project.
func (c *Client) GetCreateMetaIssueTypes(ctx context.Context, projectKey string) ([]CreateMetaIssueType, error) {
	var issueTypes []CreateMetaIssueType
	err := c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/3/issue/createmeta/%s/issuetypes", url.PathEscape(projectKey))
		req, err := c.j.NewRequestWithContext(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		var result struct {
			IssueTypes []CreateMetaIssueType `json:"issueTypes"`
		}
		resp, err := c.j.Do(req, &result)
		issueTypes = result.IssueTypes
		return resp, err
	})
	return issueTypes, err
}

// GetCreateMetaFields returns the fields (with required flag and allowed values) for a
// specific project and issue type combination.
func (c *Client) GetCreateMetaFields(ctx context.Context, projectKey, issueTypeID string) ([]CreateMetaField, error) {
	var fields []CreateMetaField
	err := c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/3/issue/createmeta/%s/issuetypes/%s",
			url.PathEscape(projectKey), url.PathEscape(issueTypeID))
		req, err := c.j.NewRequestWithContext(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		var result struct {
			Values []CreateMetaField `json:"values"`
		}
		resp, err := c.j.Do(req, &result)
		fields = result.Values
		return resp, err
	})
	return fields, err
}

// IssueLinkType describes an entry returned by GET /rest/api/3/issueLinkType.
// Inward and Outward carry the verb pair (e.g. "is blocked by" / "blocks").
type IssueLinkType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

// IssueLinkComment is the optional comment posted alongside link creation.
// Body may be either an ADF document map (v3 default) or a raw string —
// callers should send ADF since POST /rest/api/3/issueLink does not accept
// legacy wiki-markup.
type IssueLinkComment struct {
	Body any
}

// CreateIssueLinkInput is the request shape for POST /rest/api/3/issueLink.
// Empirically (verified on live Jira Cloud by reading the stored direction back),
// the resulting relationship reads "InwardIssue blocks OutwardIssue" for
// type=Blocks — i.e. OutwardIssue is the passive (blocked) side. Callers that map
// a from/to "from blocks to" contract must therefore send from as InwardIssue and
// to as OutwardIssue (see jiramcp.applyLinks).
type CreateIssueLinkInput struct {
	Type         string
	InwardIssue  string
	OutwardIssue string
	Comment      *IssueLinkComment
}

// RemoteLink represents an entry returned by
// GET /rest/api/3/issue/{issueIdOrKey}/remotelink — a web link from a Jira
// issue to an external resource (URL, document, ticket in another system).
// Internal issue-to-issue links use the separate /issueLink endpoint and
// IssueLinkType type above.
type RemoteLink struct {
	ID           int              `json:"id"`
	Self         string           `json:"self,omitempty"`
	GlobalID     string           `json:"globalId,omitempty"`
	Relationship string           `json:"relationship,omitempty"`
	Application  *RemoteLinkApp   `json:"application,omitempty"`
	Object       RemoteLinkObject `json:"object"`
}

// RemoteLinkApp is the application that owns the linked resource.
type RemoteLinkApp struct {
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
}

// RemoteLinkObject is the linked resource itself. URL and Title are the only
// fields Jira guarantees; the rest are optional.
type RemoteLinkObject struct {
	URL     string            `json:"url"`
	Title   string            `json:"title"`
	Summary string            `json:"summary,omitempty"`
	Icon    *RemoteLinkIcon   `json:"icon,omitempty"`
	Status  *RemoteLinkStatus `json:"status,omitempty"`
}

// RemoteLinkIcon is the icon shown for the link in the Jira UI.
type RemoteLinkIcon struct {
	URL16x16 string `json:"url16x16,omitempty"`
	Title    string `json:"title,omitempty"`
	Link     string `json:"link,omitempty"`
}

// RemoteLinkStatus describes the resolved/unresolved state of the linked
// resource, when the external system reports one.
type RemoteLinkStatus struct {
	Resolved bool            `json:"resolved"`
	Icon     *RemoteLinkIcon `json:"icon,omitempty"`
}

// GetRemoteLinks returns the remote (web) links attached to an issue via
// GET /rest/api/3/issue/{issueIdOrKey}/remotelink. The response is an array;
// an issue with no remote links returns an empty slice, not an error.
func (c *Client) GetRemoteLinks(ctx context.Context, issueKey string) ([]RemoteLink, error) {
	var links []RemoteLink
	err := c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/3/issue/%s/remotelink", url.PathEscape(issueKey))
		req, err := c.j.NewRequestWithContext(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.j.Do(req, &links)
		return resp, err
	})
	return links, err
}

// CreateOrUpdateRemoteLinkInput is the request shape for
// POST /rest/api/3/issue/{issueIdOrKey}/remotelink. Jira upserts on GlobalID.
type CreateOrUpdateRemoteLinkInput struct {
	URL         string
	Title       string
	GlobalID    string
	Application *RemoteLinkApp
	Resolved    *bool
}

// CreateOrUpdateRemoteLinkResult captures what Jira returns from the upsert.
type CreateOrUpdateRemoteLinkResult struct {
	ID      int    // Jira's internal link id.
	Self    string // Canonical URL of the created/updated link.
	Created bool   // True when Jira returned 201 (new link); false on 200 (existing link updated).
}

// CreateOrUpdateRemoteLink creates or updates a remote link via
// POST /rest/api/3/issue/{issueIdOrKey}/remotelink. Jira dedups on globalId:
// a request whose globalId matches an existing link updates it (status 200)
// instead of creating a duplicate (status 201) — CreateOrUpdateRemoteLinkResult.Created
// reflects which of the two occurred.
func (c *Client) CreateOrUpdateRemoteLink(ctx context.Context, issueKey string, in CreateOrUpdateRemoteLinkInput) (*CreateOrUpdateRemoteLinkResult, error) {
	object := RemoteLinkObject{
		URL:   in.URL,
		Title: in.Title,
	}
	if in.Resolved != nil {
		object.Status = &RemoteLinkStatus{Resolved: *in.Resolved}
	}
	body := struct {
		GlobalID    string           `json:"globalId,omitempty"`
		Application *RemoteLinkApp   `json:"application,omitempty"`
		Object      RemoteLinkObject `json:"object"`
	}{
		GlobalID:    in.GlobalID,
		Application: in.Application,
		Object:      object,
	}

	var result CreateOrUpdateRemoteLinkResult
	err := c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/3/issue/%s/remotelink", url.PathEscape(issueKey))
		req, reqErr := c.j.NewRequestWithContext(ctx, "POST", path, body)
		if reqErr != nil {
			return nil, reqErr
		}
		var respBody struct {
			ID   int    `json:"id"`
			Self string `json:"self"`
		}
		resp, doErr := c.j.Do(req, &respBody)
		if resp != nil {
			result.Created = resp.StatusCode == http.StatusCreated
		}
		result.ID = respBody.ID
		result.Self = respBody.Self
		return resp, doErr
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteRemoteLink removes a remote link via
// DELETE /rest/api/3/issue/{issueIdOrKey}/remotelink/{linkId} (path form, used
// when linkID is set) or DELETE .../remotelink?globalId=... (query form, used
// when only globalID is set); globalID is URL-encoded into the query string.
// Jira returns 204 on success. Exactly one of linkID / globalID should be
// non-empty — enforcing that as a user-facing validation error is the
// handler's job, but this method still rejects the case where both are empty
// to avoid silently hitting the bare collection endpoint.
func (c *Client) DeleteRemoteLink(ctx context.Context, issueKey, linkID, globalID string) error {
	if linkID == "" && globalID == "" {
		return errors.New("delete remote link: at least one of linkID or globalID must be set")
	}

	var path string
	if linkID != "" {
		path = fmt.Sprintf("rest/api/3/issue/%s/remotelink/%s", url.PathEscape(issueKey), url.PathEscape(linkID))
	} else {
		path = fmt.Sprintf("rest/api/3/issue/%s/remotelink?globalId=%s", url.PathEscape(issueKey), url.QueryEscape(globalID))
	}

	return c.retry(ctx, func() (*jira.Response, error) {
		req, err := c.j.NewRequestWithContext(ctx, "DELETE", path, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.j.Do(req, nil)
		return resp, err
	})
}

// GetIssueLinkTypes returns the issue link types configured on the Jira
// instance. Used by jira_schema link_types so callers can discover the
// (name, inward, outward) verb triples needed to construct unambiguous
// link writes.
func (c *Client) GetIssueLinkTypes(ctx context.Context) ([]IssueLinkType, error) {
	var types []IssueLinkType
	err := c.retry(ctx, func() (*jira.Response, error) {
		req, err := c.j.NewRequestWithContext(ctx, "GET", "rest/api/3/issueLinkType", nil)
		if err != nil {
			return nil, err
		}
		var result struct {
			IssueLinkTypes []IssueLinkType `json:"issueLinkTypes"`
		}
		resp, err := c.j.Do(req, &result)
		types = result.IssueLinkTypes
		return resp, err
	})
	return types, err
}

// CreateIssueLink creates an issue link via POST /rest/api/3/issueLink.
// Jira returns 201 with no body on success. When in.Comment is non-nil the
// comment is posted on the inward issue in the same call; the body must be
// ADF — this endpoint does not accept legacy wiki-markup.
func (c *Client) CreateIssueLink(ctx context.Context, in CreateIssueLinkInput) error {
	body := map[string]any{
		"type":         map[string]any{"name": in.Type},
		"inwardIssue":  map[string]any{"key": in.InwardIssue},
		"outwardIssue": map[string]any{"key": in.OutwardIssue},
	}
	if in.Comment != nil {
		body["comment"] = map[string]any{"body": in.Comment.Body}
	}
	return c.retry(ctx, func() (*jira.Response, error) {
		req, err := c.j.NewRequestWithContext(ctx, "POST", "rest/api/3/issueLink", body)
		if err != nil {
			return nil, err
		}
		resp, err := c.j.Do(req, nil)
		return resp, err
	})
}

// DeleteIssueLink removes an issue link via DELETE /rest/api/3/issueLink/{linkID}.
// Jira returns 204 on success.
func (c *Client) DeleteIssueLink(ctx context.Context, linkID string) error {
	return c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/3/issueLink/%s", url.PathEscape(linkID))
		req, err := c.j.NewRequestWithContext(ctx, "DELETE", path, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.j.Do(req, nil)
		return resp, err
	})
}

// GetIssue fetches an issue by key.
func (c *Client) GetIssue(ctx context.Context, key string, opts *jira.GetQueryOptions) (*jira.Issue, error) {
	var issue *jira.Issue
	err := c.retry(ctx, func() (*jira.Response, error) {
		var resp *jira.Response
		var err error
		issue, resp, err = c.j.Issue.GetWithContext(ctx, key, opts)
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

		req, err := c.j.NewRequestWithContext(ctx, "POST", "rest/api/3/search/jql", body)
		if err != nil {
			return nil, err
		}

		var result struct {
			Issues        []jira.Issue `json:"issues"`
			Total         int          `json:"total"`
			NextPageToken string       `json:"nextPageToken"`
		}
		resp, err := c.j.Do(req, &result)
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
		resp, err := c.j.Issue.DeleteWithContext(ctx, key)
		return resp, err
	})
}

// GetTransitions returns available transitions for an issue.
func (c *Client) GetTransitions(ctx context.Context, key string) ([]jira.Transition, error) {
	var transitions []jira.Transition
	err := c.retry(ctx, func() (*jira.Response, error) {
		var resp *jira.Response
		var err error
		transitions, resp, err = c.j.Issue.GetTransitionsWithContext(ctx, key)
		return resp, err
	})
	return transitions, err
}

// DoTransition performs a transition on an issue.
func (c *Client) DoTransition(ctx context.Context, key, transitionID string) error {
	return c.retry(ctx, func() (*jira.Response, error) {
		resp, err := c.j.Issue.DoTransitionWithContext(ctx, key, transitionID)
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
		req, err := c.j.NewRequestWithContext(ctx, "POST", path, payload)
		if err != nil {
			return nil, err
		}
		var result struct {
			ID string `json:"id"`
		}
		resp, err := c.j.Do(req, &result)
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
		req, err := c.j.NewRequestWithContext(ctx, "PUT", path, payload)
		if err != nil {
			return nil, err
		}
		resp, err := c.j.Do(req, nil)
		return resp, err
	})
}

// GetAllBoards returns boards, optionally filtered.
func (c *Client) GetAllBoards(ctx context.Context, opts *jira.BoardListOptions) ([]jira.Board, bool, error) {
	var boards []jira.Board
	var isLast bool
	err := c.retry(ctx, func() (*jira.Response, error) {
		result, resp, err := c.j.Board.GetAllBoardsWithContext(ctx, opts)
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
		result, resp, err := c.j.Board.GetAllSprintsWithOptionsWithContext(ctx, boardID, opts)
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
		issues, resp, err = c.j.Sprint.GetIssuesForSprintWithContext(ctx, sprintID)
		return resp, err
	})
	return issues, err
}

// MoveIssuesToSprint moves issues to a sprint.
func (c *Client) MoveIssuesToSprint(ctx context.Context, sprintID int, issueKeys []string) error {
	return c.retry(ctx, func() (*jira.Response, error) {
		resp, err := c.j.Sprint.MoveIssuesToSprintWithContext(ctx, sprintID, issueKeys)
		return resp, err
	})
}

// GetAllProjects returns all projects.
func (c *Client) GetAllProjects(ctx context.Context) (*jira.ProjectList, error) {
	var projects *jira.ProjectList
	err := c.retry(ctx, func() (*jira.Response, error) {
		var resp *jira.Response
		var err error
		projects, resp, err = c.j.Project.ListWithOptionsWithContext(ctx, &jira.GetQueryOptions{})
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
		fields, resp, err = c.j.Field.GetListWithContext(ctx)
		return resp, err
	})
	return fields, err
}

// CreateIssueV3 creates an issue using REST API v3 with raw JSON payload.
// This allows passing ADF description as a proper JSON object.
func (c *Client) CreateIssueV3(ctx context.Context, payload map[string]any) (string, string, error) {
	var key, id string
	err := c.retry(ctx, func() (*jira.Response, error) {
		req, err := c.j.NewRequestWithContext(ctx, "POST", "rest/api/3/issue", payload)
		if err != nil {
			return nil, err
		}
		var result struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		}
		resp, err := c.j.Do(req, &result)
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
		req, err := c.j.NewRequestWithContext(ctx, "PUT", path, payload)
		if err != nil {
			return nil, err
		}
		resp, err := c.j.Do(req, nil)
		return resp, err
	})
}

// CreateIssueV2 creates an issue using REST API v2 with a raw JSON payload.
// The v2 API accepts `description` and comment bodies as legacy wiki-markup
// strings (v3 requires ADF). Use only when the caller explicitly opts in.
func (c *Client) CreateIssueV2(ctx context.Context, payload map[string]any) (string, string, error) {
	var key, id string
	err := c.retry(ctx, func() (*jira.Response, error) {
		req, err := c.j.NewRequestWithContext(ctx, "POST", "rest/api/2/issue", payload)
		if err != nil {
			return nil, err
		}
		var result struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		}
		resp, err := c.j.Do(req, &result)
		key = result.Key
		id = result.ID
		return resp, err
	})
	return key, id, err
}

// UpdateIssueV2 updates an issue using REST API v2. See CreateIssueV2 for the
// rationale behind exposing the legacy endpoint.
func (c *Client) UpdateIssueV2(ctx context.Context, key string, payload map[string]any) error {
	return c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/2/issue/%s", key)
		req, err := c.j.NewRequestWithContext(ctx, "PUT", path, payload)
		if err != nil {
			return nil, err
		}
		resp, err := c.j.Do(req, nil)
		return resp, err
	})
}

// AddCommentV2 adds a comment using REST API v2 (wiki-markup string body).
func (c *Client) AddCommentV2(ctx context.Context, key, body string) (string, error) {
	var commentID string
	err := c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/2/issue/%s/comment", key)
		payload := map[string]any{"body": body}
		req, err := c.j.NewRequestWithContext(ctx, "POST", path, payload)
		if err != nil {
			return nil, err
		}
		var result struct {
			ID string `json:"id"`
		}
		resp, err := c.j.Do(req, &result)
		commentID = result.ID
		return resp, err
	})
	return commentID, err
}

// UpdateCommentV2 updates a comment using REST API v2 (wiki-markup string body).
func (c *Client) UpdateCommentV2(ctx context.Context, key, commentID, body string) error {
	return c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/2/issue/%s/comment/%s", key, commentID)
		payload := map[string]any{"body": body}
		req, err := c.j.NewRequestWithContext(ctx, "PUT", path, payload)
		if err != nil {
			return nil, err
		}
		resp, err := c.j.Do(req, nil)
		return resp, err
	})
}

// GetFieldOptions returns options for a custom field, aggregated across all contexts.
func (c *Client) GetFieldOptions(ctx context.Context, fieldID string) ([]json.RawMessage, error) {
	// Fetch all contexts for the field.
	var contextIDs []string
	err := c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/3/field/%s/context", fieldID)
		req, err := c.j.NewRequestWithContext(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		var ctxResult struct {
			Values []struct {
				ID string `json:"id"`
			} `json:"values"`
		}
		resp, err := c.j.Do(req, &ctxResult)
		if err != nil {
			return resp, err
		}
		for _, v := range ctxResult.Values {
			contextIDs = append(contextIDs, v.ID)
		}
		return resp, nil
	})
	if err != nil {
		return nil, err
	}

	// Fetch options from each context, deduplicating by id.
	seen := make(map[string]bool)
	var values []json.RawMessage
	for _, contextID := range contextIDs {
		err := c.retry(ctx, func() (*jira.Response, error) {
			optPath := fmt.Sprintf("rest/api/3/field/%s/context/%s/option", fieldID, contextID)
			optReq, err := c.j.NewRequestWithContext(ctx, "GET", optPath, nil)
			if err != nil {
				return nil, err
			}
			var optResult struct {
				Values []json.RawMessage `json:"values"`
			}
			resp, err := c.j.Do(optReq, &optResult)
			if err != nil {
				return resp, err
			}
			for _, raw := range optResult.Values {
				var id struct {
					ID string `json:"id"`
				}
				if jsonErr := json.Unmarshal(raw, &id); jsonErr == nil && !seen[id.ID] {
					seen[id.ID] = true
					values = append(values, raw)
				}
			}
			return resp, nil
		})
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

// GetAttachmentMeta returns the metadata for a single attachment.
func (c *Client) GetAttachmentMeta(ctx context.Context, id string) (*jira.Attachment, error) {
	type attachmentMeta struct {
		Self      string       `json:"self,omitempty"`
		ID        attachmentID `json:"id,omitempty"`
		Filename  string       `json:"filename,omitempty"`
		Author    *jira.User   `json:"author,omitempty"`
		Created   string       `json:"created,omitempty"`
		Size      int          `json:"size,omitempty"`
		MimeType  string       `json:"mimeType,omitempty"`
		Content   string       `json:"content,omitempty"`
		Thumbnail string       `json:"thumbnail,omitempty"`
	}
	var raw attachmentMeta
	err := c.retry(ctx, func() (*jira.Response, error) {
		path := fmt.Sprintf("rest/api/3/attachment/%s", url.PathEscape(id))
		req, err := c.j.NewRequestWithContext(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}
		raw = attachmentMeta{}
		resp, err := c.j.Do(req, &raw)
		return resp, err
	})
	if err != nil {
		return nil, err
	}
	return &jira.Attachment{
		Self:      raw.Self,
		ID:        string(raw.ID),
		Filename:  raw.Filename,
		Author:    raw.Author,
		Created:   raw.Created,
		Size:      raw.Size,
		MimeType:  raw.MimeType,
		Content:   raw.Content,
		Thumbnail: raw.Thumbnail,
	}, nil
}

// GetAttachmentBody downloads the attachment body, capped at maxBytes
// (inclusive). Over-cap returns an error that wraps ErrAttachmentTooLarge.
func (c *Client) GetAttachmentBody(ctx context.Context, id string, maxBytes int64) ([]byte, error) {
	var body []byte
	err := c.retry(ctx, func() (*jira.Response, error) {
		resp, dlErr := c.j.Issue.DownloadAttachmentWithContext(ctx, id)
		if dlErr != nil {
			return resp, dlErr
		}
		if resp == nil || resp.Body == nil {
			return resp, fmt.Errorf("attachment %s: empty response", id)
		}
		// Read one more than the cap so we can distinguish at-cap from over-cap.
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		if readErr != nil {
			return resp, readErr
		}
		if int64(len(b)) > maxBytes {
			return resp, fmt.Errorf("attachment %s exceeds cap of %d bytes: %w", id, maxBytes, ErrAttachmentTooLarge)
		}
		body = b
		return resp, nil
	})
	return body, err
}

// PostAttachmentText uploads body as filename to the issue.
func (c *Client) PostAttachmentText(ctx context.Context, issueKey, filename, body string) (*jira.Attachment, error) {
	var att *jira.Attachment
	err := c.retry(ctx, func() (*jira.Response, error) {
		atts, resp, postErr := c.j.Issue.PostAttachmentWithContext(ctx, issueKey, strings.NewReader(body), filename)
		if postErr != nil {
			return resp, postErr
		}
		if atts == nil || len(*atts) == 0 {
			return resp, fmt.Errorf("attachment upload to %s returned no metadata", issueKey)
		}
		first := (*atts)[0]
		att = &first
		return resp, nil
	})
	return att, err
}

// DeleteAttachment removes the attachment by id.
func (c *Client) DeleteAttachment(ctx context.Context, id string) error {
	return c.retry(ctx, func() (*jira.Response, error) {
		resp, err := c.j.Issue.DeleteAttachmentWithContext(ctx, id)
		return resp, err
	})
}

// attachmentID decodes a Jira attachment id, which the API returns as either
// a JSON string (issue-fields path) or number (rest/api/3/attachment/{id}).
// The decoded value is the canonical string form.
type attachmentID string

func (a *attachmentID) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*a = ""
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*a = attachmentID(s)
		return nil
	}
	// Numeric literal — trim whitespace and accept as-is. Validate that it
	// decodes as a JSON number so malformed input still errors.
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*a = attachmentID(n.String())
	return nil
}

func (c *Client) shouldRetry(resp *jira.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil {
				return time.Duration(secs) * time.Second, true
			}
		}
		return 0, true
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return 0, true
	}
	return 0, false
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
			// Only a non-2xx status becomes an APIError. A 2xx response paired
			// with an error means the call succeeded and the failure came from
			// our own handling of the body, where the status says nothing.
			if resp != nil && (resp.StatusCode < 200 || resp.StatusCode > 299) {
				enriched = &APIError{StatusCode: resp.StatusCode, Err: enriched}
			}
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
