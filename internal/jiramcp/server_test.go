package jiramcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServerInstructions_MentionsAttachments(t *testing.T) {
	assert.Contains(t, serverInstructions, "jira_attachments")
}
