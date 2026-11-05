package snapshot

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"

	"aisdlc/internal/model"
)

// ControlCacheFile là .aisdlc/web/control/<KEY>.json.
func ControlCacheFile(repoRoot string, key model.FeatureKey) string {
	return filepath.Join(repoRoot, ".aisdlc/web/control", key.String()+".json")
}

// ReadControlCache chỉ trả bản đã xác minh; false khi không có, hỏng, sai chữ ký, sai repo/tính năng hoặc chưa có
// khóa ghim. Không bao giờ trả lỗi.
func ReadControlCache(file string, pinned ed25519.PublicKey, repoID string, key model.FeatureKey) (FeatureControl, bool) {
	if pinned == nil || file == "" || repoID == "" || key == "" {
		return FeatureControl{}, false
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return FeatureControl{}, false
	}
	var se *SignedEnvelope
	if json.Unmarshal(raw, &se) != nil || se == nil {
		return FeatureControl{}, false
	}
	payload, ok := se.Verify(pinned)
	if !ok {
		return FeatureControl{}, false
	}
	c, err := FeatureControlFromJSON(payload)
	if err != nil || c.Repo != repoID || c.Feature != key.String() {
		return FeatureControl{}, false
	}
	return c, true
}
