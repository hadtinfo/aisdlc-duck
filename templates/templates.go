// Package templates nhúng các template markdown/yaml của AISDLC.
package templates

import "embed"

// FS chứa brd.md, design.md, design.yaml, cases.full.yaml, cases.lite.yaml.
//
//go:embed *.md *.yaml
var FS embed.FS
