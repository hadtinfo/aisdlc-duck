package snapshot

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
)

// CachedPolicy là payload đã ký: phiên bản trên web và YAML chính sách nguyên văn.
type CachedPolicy struct {
	RepoID  string `json:"repoId"`
	Version int    `json:"version"`
	YAML    string `json:"yaml"`
}

// PolicyCacheFile là .aisdlc/web/policy.json.
func PolicyCacheFile(repoRoot string) string {
	return filepath.Join(repoRoot, ".aisdlc/web/policy.json")
}

// ReadPolicyCache false khi không có, hỏng, sai chữ ký, sai repo, thiếu YAML hoặc chưa có khóa ghim.
func ReadPolicyCache(file string, pinned ed25519.PublicKey, repoID string) (CachedPolicy, bool) {
	if pinned == nil || file == "" || repoID == "" {
		return CachedPolicy{}, false
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return CachedPolicy{}, false
	}
	return VerifyPolicy(raw, pinned, repoID)
}

// VerifyPolicy như ReadPolicyCache trên bytes phong bì (CLI kiểm phản hồi của web trước khi ghi cache).
func VerifyPolicy(envelope []byte, pinned ed25519.PublicKey, repoID string) (CachedPolicy, bool) {
	if pinned == nil || envelope == nil || repoID == "" {
		return CachedPolicy{}, false
	}
	var se *SignedEnvelope
	if json.Unmarshal(envelope, &se) != nil || se == nil {
		return CachedPolicy{}, false
	}
	payload, ok := se.Verify(pinned)
	if !ok {
		return CachedPolicy{}, false
	}
	var c *struct {
		RepoID  string  `json:"repoId"`
		Version int     `json:"version"`
		YAML    *string `json:"yaml"`
	}
	if json.Unmarshal(payload, &c) != nil || c == nil || c.RepoID != repoID || c.YAML == nil {
		return CachedPolicy{}, false
	}
	return CachedPolicy{RepoID: c.RepoID, Version: c.Version, YAML: *c.YAML}, true
}
