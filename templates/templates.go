// Package templates nhúng các template markdown/yaml của AISDLC.
package templates

import "embed"

// FS chứa brd.md, design.md, design.yaml, cases.full.yaml, cases.lite.yaml, config.yaml (init/setup) và
// CLAUDE.aisdlc.md, gitlab-ci.aisdlc.yml (setup).
//
//go:embed *.md *.yaml *.yml
var FS embed.FS
