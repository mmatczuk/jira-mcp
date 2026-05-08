package jiramcp

import (
	"context"
	"fmt"
	"sync"

	"github.com/mmatczuk/jira-mcp/internal/jira"
)

// fieldSchemaCache resolves field IDs to their FieldSchema with one
// GetFields call per cache lifetime, regardless of how many lookups happen.
// Sibling to createMetaCache: scoped to a single batch (handleWrite or
// handleRead invocation), thrown away when the call returns.
//
// sync.Once is the right primitive here because GetFields fetches the full
// catalogue in one round-trip — there's no per-field call to retry. A
// transient client error is returned once and not retried within the batch;
// the next user-issued request gets a fresh cache and another attempt.
type fieldSchemaCache struct {
	client JiraClient
	once   sync.Once
	byID   map[string]jira.FieldSchema
	err    error
}

func newFieldSchemaCache(client JiraClient) *fieldSchemaCache {
	return &fieldSchemaCache{client: client}
}

// get returns the schema for fieldID, populating the cache lazily on first
// call. Returns an error if the underlying GetFields call failed or if
// fieldID is not present in the catalogue.
func (c *fieldSchemaCache) get(ctx context.Context, fieldID string) (jira.FieldSchema, error) {
	c.once.Do(func() {
		fields, err := c.client.GetFields(ctx)
		if err != nil {
			c.err = fmt.Errorf("list fields: %w", err)
			return
		}
		c.byID = make(map[string]jira.FieldSchema, len(fields))
		for _, f := range fields {
			c.byID[f.ID] = f.Schema
		}
	})
	if c.err != nil {
		return jira.FieldSchema{}, c.err
	}
	schema, ok := c.byID[fieldID]
	if !ok {
		return jira.FieldSchema{}, fmt.Errorf("unknown field %q — use jira_schema resource=fields to discover field IDs", fieldID)
	}
	return schema, nil
}
