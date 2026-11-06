package main

import (
	"os/exec"
	"strings"
	"testing"
)

// Hook của Claude Code chạy binary aisdlc ở mỗi lần gọi công cụ: init của testcontainers-go (gopsutil quét mọi tiến
// trình) tốn ~25 ms nên harness nằm ở binary aisdlc-harness riêng. Gói nào kéo nó vào aisdlc thì test này fail.
func TestHookBinaryHasNoTestcontainers(t *testing.T) {
	for _, pkg := range []string{".", "aisdlc/internal/eval", "aisdlc/internal/webclient", "aisdlc/internal/harness/harnessapi"} {
		out, err := exec.Command("go", "list", "-deps", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
		}
		for _, dep := range strings.Fields(string(out)) {
			if strings.HasPrefix(dep, "github.com/testcontainers/") || strings.HasPrefix(dep, "github.com/shirou/gopsutil") ||
				dep == "aisdlc/internal/harness" {
				t.Errorf("%s phụ thuộc %s", pkg, dep)
			}
		}
	}
}
