// Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
// Use of this source code is governed by the GNU AGPL v3
// license that can be found in the LICENSE file.

package runbooks

import (
	"bytes"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var markdownConverter = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
)

// UGC policy for runbook HTML served into the control-plane UI.
var htmlPolicy = bluemonday.UGCPolicy()

// MarkdownToHTML converts runbook markdown to a sanitized HTML fragment.
func MarkdownToHTML(content string) (string, error) {
	var buf bytes.Buffer
	if err := markdownConverter.Convert([]byte(content), &buf); err != nil {
		return "", err
	}
	return htmlPolicy.Sanitize(buf.String()), nil
}
