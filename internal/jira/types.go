package jira

import "github.com/andygrunwald/go-jira"

// Type aliases re-exported from go-jira so that consumers do not need to
// import go-jira directly.

type (
	Attachment           = jira.Attachment
	Board                = jira.Board
	BoardListOptions     = jira.BoardListOptions
	Changelog            = jira.Changelog
	ChangelogHistory     = jira.ChangelogHistory
	ChangelogItems       = jira.ChangelogItems
	Comment              = jira.Comment
	Comments             = jira.Comments
	Field                = jira.Field
	FieldSchema          = jira.FieldSchema
	GetAllSprintsOptions = jira.GetAllSprintsOptions
	GetQueryOptions      = jira.GetQueryOptions
	Issue                = jira.Issue
	IssueFields          = jira.IssueFields
	IssueLink            = jira.IssueLink
	IssueType            = jira.IssueType
	Parent               = jira.Parent
	Priority             = jira.Priority
	ProjectList          = jira.ProjectList
	SearchOptions        = jira.SearchOptions
	Sprint               = jira.Sprint
	Status               = jira.Status
	Time                 = jira.Time
	Transition           = jira.Transition
	User                 = jira.User
)

// ChangelogPage holds the paging counts of an issue's embedded changelog,
// which go-jira's Changelog drops. Jira caps the embedded histories (100 on
// GET issue, 40 on search/jql), so Total can exceed the histories returned.
type ChangelogPage struct {
	StartAt    int `json:"startAt"`
	MaxResults int `json:"maxResults"`
	Total      int `json:"total"`
}
