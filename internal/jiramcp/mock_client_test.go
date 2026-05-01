package jiramcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mmatczuk/jira-mcp/internal/jira"
)

// mockClient implements JiraClient for testing. Set only the Fn fields your
// test needs; unset methods panic with a clear message.
type mockClient struct {
	GetMyselfFn               func(ctx context.Context) (*jira.User, error)
	SearchUsersFn             func(ctx context.Context, query string) ([]jira.User, error)
	GetCreateMetaIssueTypesFn func(ctx context.Context, projectKey string) ([]jira.CreateMetaIssueType, error)
	GetCreateMetaFieldsFn     func(ctx context.Context, projectKey, issueTypeID string) ([]jira.CreateMetaField, error)
	GetIssueFn                func(ctx context.Context, key string, opts *jira.GetQueryOptions) (*jira.Issue, error)
	SearchIssuesFn            func(ctx context.Context, jql string, opts *jira.SearchOptionsV3) (*jira.SearchResultV3, error)
	CreateIssueV3Fn           func(ctx context.Context, payload map[string]any) (string, string, error)
	UpdateIssueV3Fn           func(ctx context.Context, key string, payload map[string]any) error
	CreateIssueV2Fn           func(ctx context.Context, payload map[string]any) (string, string, error)
	UpdateIssueV2Fn           func(ctx context.Context, key string, payload map[string]any) error
	DeleteIssueFn             func(ctx context.Context, key string) error
	DoTransitionFn            func(ctx context.Context, key, transitionID string) error
	AddCommentFn              func(ctx context.Context, key string, body any) (string, error)
	UpdateCommentFn           func(ctx context.Context, key, commentID string, body any) error
	AddCommentV2Fn            func(ctx context.Context, key, body string) (string, error)
	UpdateCommentV2Fn         func(ctx context.Context, key, commentID, body string) error
	GetAllBoardsFn            func(ctx context.Context, opts *jira.BoardListOptions) ([]jira.Board, bool, error)
	GetAllSprintsFn           func(ctx context.Context, boardID int, opts *jira.GetAllSprintsOptions) ([]jira.Sprint, bool, error)
	GetSprintIssuesFn         func(ctx context.Context, sprintID int) ([]jira.Issue, error)
	MoveIssuesToSprintFn      func(ctx context.Context, sprintID int, issueKeys []string) error
	GetAllProjectsFn          func(ctx context.Context) (*jira.ProjectList, error)
	GetFieldsFn               func(ctx context.Context) ([]jira.Field, error)
	GetTransitionsFn          func(ctx context.Context, key string) ([]jira.Transition, error)
	GetFieldOptionsFn         func(ctx context.Context, fieldID string) ([]json.RawMessage, error)
	CreateIssueLinkFn         func(ctx context.Context, in jira.CreateIssueLinkInput) error
	DeleteIssueLinkFn         func(ctx context.Context, linkID string) error
	GetIssueLinkTypesFn       func(ctx context.Context) ([]jira.IssueLinkType, error)
}

func (m *mockClient) GetMyself(ctx context.Context) (*jira.User, error) {
	if m.GetMyselfFn == nil {
		panic("mockClient.GetMyself called but GetMyselfFn not set")
	}
	return m.GetMyselfFn(ctx)
}

func (m *mockClient) SearchUsers(ctx context.Context, query string) ([]jira.User, error) {
	if m.SearchUsersFn == nil {
		panic(fmt.Sprintf("mockClient.SearchUsers called but SearchUsersFn not set (query=%s)", query))
	}
	return m.SearchUsersFn(ctx, query)
}

func (m *mockClient) GetCreateMetaIssueTypes(ctx context.Context, projectKey string) ([]jira.CreateMetaIssueType, error) {
	if m.GetCreateMetaIssueTypesFn == nil {
		panic(fmt.Sprintf("mockClient.GetCreateMetaIssueTypes called but GetCreateMetaIssueTypesFn not set (projectKey=%s)", projectKey))
	}
	return m.GetCreateMetaIssueTypesFn(ctx, projectKey)
}

func (m *mockClient) GetCreateMetaFields(ctx context.Context, projectKey, issueTypeID string) ([]jira.CreateMetaField, error) {
	if m.GetCreateMetaFieldsFn == nil {
		panic(fmt.Sprintf("mockClient.GetCreateMetaFields called but GetCreateMetaFieldsFn not set (projectKey=%s, issueTypeID=%s)", projectKey, issueTypeID))
	}
	return m.GetCreateMetaFieldsFn(ctx, projectKey, issueTypeID)
}

func (m *mockClient) GetIssue(ctx context.Context, key string, opts *jira.GetQueryOptions) (*jira.Issue, error) {
	if m.GetIssueFn == nil {
		panic(fmt.Sprintf("mockClient.GetIssue called but GetIssueFn not set (key=%s)", key))
	}
	return m.GetIssueFn(ctx, key, opts)
}

func (m *mockClient) SearchIssues(ctx context.Context, jql string, opts *jira.SearchOptionsV3) (*jira.SearchResultV3, error) {
	if m.SearchIssuesFn == nil {
		panic(fmt.Sprintf("mockClient.SearchIssues called but SearchIssuesFn not set (jql=%s)", jql))
	}
	return m.SearchIssuesFn(ctx, jql, opts)
}

func (m *mockClient) CreateIssueV3(ctx context.Context, payload map[string]any) (string, string, error) {
	if m.CreateIssueV3Fn == nil {
		panic("mockClient.CreateIssueV3 called but CreateIssueV3Fn not set")
	}
	return m.CreateIssueV3Fn(ctx, payload)
}

func (m *mockClient) UpdateIssueV3(ctx context.Context, key string, payload map[string]any) error {
	if m.UpdateIssueV3Fn == nil {
		panic(fmt.Sprintf("mockClient.UpdateIssueV3 called but UpdateIssueV3Fn not set (key=%s)", key))
	}
	return m.UpdateIssueV3Fn(ctx, key, payload)
}

func (m *mockClient) CreateIssueV2(ctx context.Context, payload map[string]any) (string, string, error) {
	if m.CreateIssueV2Fn == nil {
		panic("mockClient.CreateIssueV2 called but CreateIssueV2Fn not set")
	}
	return m.CreateIssueV2Fn(ctx, payload)
}

func (m *mockClient) UpdateIssueV2(ctx context.Context, key string, payload map[string]any) error {
	if m.UpdateIssueV2Fn == nil {
		panic(fmt.Sprintf("mockClient.UpdateIssueV2 called but UpdateIssueV2Fn not set (key=%s)", key))
	}
	return m.UpdateIssueV2Fn(ctx, key, payload)
}

func (m *mockClient) AddCommentV2(ctx context.Context, key, body string) (string, error) {
	if m.AddCommentV2Fn == nil {
		panic(fmt.Sprintf("mockClient.AddCommentV2 called but AddCommentV2Fn not set (key=%s)", key))
	}
	return m.AddCommentV2Fn(ctx, key, body)
}

func (m *mockClient) UpdateCommentV2(ctx context.Context, key, commentID, body string) error {
	if m.UpdateCommentV2Fn == nil {
		panic(fmt.Sprintf("mockClient.UpdateCommentV2 called but UpdateCommentV2Fn not set (key=%s, commentID=%s)", key, commentID))
	}
	return m.UpdateCommentV2Fn(ctx, key, commentID, body)
}

func (m *mockClient) DeleteIssue(ctx context.Context, key string) error {
	if m.DeleteIssueFn == nil {
		panic(fmt.Sprintf("mockClient.DeleteIssue called but DeleteIssueFn not set (key=%s)", key))
	}
	return m.DeleteIssueFn(ctx, key)
}

func (m *mockClient) DoTransition(ctx context.Context, key, transitionID string) error {
	if m.DoTransitionFn == nil {
		panic(fmt.Sprintf("mockClient.DoTransition called but DoTransitionFn not set (key=%s)", key))
	}
	return m.DoTransitionFn(ctx, key, transitionID)
}

func (m *mockClient) AddComment(ctx context.Context, key string, body any) (string, error) {
	if m.AddCommentFn == nil {
		panic(fmt.Sprintf("mockClient.AddComment called but AddCommentFn not set (key=%s)", key))
	}
	return m.AddCommentFn(ctx, key, body)
}

func (m *mockClient) UpdateComment(ctx context.Context, key, commentID string, body any) error {
	if m.UpdateCommentFn == nil {
		panic(fmt.Sprintf("mockClient.UpdateComment called but UpdateCommentFn not set (key=%s, commentID=%s)", key, commentID))
	}
	return m.UpdateCommentFn(ctx, key, commentID, body)
}

func (m *mockClient) GetAllBoards(ctx context.Context, opts *jira.BoardListOptions) ([]jira.Board, bool, error) {
	if m.GetAllBoardsFn == nil {
		panic("mockClient.GetAllBoards called but GetAllBoardsFn not set")
	}
	return m.GetAllBoardsFn(ctx, opts)
}

func (m *mockClient) GetAllSprints(ctx context.Context, boardID int, opts *jira.GetAllSprintsOptions) ([]jira.Sprint, bool, error) {
	if m.GetAllSprintsFn == nil {
		panic(fmt.Sprintf("mockClient.GetAllSprints called but GetAllSprintsFn not set (boardID=%d)", boardID))
	}
	return m.GetAllSprintsFn(ctx, boardID, opts)
}

func (m *mockClient) GetSprintIssues(ctx context.Context, sprintID int) ([]jira.Issue, error) {
	if m.GetSprintIssuesFn == nil {
		panic(fmt.Sprintf("mockClient.GetSprintIssues called but GetSprintIssuesFn not set (sprintID=%d)", sprintID))
	}
	return m.GetSprintIssuesFn(ctx, sprintID)
}

func (m *mockClient) MoveIssuesToSprint(ctx context.Context, sprintID int, issueKeys []string) error {
	if m.MoveIssuesToSprintFn == nil {
		panic(fmt.Sprintf("mockClient.MoveIssuesToSprint called but MoveIssuesToSprintFn not set (sprintID=%d)", sprintID))
	}
	return m.MoveIssuesToSprintFn(ctx, sprintID, issueKeys)
}

func (m *mockClient) GetAllProjects(ctx context.Context) (*jira.ProjectList, error) {
	if m.GetAllProjectsFn == nil {
		panic("mockClient.GetAllProjects called but GetAllProjectsFn not set")
	}
	return m.GetAllProjectsFn(ctx)
}

func (m *mockClient) GetFields(ctx context.Context) ([]jira.Field, error) {
	if m.GetFieldsFn == nil {
		panic("mockClient.GetFields called but GetFieldsFn not set")
	}
	return m.GetFieldsFn(ctx)
}

func (m *mockClient) GetTransitions(ctx context.Context, key string) ([]jira.Transition, error) {
	if m.GetTransitionsFn == nil {
		panic(fmt.Sprintf("mockClient.GetTransitions called but GetTransitionsFn not set (key=%s)", key))
	}
	return m.GetTransitionsFn(ctx, key)
}

func (m *mockClient) GetFieldOptions(ctx context.Context, fieldID string) ([]json.RawMessage, error) {
	if m.GetFieldOptionsFn == nil {
		panic(fmt.Sprintf("mockClient.GetFieldOptions called but GetFieldOptionsFn not set (fieldID=%s)", fieldID))
	}
	return m.GetFieldOptionsFn(ctx, fieldID)
}

func (m *mockClient) CreateIssueLink(ctx context.Context, in jira.CreateIssueLinkInput) error {
	if m.CreateIssueLinkFn == nil {
		panic(fmt.Sprintf("mockClient.CreateIssueLink called but CreateIssueLinkFn not set (type=%s, in=%s, out=%s)", in.Type, in.InwardIssue, in.OutwardIssue))
	}
	return m.CreateIssueLinkFn(ctx, in)
}

func (m *mockClient) DeleteIssueLink(ctx context.Context, linkID string) error {
	if m.DeleteIssueLinkFn == nil {
		panic(fmt.Sprintf("mockClient.DeleteIssueLink called but DeleteIssueLinkFn not set (linkID=%s)", linkID))
	}
	return m.DeleteIssueLinkFn(ctx, linkID)
}

func (m *mockClient) GetIssueLinkTypes(ctx context.Context) ([]jira.IssueLinkType, error) {
	if m.GetIssueLinkTypesFn == nil {
		panic("mockClient.GetIssueLinkTypes called but GetIssueLinkTypesFn not set")
	}
	return m.GetIssueLinkTypesFn(ctx)
}
