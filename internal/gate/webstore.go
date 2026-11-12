package gate

import (
	"crypto/ed25519"
	"encoding/json"
	"os"

	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
)

// readOnlyMsg là lỗi khi ghi trạng thái cổng cục bộ ở chế độ duyệt trên web.
func readOnlyMsg() string {
	return i18n.T("Ở chế độ duyệt trên web, trạng thái cổng do web quản lý; không ghi được cục bộ.")
}

// WebGateStore là trạng thái cổng ở chế độ duyệt trên web: chỉ đọc từ phong bì đã ký trong cache. Phong bì không
// hợp lệ, của repo/tính năng khác, hoặc chưa có khóa ghim thì coi như chưa có submission/approval nào.
type WebGateStore struct {
	cacheFile string
	pinned    ed25519.PublicKey
	repoID    string
	key       model.FeatureKey

	loaded bool
	state  snapshot.FeatureGateState
	ok     bool
}

// NewWebGateStore tạo store đọc cacheFile.
func NewWebGateStore(cacheFile string, pinned ed25519.PublicKey, repoID string, key model.FeatureKey) *WebGateStore {
	return &WebGateStore{cacheFile: cacheFile, pinned: pinned, repoID: repoID, key: key}
}

// State là trạng thái đã xác minh trong cache (đọc lại sau Refresh).
func (s *WebGateStore) State() (snapshot.FeatureGateState, bool) {
	if !s.loaded {
		s.state, s.ok = ReadWebGateState(s.cacheFile, s.pinned, s.repoID, s.key)
		s.loaded = true
	}
	return s.state, s.ok
}

func (s *WebGateStore) Refresh() error { s.loaded = false; return nil }

func (s *WebGateStore) Submission(g Gate) (Submission, bool, error) {
	st, ok := s.State()
	if !ok {
		return Submission{}, false, nil
	}
	var found *snapshot.Sub
	for i := range st.Submissions {
		if st.Submissions[i].Gate == string(g) {
			found = &st.Submissions[i]
		}
	}
	if found == nil {
		return Submission{}, false, nil
	}
	f := found.Findings
	if f == nil {
		f = emptyFindings()
	}
	return Submission{Gate: g, Hash: found.Hash, At: found.At, Findings: f}, true, nil
}

func lastAppr(items []snapshot.Appr, g Gate) (Approval, bool) {
	var found *snapshot.Appr
	for i := range items {
		if items[i].Gate == string(g) {
			found = &items[i]
		}
	}
	if found == nil {
		return Approval{}, false
	}
	return ApprovalFromWeb(g, found.Hash, found.Approver, found.At, found.Channel, found.Rule), true
}

// Approval là bản duyệt hiệu lực của cổng; không có thì bản tự duyệt đã bị thay thế khi nâng mức (đánh dấu
// Superseded, không tính là đã duyệt) để hiển thị như chế độ local.
func (s *WebGateStore) Approval(g Gate) (Approval, bool, error) {
	st, ok := s.State()
	if !ok {
		return Approval{}, false, nil
	}
	if a, ok := lastAppr(st.Approvals, g); ok {
		return a, true, nil
	}
	if a, ok := lastAppr(st.Superseded, g); ok && a.IsAuto() {
		return a.AsSuperseded(), true, nil
	}
	return Approval{}, false, nil
}

func (s *WebGateStore) SaveSubmission(Submission) error { return errf(readOnlyMsg()) }
func (s *WebGateStore) SaveApproval(Approval) error     { return errf(readOnlyMsg()) }
func (s *WebGateStore) Clear(Gate) error                { return errf(readOnlyMsg()) }

// ReadWebGateState đọc và xác minh phong bì trong cacheFile; false khi không có, hỏng, sai chữ ký, sai
// repo/tính năng hoặc chưa có khóa ghim. Không bao giờ trả lỗi.
func ReadWebGateState(cacheFile string, pinned ed25519.PublicKey, repoID string, key model.FeatureKey) (snapshot.FeatureGateState, bool) {
	var zero snapshot.FeatureGateState
	if pinned == nil || cacheFile == "" || repoID == "" || key == "" {
		return zero, false
	}
	raw, err := os.ReadFile(cacheFile)
	if err != nil {
		return zero, false
	}
	var env *snapshot.SignedEnvelope
	if json.Unmarshal(raw, &env) != nil || env == nil {
		return zero, false
	}
	payload, ok := env.Verify(pinned)
	if !ok {
		return zero, false
	}
	st, err := snapshot.FeatureGateStateFromJSON(payload)
	if err != nil || st.Repo != repoID || st.Feature != key.String() {
		return zero, false
	}
	return st, true
}
