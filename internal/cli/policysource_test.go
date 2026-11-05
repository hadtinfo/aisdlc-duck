package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aisdlc/internal/gitx"
	"aisdlc/internal/risk"
	"aisdlc/internal/snapshot"
)

// PolicySourceTest.

type policyFixture struct {
	*testRepo
	home   string
	repoID string
	errBuf *bytes.Buffer
}

func newPolicyFixture(t *testing.T) *policyFixture {
	r := newTestRepo(t)
	r.ok(noConsole(), "setup")
	home := t.TempDir()
	r.env["HOME"] = home
	return &policyFixture{testRepo: r, home: home,
		repoID: snapshot.RepoID(context.Background(), gitx.New(r.root), r.root), errBuf: &bytes.Buffer{}}
}

func (p *policyFixture) ctx() *Context { return p.context(noConsole(), "", &bytes.Buffer{}, p.errBuf) }

// webLogin như `aisdlc web login`: cấu hình web và ghim khóa; trả khóa riêng để ký cache.
func (p *policyFixture) webLogin() ed25519.PrivateKey {
	p.env["AISDLC_WEB_URL"] = "http://127.0.0.1:9"
	p.env["AISDLC_WEB_TOKEN"] = "ingest-tok"
	_, priv := pinKey(p.t, p.home)
	return priv
}

func (p *policyFixture) writeCache(signer ed25519.PrivateKey, repo string, version int, yaml string) {
	p.t.Helper()
	b, err := json.Marshal(snapshot.CachedPolicy{RepoID: repo, Version: version, YAML: yaml})
	must(p.t, err)
	rel, err := filepath.Rel(p.root, snapshot.PolicyCacheFile(p.root))
	must(p.t, err)
	p.writeSigned(rel, b, signer)
}

func presetVersionYAML(t *testing.T, name, version string) string {
	t.Helper()
	y, err := risk.PresetYAML(name, version)
	must(t, err)
	return y
}

func wantAbort(t *testing.T, err error, code int, subs ...string) {
	t.Helper()
	var a *Abort
	if !asAbort(err, &a) {
		t.Fatalf("phải là Abort, got %v", err)
	}
	eq(t, a.Code, code)
	contains(t, a.Msg, subs...)
}

func TestFileModeReadsRepoFile(t *testing.T) {
	p := newPolicyFixture(t)
	p.write(policyFile, strings.Replace(presetVersionYAML(t, "banking", "1"), "schema: 1", "schema: 1\nversion: 7", 1))
	l, err := LoadPolicy(p.ctx(), p.root, false)
	must(t, err)
	eq(t, l.Origin, "file")
	eq(t, l.Policy.Version, 7)
	eq(t, l.Policy.Preset.Name, "banking")
	eq(t, p.errBuf.String(), "")
}

func TestFileModeMissingFileGenericWithWarning(t *testing.T) {
	p := newPolicyFixture(t)
	must(t, os.Remove(filepath.Join(p.root, policyFile)))
	l, err := LoadPolicy(p.ctx(), p.root, false)
	must(t, err)
	eq(t, l.Origin, "file")
	eq(t, l.Policy.Preset.Name, "generic")
	contains(t, p.errBuf.String(), "Chưa có aisdlc/risk-policy.yaml; dùng mẫu generic. Chạy aisdlc setup --preset")
}

func TestFileModeBrokenPolicyAbortsListingErrors(t *testing.T) {
	p := newPolicyFixture(t)
	p.write(policyFile, strings.Replace(presetVersionYAML(t, "generic", "1"), "level: medium, note: Đổi cấu",
		"level: extreme, note: Đổi cấu", 1))
	_, err := LoadPolicy(p.ctx(), p.root, false)
	wantAbort(t, err, 2, "G2", "extreme")
}

func TestLoadWebModeMissingCacheAborts(t *testing.T) {
	p := newPolicyFixture(t)
	p.webLogin()
	_, err := LoadPolicy(p.ctx(), p.root, false)
	wantAbort(t, err, 2, "Chưa có chính sách rủi ro từ web (hoặc chữ ký sai). Chạy: aisdlc policy pull")
}

func TestLoadWebModeBadSignatureAborts(t *testing.T) {
	p := newPolicyFixture(t)
	priv := p.webLogin()
	_, other, err := ed25519.GenerateKey(nil)
	must(t, err)
	p.writeCache(other, p.repoID, 2, presetVersionYAML(t, "banking", "1"))
	_, err = LoadPolicy(p.ctx(), p.root, false)
	wantAbort(t, err, 2, "aisdlc policy pull")
	// đúng khóa nhưng repo khác cũng không nhận
	p.writeCache(priv, "other/repo", 2, presetVersionYAML(t, "banking", "1"))
	_, err = LoadPolicy(p.ctx(), p.root, false)
	wantAbort(t, err, 2)
}

func TestWebModeValidCache(t *testing.T) {
	p := newPolicyFixture(t)
	priv := p.webLogin()
	p.writeCache(priv, p.repoID, 5, presetVersionYAML(t, "banking", "1"))
	l, err := LoadPolicy(p.ctx(), p.root, false)
	must(t, err)
	eq(t, l.Origin, "web")
	eq(t, l.Policy.Version, 5)
	eq(t, l.Policy.Preset.Name, "banking") // không dùng file generic trong repo
}
