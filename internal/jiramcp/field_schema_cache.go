package jiramcp

import (
	"context"
	"fmt"

	"github.com/mmatczuk/jira-mcp/internal/jira"
)

// fieldSchemaCache resolves field IDs to their FieldSchema from a single
// GetFields call. Sibling to createMetaCache: scoped to a single batch
// (handleWrite or handleRead invocation), thrown away when the call returns.
type fieldSchemaCache struct {
	byID map[string]jira.FieldSchema
}

func newFieldSchemaCache(ctx context.Context, client JiraClient) (*fieldSchemaCache, error) {
	fields, err := client.GetFields(ctx)
	if err != nil {
		return nil, fmt.Errorf("list fields: %w", err)
	}
	byID := make(map[string]jira.FieldSchema, len(fields))
	for _, f := range fields {
		byID[f.ID] = f.Schema
	}
	return &fieldSchemaCache{byID: byID}, nil
}

func (c *fieldSchemaCache) get(fieldID string) (jira.FieldSchema, error) {
	schema, ok := c.byID[fieldID]
	if !ok {
		return jira.FieldSchema{}, fmt.Errorf("unknown field %q — use jira_schema resource=fields to discover field IDs", fieldID)
	}
	return schema, nil
}
