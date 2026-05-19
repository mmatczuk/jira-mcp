package jiramcp

import (
	"context"
	"errors"
	"testing"

	"github.com/mmatczuk/jira-mcp/internal/jira"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFieldSchemaCache_GetReturnsSchema(t *testing.T) {
	mc := &mockClient{
		GetFieldsFn: func(context.Context) ([]jira.Field, error) {
			return []jira.Field{
				{ID: "customfield_1", Schema: jira.FieldSchema{Type: "string", Custom: "x:textarea"}},
				{ID: "customfield_2", Schema: jira.FieldSchema{Type: "string"}},
			}, nil
		},
	}
	cache, err := newFieldSchemaCache(context.Background(), mc)
	require.NoError(t, err)

	first, err := cache.get("customfield_1")
	require.NoError(t, err)
	assert.Equal(t, "x:textarea", first.Custom)

	second, err := cache.get("customfield_2")
	require.NoError(t, err)
	assert.Equal(t, "string", second.Type)
}

func TestFieldSchemaCache_UnknownFieldReturnsError(t *testing.T) {
	mc := &mockClient{
		GetFieldsFn: func(context.Context) ([]jira.Field, error) {
			return []jira.Field{
				{ID: "customfield_1", Schema: jira.FieldSchema{Type: "string"}},
			}, nil
		},
	}
	cache, err := newFieldSchemaCache(context.Background(), mc)
	require.NoError(t, err)

	_, err = cache.get("customfield_99")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customfield_99")
	assert.Contains(t, err.Error(), "jira_schema resource=fields")
}

func TestFieldSchemaCache_PropagatesClientError(t *testing.T) {
	mc := &mockClient{
		GetFieldsFn: func(context.Context) ([]jira.Field, error) {
			return nil, errors.New("auth expired")
		},
	}
	_, err := newFieldSchemaCache(context.Background(), mc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth expired")
}
