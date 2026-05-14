package jiramcp

import (
	"reflect"
	"testing"

	"github.com/andygrunwald/go-jira"
)

// projected names the jira.IssueFields members that issueToMap deliberately
// surfaces under fields.<name>. Source of truth: the if-blocks in
// internal/jiramcp/tool_read.go issueToMap.
var projected = map[string]struct{}{
	"Assignee":    {},
	"Created":     {},
	"Description": {},
	"Labels":      {},
	"Priority":    {},
	"Status":      {},
	"Summary":     {},
	"Type":        {},
	"Updated":     {},
}

// declined names jira.IssueFields members we deliberately do not project,
// each paired with a one-line reason. New fields added upstream must land
// in either projected or declined before TestIssueFieldsProjectionPolicy
// passes.
var declined = map[string]string{
	// --- tracked in open follow-up tickets ---
	"Parent":      "tracked in #25 / PR #27",
	"Comments":    "tracked in #21 / PR #22",
	"Attachments": "tracked in #19 / PR #20",
	"IssueLinks":  "tracked in #28 / PR #30 (read-side asymmetry with #14's write surface)",

	// --- deferred until a workflow asks ---
	"AffectsVersions": "deferred",
	"Components":      "deferred",
	"Creator":         "rarely consumed; assignee/reporter cover common cases",
	"Duedate":         "deferred; surface if a workflow requires it",
	"Environment":     "free-form prose, niche",
	"FixVersions":     "deferred",
	"Reporter":        "rarely consumed; assignee covers the common case",
	"Resolution":      "available via status workflow",
	"Resolutiondate":  "redundant; Updated covers state changes",
	"Subtasks":        "deferred",

	// --- aggregate metrics not useful as agent context ---
	"AggregateProgress": "aggregate metric, separate concern",
	"Progress":          "aggregate metric, separate concern",
	"Watches":           "viewer-affinity metric, noisy",

	// --- time-tracking sub-surfaces (own pagination / workflow) ---
	"AggregateTimeEstimate":         "time-tracking sub-surface, separate workflow",
	"AggregateTimeOriginalEstimate": "time-tracking sub-surface, separate workflow",
	"AggregateTimeSpent":            "time-tracking sub-surface, separate workflow",
	"TimeEstimate":                  "time-tracking sub-surface, separate workflow",
	"TimeOriginalEstimate":          "time-tracking sub-surface, separate workflow",
	"TimeSpent":                     "time-tracking sub-surface, separate workflow",
	"TimeTracking":                  "time-tracking sub-surface, separate workflow",
	"Worklog":                       "separate workflow with own pagination",

	// --- epic / sprint native fields (customfield_* fallback in use) ---
	"Epic":   "use customfield_* for Epic Link; native surface deferred",
	"Sprint": "use customfield_* for Sprint field; native surface deferred",

	// --- pagination plumbing, not issue data ---
	"Expand":  "go-jira pagination hint, not issue data",
	"Project": "redundant; encoded in the issue key prefix",

	// --- internal go-jira slot ---
	"Unknowns": "dynamic custom-field passthrough; not surfaced on main",
}

// TestIssueFieldsProjectionPolicy enforces that every exported member of
// go-jira's IssueFields is explicitly classified as either projected by
// issueToMap or deliberately declined. The intent is to make future
// silent-field-drop bugs impossible: a go-jira bump that adds a typed
// member fails this test until a contributor chooses project-or-decline.
//
// Scope: this test covers jira.IssueFields only — the shape returned by
// issue fetches. Other hand-projected response shapes (e.g. the
// remote_links resource, projects, boards, sprints) have their own
// projection sites and could grow the same bug independently; extending
// this pattern to them is a future task.
//
// Edit this file (internal/jiramcp/projection_policy_test.go) — not the
// production code — when reclassifying a field.
func TestIssueFieldsProjectionPolicy(t *testing.T) {
	rt := reflect.TypeOf(jira.IssueFields{})
	realFields := make(map[string]struct{}, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		realFields[name] = struct{}{}
		_, isProjected := projected[name]
		_, isDeclined := declined[name]
		switch {
		case isProjected && isDeclined:
			t.Errorf("IssueFields.%s appears in both projected and declined — pick one (projection_policy_test.go)", name)
		case !isProjected && !isDeclined:
			t.Errorf("IssueFields.%s is neither projected nor declined — add it to one or the other in projection_policy_test.go", name)
		}
	}

	for name := range projected {
		if _, ok := realFields[name]; !ok {
			t.Errorf("projected[%q] does not match any IssueFields member — go-jira renamed or removed it; update projection_policy_test.go", name)
		}
	}
	for name := range declined {
		if _, ok := realFields[name]; !ok {
			t.Errorf("declined[%q] does not match any IssueFields member — go-jira renamed or removed it; update projection_policy_test.go", name)
		}
	}
}
