package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func signedPolicy(t *testing.T, repoID string) ([]byte, *cacheFixture) {
	e := newCacheFixture(t)
	p, _ := json.Marshal(CachedPolicy{RepoID: repoID, Version: 3, YAML: "levels: {}\n"})
	raw, _ := json.Marshal(Sign(p, e.priv, "k"))
	return raw, e
}

func TestPolicyCacheFileAndRead(t *testing.T) {
	raw, e := signedPolicy(t, "repo1")
	f := PolicyCacheFile(e.tmp)
	if f != filepath.Join(e.tmp, ".aisdlc/web/policy.json") {
		t.Fatal(f)
	}
	if _, ok := ReadPolicyCache(f, e.pub, "repo1"); ok {
		t.Error("thiếu file")
	}
	_ = os.MkdirAll(filepath.Dir(f), 0o755)
	_ = os.WriteFile(f, raw, 0o644)
	c, ok := ReadPolicyCache(f, e.pub, "repo1")
	if !ok || c.Version != 3 || c.YAML != "levels: {}\n" || c.RepoID != "repo1" {
		t.Fatalf("%v %+v", ok, c)
	}
	if _, ok := ReadPolicyCache(f, e.pub, "other"); ok {
		t.Error("sai repo")
	}
	if _, ok := ReadPolicyCache(f, nil, "repo1"); ok {
		t.Error("khóa nil")
	}
	other, _ := pair(t)
	if _, ok := ReadPolicyCache(f, other, "repo1"); ok {
		t.Error("sai khóa")
	}
}

func TestVerifyPolicy(t *testing.T) {
	raw, e := signedPolicy(t, "repo1")
	if c, ok := VerifyPolicy(raw, e.pub, "repo1"); !ok || c.Version != 3 {
		t.Fatalf("%v %+v", ok, c)
	}
	for _, bad := range [][]byte{nil, []byte("garbage"), []byte("null")} {
		if _, ok := VerifyPolicy(bad, e.pub, "repo1"); ok {
			t.Errorf("%q phải rỗng", bad)
		}
	}
	p, _ := json.Marshal(map[string]any{"repoId": "repo1", "version": 1})
	noYaml, _ := json.Marshal(Sign(p, e.priv, "k"))
	if _, ok := VerifyPolicy(noYaml, e.pub, "repo1"); ok {
		t.Error("thiếu yaml phải rỗng")
	}
}
