package jira

import "github.com/andygrunwald/go-jira"

// Type aliases re-exported from go-jira so that consumers do not need to
// import go-jira directly.

type (
	Attachment           = jira.Attachment
	Board                = jira.Board
	BoardListOptions     = jira.BoardListOptions
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
