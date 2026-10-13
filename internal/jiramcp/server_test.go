package jiramcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerInstructions_MentionsAttachments(t *testing.T) {
	assert.Contains(t, serverInstructions, "jira_attachments")
}

func TestNewServer_ListTools(t *testing.T) {
	ctx := t.Context()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	ss, err := NewServer(&mockClient{}, nil, nil).Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })

	res, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	assert.ElementsMatch(t, []string{"jira_read", "jira_write", "jira_schema", "jira_user_search", "jira_attachments"}, names)
}
