package executor

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// textMIMEPrefixes lists MIME type prefixes that can be decoded as plain text.
// Files with these MIME types will have their base64 content inlined as text
// in the message, ensuring Claude can read them regardless of auth token type.
var textMIMEPrefixes = []string{
	"text/",
	"application/json",
	"application/xml",
	"application/csv",
	"application/x-csv",
	"application/javascript",
	"application/typescript",
	"application/x-yaml",
	"application/yaml",
	"application/toml",
	"application/x-sh",
}

func isTextMIMEType(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	for _, prefix := range textMIMEPrefixes {
		if strings.HasPrefix(mimeType, prefix) {
			return true
		}
	}
	return false
}

// convertDocumentsToText scans messages for document content blocks with text-based
// MIME types and replaces them with text blocks containing the decoded file content.
//
// This ensures compatibility with OAuth/subscription tokens that may not support
// the native document content type, as well as clients (e.g. Open WebUI) that
// upload files as base64-encoded document blocks.
//
// Only base64-encoded documents with text-compatible MIME types are converted.
// Image documents and binary formats (e.g. PDF) are left unchanged.
func convertDocumentsToText(body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}

	for msgIdx, msg := range messages.Array() {
		content := msg.Get("content")
		if !content.Exists() || !content.IsArray() {
			continue
		}

		var newParts []string
		changed := false

		for _, part := range content.Array() {
			if part.Get("type").String() != "document" {
				newParts = append(newParts, part.Raw)
				continue
			}

			source := part.Get("source")
			if !source.Exists() || source.Get("type").String() != "base64" {
				newParts = append(newParts, part.Raw)
				continue
			}

			mediaType := source.Get("media_type").String()
			if !isTextMIMEType(mediaType) {
				newParts = append(newParts, part.Raw)
				continue
			}

			data := source.Get("data").String()
			decoded, err := base64.StdEncoding.DecodeString(data)
			if err != nil {
				decoded, err = base64.RawStdEncoding.DecodeString(data)
			}
			if err != nil {
				// Cannot decode; keep the original document block unchanged
				newParts = append(newParts, part.Raw)
				continue
			}

			title := part.Get("title").String()
			var textContent string
			if title != "" {
				textContent = fmt.Sprintf("[File: %s]\n```\n%s\n```", title, string(decoded))
			} else {
				textContent = fmt.Sprintf("```\n%s\n```", string(decoded))
			}

			textPart := []byte(`{"type":"text","text":""}`)
			textPart, _ = sjson.SetBytes(textPart, "text", textContent)
			newParts = append(newParts, string(textPart))
			changed = true
		}

		if changed {
			newContent := "[" + strings.Join(newParts, ",") + "]"
			body, _ = sjson.SetRawBytes(body, fmt.Sprintf("messages.%d.content", msgIdx), []byte(newContent))
		}
	}

	return body
}
