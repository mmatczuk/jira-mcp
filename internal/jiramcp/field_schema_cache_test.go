package jiramcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/mmatczuk/jira-mcp/internal/jira"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFieldSchemaCache_LoadsOnceAcrossLookups(t *testing.T) {
	var calls atomic.Int32
	mc := &mockClient{
		GetFieldsFn: func(context.Context) ([]jira.Field, error) {
			calls.Add(1)
			return []jira.Field{
				{ID: "customfield_1", Schema: jira.FieldSchema{Type: "string", Custom: "x:textarea"}},
				{ID: "customfield_2", Schema: jira.FieldSchema{Type: "string"}},
			}, nil
		},
	}
	cache := newFieldSchemaCache(mc)

	first, err := cache.get(context.Background(), "customfield_1")
	require.NoError(t, err)
	assert.Equal(t, "x:textarea", first.Custom)

	second, err := cache.get(context.Background(), "customfield_2")
	require.NoError(t, err)
	assert.Equal(t, "string", second.Type)

	assert.EqualValues(t, 1, calls.Load(), "GetFields should be called exactly once across lookups")
}

func TestFieldSchemaCache_UnknownFieldReturnsError(t *testing.T) {
	mc := &mockClient{
		GetFieldsFn: func(context.Context) ([]jira.Field, error) {
			return []jira.Field{
				{ID: "customfield_1", Schema: jira.FieldSchema{Type: "string"}},
			}, nil
		},
	}
	cache := newFieldSchemaCache(mc)

	_, err := cache.get(context.Background(), "customfield_99")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "customfield_99")
	assert.Contains(t, err.Error(), "jira_schema resource=fields")
}

func TestFieldSchemaCache_PropagatesClientErrorToEveryCaller(t *testing.T) {
	var calls atomic.Int32
	mc := &mockClient{
		GetFieldsFn: func(context.Context) ([]jira.Field, error) {
			calls.Add(1)
			return nil, errors.New("auth expired")
		},
	}
	cache := newFieldSchemaCache(mc)

	_, err1 := cache.get(context.Background(), "customfield_1")
	_, err2 := cache.get(context.Background(), "customfield_2")
	require.Error(t, err1)
	require.Error(t, err2)
	assert.Contains(t, err1.Error(), "auth expired")
	assert.Contains(t, err2.Error(), "auth expired")
	assert.EqualValues(t, 1, calls.Load(), "client error must not trigger retries within a batch")
}
