package jiramcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTextAttachment_Accepts(t *testing.T) {
	cases := []struct {
		name string
		mime string
		body []byte
	}{
		{"text/plain", "text/plain", []byte("hello")},
		{"text/plain with charset", "text/plain; charset=utf-8", []byte("hello")},
		{"application/json", "application/json", []byte(`{"a":1}`)},
		{"application/xml", "application/xml", []byte("<root/>")},
		{"text/csv", "text/csv", []byte("a,b\n1,2\n")},
		{"empty mime, bytes look text", "", []byte("just plain text")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.NoError(t, validateTextAttachment(tc.mime, tc.body))
		})
	}
}

func TestValidateTextAttachment_Rejects(t *testing.T) {
	pngHeader := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}
	cases := []struct {
		name        string
		mime        string
		body        []byte
		errContains string
	}{
		{"declared binary mime", "image/png", []byte("ignored"), "image/png"},
		{"declared text but NUL byte in body", "text/plain", []byte("hello\x00world"), "binary content"},
		{"declared text but PNG signature in body", "text/plain", pngHeader, "binary content"},
		{"empty mime, bytes look binary", "", pngHeader[:8], "binary content"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTextAttachment(tc.mime, tc.body)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}
