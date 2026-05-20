package jiramcp

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmatczuk/jira-mcp/internal/jira"
)

type AttachmentsArgs struct {
	Action       string `json:"action" jsonschema:"One of: list, upload, download, delete."`
	Key          string `json:"key,omitempty" jsonschema:"Issue key. Required for list and upload."`
	AttachmentID string `json:"attachment_id,omitempty" jsonschema:"Attachment id. Required for download and delete."`
	Filename     string `json:"filename,omitempty" jsonschema:"Filename for upload. Mime type is inferred from the extension."`
	Content      string `json:"content,omitempty" jsonschema:"Text body for upload. Text mime types only; 5 MB cap."`
}

var attachmentsTool = &mcp.Tool{
	Name: "jira_attachments",
	Description: `List, upload, download, and delete issue attachments. Text mime types only (text/*, JSON, XML, YAML). 5 MB cap per body.

Actions:
- list: Returns attachment metadata for an issue. Requires key.
- upload: Posts a new text attachment to an issue. Requires key, filename, content.
- download: Returns the body of an attachment. Requires attachment_id.
- delete: Removes an attachment. Requires attachment_id.`,
}

func (h *handlers) handleAttachments(ctx context.Context, _ *mcp.CallToolRequest, args AttachmentsArgs) (*mcp.CallToolResult, any, error) {
	requireField := func(name string) (*mcp.CallToolResult, any, error) {
		return textResult(fmt.Sprintf("%s is required for action=%s.", name, args.Action), true), nil, nil
	}
	switch args.Action {
	case "list":
		if args.Key == "" {
			return requireField("key")
		}
		return h.attachmentsList(ctx, args.Key)
	case "upload":
		if args.Key == "" {
			return requireField("key")
		}
		if args.Filename == "" {
			return requireField("filename")
		}
		if args.Content == "" {
			return requireField("content")
		}
		return h.attachmentsUpload(ctx, args.Key, args.Filename, args.Content)
	case "download":
		if args.AttachmentID == "" {
			return requireField("attachment_id")
		}
		return h.attachmentsDownload(ctx, args.AttachmentID)
	case "delete":
		if args.AttachmentID == "" {
			return requireField("attachment_id")
		}
		return h.attachmentsDelete(ctx, args.AttachmentID)
	default:
		return textResult(fmt.Sprintf("action %q is invalid; valid actions: list, upload, download, delete.", args.Action), true), nil, nil
	}
}

func (h *handlers) attachmentsList(ctx context.Context, key string) (*mcp.CallToolResult, any, error) {
	issue, err := h.client.GetIssue(ctx, key, &jira.GetQueryOptions{Fields: "attachment"})
	if err != nil {
		return textResult(fmt.Sprintf("Failed to list attachments for %s: %v", key, err), true), nil, nil
	}
	out := []attachmentMeta{}
	if issue != nil && issue.Fields != nil {
		for _, a := range issue.Fields.Attachments {
			if a == nil {
				continue
			}
			out = append(out, attachmentMetaFrom(*a))
		}
	}
	data, _ := json.Marshal(out)
	return textResult(string(data), false), nil, nil
}

func (h *handlers) attachmentsUpload(ctx context.Context, key, filename, content string) (*mcp.CallToolResult, any, error) {
	declaredMIME := mime.TypeByExtension(filepath.Ext(filename))
	if err := validateTextAttachment(declaredMIME, []byte(content)); err != nil {
		return textResult(fmt.Sprintf("Upload rejected: %v", err), true), nil, nil
	}
	att, err := h.client.PostAttachmentText(ctx, key, filename, content)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to upload %s to %s: %v", filename, key, err), true), nil, nil
	}
	if att == nil {
		return textResult(fmt.Sprintf("Upload of %s to %s returned no metadata.", filename, key), true), nil, nil
	}
	data, _ := json.Marshal(attachmentMetaFrom(*att))
	return textResult(string(data), false), nil, nil
}

func (h *handlers) attachmentsDownload(ctx context.Context, id string) (*mcp.CallToolResult, any, error) {
	meta, err := h.client.GetAttachmentMeta(ctx, id)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to fetch attachment %s: %v", id, err), true), nil, nil
	}
	// Short-circuit on declared mime so we never pull a 5 MB binary body just
	// to reject it after the read.
	if meta.MimeType != "" && !isAllowedTextMime(meta.MimeType) {
		return textResult(fmt.Sprintf("Download rejected: text attachments only, got %q.", meta.MimeType), true), nil, nil
	}
	body, err := h.client.GetAttachmentBody(ctx, id, attachmentMaxBytes)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to fetch body for %s: %v", id, err), true), nil, nil
	}
	if err := validateTextAttachment(meta.MimeType, body); err != nil {
		return textResult(fmt.Sprintf("Download rejected: %v", err), true), nil, nil
	}
	return textResult(string(body), false), nil, nil
}

func (h *handlers) attachmentsDelete(ctx context.Context, id string) (*mcp.CallToolResult, any, error) {
	if err := h.client.DeleteAttachment(ctx, id); err != nil {
		return textResult(fmt.Sprintf("Failed to delete attachment %s: %v", id, err), true), nil, nil
	}
	return textResult(fmt.Sprintf("Deleted attachment %s.", id), false), nil, nil
}

// attachmentMeta is the wire shape returned to MCP callers for each
// attachment. The JSON tags match Jira's field names.
type attachmentMeta struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int    `json:"size"`
	Created  string `json:"created,omitempty"`
	Author   string `json:"author,omitempty"`
}

// attachmentMetaFrom projects a jira.Attachment into the wire shape.
func attachmentMetaFrom(a jira.Attachment) attachmentMeta {
	m := attachmentMeta{
		ID:       a.ID,
		Filename: a.Filename,
		MimeType: a.MimeType,
		Size:     a.Size,
		Created:  a.Created,
	}
	if a.Author != nil {
		m.Author = a.Author.DisplayName
	}
	return m
}

