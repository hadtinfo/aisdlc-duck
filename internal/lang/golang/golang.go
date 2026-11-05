// Package golang là adapter ngôn ngữ Go.
package golang

import "strings"

// Adapter cài đặt lang.Adapter cho Go.
type Adapter struct{}

func (Adapter) Name() string { return "go" }

func (Adapter) SampleFileName() string { return "aisdlcx.go" }

// IsSource: file .go, không phải _test.go, không nằm trong vendor/ hay testdata/.
func (Adapter) IsSource(p string) bool {
	if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
		return false
	}
	segs := strings.Split(p, "/")
	for _, s := range segs[:len(segs)-1] {
		if s == "vendor" || s == "testdata" {
			return false
		}
	}
	return true
}
