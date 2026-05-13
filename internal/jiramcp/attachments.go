package jiramcp

import (
	"bytes"
	"fmt"
	"mime"
	"net/http"
	"strings"
)

// attachmentMaxBytes is the symmetric 5 MB cap applied to attachment uploads
// and downloads. Larger payloads round-trip badly through MCP tool args and
// results, and the cap also short-circuits accidental binary fetches.
const attachmentMaxBytes int64 = 5 * 1024 * 1024

// textMimeAllowlist holds the exact mime types accepted for text attachments
// in v1, in addition to any subtype of text/*. Lookup is via the trimmed,
// lowercased base mime; charset and other parameters are stripped first.
var textMimeAllowlist = map[string]struct{}{
	"application/json":       {},
	"application/x-ndjson":   {},
	"application/xml":        {},
	"application/x-yaml":     {},
	"application/yaml":       {},
	"application/javascript": {},
	"application/x-sh":       {},
	"application/toml":       {},
}

// validateTextAttachment enforces the text-only v1 policy on the (declared
// mime, body bytes) pair. Two checks must pass:
//
//   - The declared mime, if non-empty, is in the text allowlist.
//   - The body bytes don't look binary — no NUL byte in the first 8 KB, and
//     http.DetectContentType on the first 512 bytes also passes the allowlist.
//
// An empty declared mime ("agent upload, mime not yet known") is accepted as
// a hint to rely on byte sniffing alone.
func validateTextAttachment(declaredMIME string, body []byte) error {
	if declaredMIME != "" {
		if !isAllowedTextMime(declaredMIME) {
			return fmt.Errorf("text attachments only: declared mime %q is not in the allowlist", declaredMIME)
		}
	}
	if hasNulByte(body) {
		return fmt.Errorf("binary content detected: body contains a NUL byte")
	}
	sniff := http.DetectContentType(body)
	if !isAllowedTextMime(sniff) {
		return fmt.Errorf("binary content detected: sniffed type %q is not text", sniff)
	}
	return nil
}

// isAllowedTextMime returns true if m is text/* or in the explicit allowlist.
// The check tolerates a "; charset=..." or other parameter suffix.
func isAllowedTextMime(m string) bool {
	base, _, err := mime.ParseMediaType(m)
	if err != nil {
		base = strings.ToLower(strings.TrimSpace(m))
	}
	if strings.HasPrefix(base, "text/") {
		return true
	}
	_, ok := textMimeAllowlist[base]
	return ok
}

// hasNulByte reports whether b contains 0x00 in its first 8 KB. Text formats
// do not embed NULs; presence is a strong binary signal that catches files
// where http.DetectContentType returns application/octet-stream and gives up.
func hasNulByte(b []byte) bool {
	const window = 8 * 1024
	if len(b) > window {
		b = b[:window]
	}
	return bytes.IndexByte(b, 0x00) >= 0
}
