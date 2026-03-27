package jiramcp

import (
	"context"
	"encoding/json"

	"github.com/mmatczuk/jira-mcp/internal/jira"
)

// JiraClient defines the Jira operations used by the MCP handlers.
type JiraClient interface {
	GetMyself(ctx context.Context) (*jira.User, error)
	SearchUsers(ctx context.Context, query string) ([]jira.User, error)
	GetCreateMetaIssueTypes(ctx context.Context, projectKey string) ([]jira.CreateMetaIssueType, error)
	GetCreateMetaFields(ctx context.Context, projectKey, issueTypeID string) ([]jira.CreateMetaField, error)
	GetIssue(ctx context.Context, key string, opts *jira.GetQueryOptions) (*jira.Issue, error)
	SearchIssues(ctx context.Context, jql string, opts *jira.SearchOptionsV3) (*jira.SearchResultV3, error)
	CreateIssueV3(ctx context.Context, payload map[string]any) (string, string, error)
	UpdateIssueV3(ctx context.Context, key string, payload map[string]any) error
	DeleteIssue(ctx context.Context, key string) error
	DoTransition(ctx context.Context, key, transitionID string) error
	AddComment(ctx context.Context, key string, body any) (string, error)
	UpdateComment(ctx context.Context, key, commentID string, body any) error
	GetAllBoards(ctx context.Context, opts *jira.BoardListOptions) ([]jira.Board, bool, error)
	GetAllSprints(ctx context.Context, boardID int, opts *jira.GetAllSprintsOptions) ([]jira.Sprint, bool, error)
	GetSprintIssues(ctx context.Context, sprintID int) ([]jira.Issue, error)
	MoveIssuesToSprint(ctx context.Context, sprintID int, issueKeys []string) error
	GetAllProjects(ctx context.Context) (*jira.ProjectList, error)
	GetFields(ctx context.Context) ([]jira.Field, error)
	GetTransitions(ctx context.Context, key string) ([]jira.Transition, error)
	GetFieldOptions(ctx context.Context, fieldID string) ([]json.RawMessage, error)
}
