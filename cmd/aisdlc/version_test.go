package main

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"aisdlc/internal/harness/harnessapi"
)

var vcsRe = regexp.MustCompile(`(?m)^\s*build\s+(vcs\.revision|vcs\.modified)=(\S+)`)

// vcsVersion là phiên bản mà buildinfo.Version tính cho binary bin (đọc qua `go version -m`).
func vcsVersion(t *testing.T, bin string) string {
	t.Helper()
	out, err := exec.Command("go", "version", "-m", bin).CombinedOutput()
	if err != nil {
		t.Fatalf("go version -m: %v\n%s", err, out)
	}
	rev, dirty := "", false
	for _, m := range vcsRe.FindAllStringSubmatch(string(out), -1) {
		if m[1] == "vcs.revision" {
			rev = m[2]
		} else {
			dirty = m[2] == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if dirty {
		rev += "+dirty"
	}
	return rev
}

// aisdlc và aisdlc-harness build từ cùng cây nhúng cùng phiên bản; harness ghi phiên bản đó vào harness-result.json
// để CLI kiểm (khác bản thì fail closed).
func TestBinariesEmbedSameBuildVersion(t *testing.T) {
	dir := t.TempDir()
	harness := filepath.Join(dir, "aisdlc-harness")
	if out, err := exec.Command("go", "build", "-o", harness, "aisdlc/cmd/aisdlc-harness").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	cli, h := vcsVersion(t, binPath), vcsVersion(t, harness)
	if cli != h {
		t.Fatalf("aisdlc %q, aisdlc-harness %q", cli, h)
	}
	out := filepath.Join(dir, "out")
	_ = exec.Command(harness, "run", "--root", dir, "--feature", "DEMO-1", "--out", out, "--mode", "lạ").Run()
	r, err := harnessapi.ReadResult(out)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != h || r.Error == "" {
		t.Errorf("harness-result.json: version %q (muốn %q), error %q", r.Version, h, r.Error)
	}
}
