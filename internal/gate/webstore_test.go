package gate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/spec"
)

const (
	repoID = "git.corp.vn/team/pay"
	keyPay = model.FeatureKey("PAY-1")
)

var tw = time.Date(2026, 11, 3, 3, 0, 0, 0, time.UTC)

type webFx struct {
	t     *testing.T
	pub   ed25519.PublicKey
	priv  ed25519.PrivateKey
	cache string
}

func newWebFx(t *testing.T) *webFx {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &webFx{t, pub, priv, filepath.Join(t.TempDir(), ".aisdlc/web/gates/PAY-1.json")}
}

func webState(repo, feature, designHash string) snapshot.FeatureGateState {
	return snapshot.FeatureGateState{
		Repo: repo, Feature: feature, Mode: "web", IssuedAt: tw,
		Submissions: []snapshot.Sub{
			{Gate: "BRD", Hash: "h-brd", At: tw, Findings: []spec.Finding{}},
			{Gate: "DESIGN", Hash: designHash, At: tw, Findings: []spec.Finding{{Code: "X", Message: "nhắc"}}},
		},
		Approvals: []snapshot.Appr{
			{Gate: "BRD", Hash: "h-brd", Approver: "an", At: tw, Channel: "web"},
			{Gate: "DESIGN", Hash: designHash, Approver: "an", At: tw, Channel: "terminal"},
		},
		Feedback: []snapshot.Fb{{ID: 7, Gate: "DESIGN", Kind: "REJECTED", By: "binh", At: tw, Comment: "sửa bảng"}},
	}
}

func (w *webFx) write(s snapshot.FeatureGateState, priv ed25519.PrivateKey, pub ed25519.PublicKey) {
	w.t.Helper()
	payload, err := s.ToJSON()
	if err != nil {
		w.t.Fatal(err)
	}
	signed := snapshot.Sign(payload, priv, snapshot.KeyID(pub))
	b, _ := json.Marshal(signed)
	write(w.t, w.cache, string(b))
}

func (w *webFx) writeOK(s snapshot.FeatureGateState) { w.write(s, w.priv, w.pub) }

func (w *webFx) store() *WebGateStore { return NewWebGateStore(w.cache, w.pub, repoID, keyPay) }

func approvalOf(t *testing.T, s GateStore, g Gate) (Approval, bool) {
	t.Helper()
	a, ok, err := s.Approval(g)
	if err != nil {
		t.Fatal(err)
	}
	return a, ok
}

func TestAutoOnlyWhenChannelAuto(t *testing.T) {
	w := newWebFx(t)
	w.writeOK(snapshot.FeatureGateState{Repo: repoID, Feature: "PAY-1", Mode: "web", IssuedAt: tw,
		Approvals: []snapshot.Appr{
			{Gate: "BRD", Hash: "h1", Approver: "auto", At: tw, Channel: "auto", Rule: "flow:low"},
			{Gate: "DESIGN", Hash: "h2", Approver: "auto", At: tw, Channel: "web", Rule: "flow:low"},
		}})
	s := w.store()
	brd, _ := approvalOf(t, s, BRD)
	if !brd.IsAuto() || brd.Rule != "flow:low" {
		t.Errorf("%+v", brd)
	}
	d, _ := approvalOf(t, s, DESIGN)
	if d.IsAuto() || d.Rule != "" {
		t.Errorf("%+v", d)
	}
}

func TestManualApprovalKeepsChannel(t *testing.T) {
	w := newWebFx(t)
	w.writeOK(snapshot.FeatureGateState{Repo: repoID, Feature: "PAY-1", Mode: "web", IssuedAt: tw,
		Approvals: []snapshot.Appr{{Gate: "BRD", Hash: "h1", Approver: "an", At: tw, Channel: ApprovalClaudeCode, Rule: "flow:low"}}})
	a, _ := approvalOf(t, w.store(), BRD)
	if a.Channel != ApprovalClaudeCode || a.IsAuto() || a.Rule != "" {
		t.Errorf("%+v", a)
	}
	if ApprovalFromWeb(BRD, "h", "auto", tw, "auto", "flow:low").Channel != "" {
		t.Error("channel auto thì channel rỗng")
	}
	if x := ApprovalFromWeb(BRD, "h", "auto", tw, "web", ""); x.Approver != "auto (web)" || x.IsAuto() {
		t.Errorf("%+v", x)
	}
}

func TestSupersededAutoApprovalIsShownButNotActive(t *testing.T) {
	w := newWebFx(t)
	w.writeOK(snapshot.FeatureGateState{Repo: repoID, Feature: "PAY-1", Mode: "web", IssuedAt: tw,
		Approvals:  []snapshot.Appr{{Gate: "DESIGN", Hash: "h2", Approver: "dev", At: tw, Channel: "web"}},
		Superseded: []snapshot.Appr{{Gate: "BRD", Hash: "h1", Approver: "auto", At: tw, Channel: "auto", Rule: "flow:low"}}})
	s := w.store()
	brd, ok := approvalOf(t, s, BRD)
	if !ok || !brd.Superseded || !brd.IsAuto() || brd.RuleLevel() != "low" {
		t.Errorf("%+v", brd)
	}
	if d, _ := approvalOf(t, s, DESIGN); d.Superseded {
		t.Error("DESIGN không superseded")
	}
}

func TestOldEnvelopeWithoutSupersededStillReads(t *testing.T) {
	w := newWebFx(t)
	raw := `{"repo":"` + repoID + `","feature":"PAY-1","mode":"web","issuedAt":"2026-11-03T03:00:00Z",` +
		`"submissions":[],"approvals":[{"gate":"BRD","hash":"h1","approver":"an","at":"2026-11-03T03:00:00Z","channel":"web"}],"feedback":[]}`
	st, err := snapshot.FeatureGateStateFromJSON([]byte(raw))
	if err != nil || len(st.Superseded) != 0 {
		t.Fatalf("%v %v", st, err)
	}
	w.writeOK(st)
	if a, _ := approvalOf(t, w.store(), BRD); a.Superseded {
		t.Error("không superseded")
	}
	b, _ := snapshot.FeatureGateState{Repo: repoID, Feature: "PAY-1", Mode: "web", IssuedAt: tw}.ToJSON()
	if strings.Contains(string(b), "superseded") {
		t.Errorf("%s", b)
	}
}

func TestValidEnvelopeGivesApprovalAndSubmission(t *testing.T) {
	w := newWebFx(t)
	w.writeOK(webState(repoID, "PAY-1", "h-design"))
	s := w.store()
	a, ok := approvalOf(t, s, DESIGN)
	want := Approval{Gate: DESIGN, Hash: "h-design", Approver: "an", At: tw, Channel: "terminal"}
	if !ok || a != want {
		t.Errorf("%+v want %+v", a, want)
	}
	sub, ok, _ := s.Submission(DESIGN)
	if !ok || sub.Hash != "h-design" || !sub.At.Equal(tw) || len(sub.Findings) != 1 || sub.Findings[0].Code != "X" {
		t.Errorf("%+v", sub)
	}
	if _, ok := approvalOf(t, s, CASES); ok {
		t.Error("CASES không có")
	}
	st, ok := s.State()
	if !ok || st.Feedback[0].Comment != "sửa bảng" {
		t.Errorf("%+v", st)
	}
}

func TestWrongSignatureIsEmpty(t *testing.T) {
	w := newWebFx(t)
	pub2, priv2, _ := ed25519.GenerateKey(rand.Reader)
	w.write(webState(repoID, "PAY-1", "h-design"), priv2, pub2)
	if _, ok := approvalOf(t, w.store(), DESIGN); ok {
		t.Error("sai khóa phải rỗng")
	}
	if _, ok := w.store().State(); ok {
		t.Error("State phải rỗng")
	}
}

func TestTamperedFileIsEmpty(t *testing.T) {
	w := newWebFx(t)
	w.writeOK(webState(repoID, "PAY-1", "h-design"))
	b, _ := os.ReadFile(w.cache)
	write(t, w.cache, strings.Replace(string(b), `"sig":"`, `"sig":"AAAA`, 1))
	if _, ok := approvalOf(t, w.store(), DESIGN); ok {
		t.Error("sig sửa phải rỗng")
	}
	write(t, w.cache, "{không phải json")
	if _, ok := approvalOf(t, w.store(), DESIGN); ok {
		t.Error("json hỏng phải rỗng")
	}
}

func TestWebStoreTamperedCacheIsEmpty(t *testing.T) {
	w := newWebFx(t)
	// payload sửa 1 byte nhưng giữ chữ ký
	w.writeOK(webState(repoID, "PAY-1", "h-design"))
	var signed snapshot.SignedEnvelope
	b, _ := os.ReadFile(w.cache)
	if err := json.Unmarshal(b, &signed); err != nil {
		t.Fatal(err)
	}
	raw := []byte(signed.Payload)
	i := len(raw) / 2
	if raw[i] == 'A' {
		raw[i] = 'B'
	} else {
		raw[i] = 'A'
	}
	signed.Payload = string(raw)
	b, _ = json.Marshal(signed)
	write(t, w.cache, string(b))
	s := w.store()
	a, ok, err := s.Approval(DESIGN)
	if ok || err != nil || a != (Approval{}) {
		t.Errorf("payload sửa: %+v %v %v", a, ok, err)
	}
	// ký đúng nhưng bằng khóa khác
	pub2, priv2, _ := ed25519.GenerateKey(rand.Reader)
	w.write(webState(repoID, "PAY-1", "h-design"), priv2, pub2)
	s = w.store()
	if _, ok, err := s.Approval(DESIGN); ok || err != nil {
		t.Errorf("khóa khác: %v %v", ok, err)
	}
	if _, ok, err := s.Submission(DESIGN); ok || err != nil {
		t.Errorf("khóa khác (submission): %v %v", ok, err)
	}
}

func TestOtherRepoOrFeatureIsEmpty(t *testing.T) {
	w := newWebFx(t)
	w.writeOK(webState("git.corp.vn/team/other", "PAY-1", "h-design"))
	if _, ok := approvalOf(t, w.store(), DESIGN); ok {
		t.Error("repo khác")
	}
	w.writeOK(webState(repoID, "PAY-2", "h-design"))
	if _, ok := approvalOf(t, w.store(), DESIGN); ok {
		t.Error("tính năng khác")
	}
	if _, ok := ReadWebGateState(w.cache, w.pub, repoID, "PAY-2"); !ok {
		t.Error("PAY-2 phải đọc được")
	}
}

func TestMissingFileOrNoPinnedKeyIsEmpty(t *testing.T) {
	w := newWebFx(t)
	if _, ok := approvalOf(t, w.store(), BRD); ok {
		t.Error("thiếu file")
	}
	if _, ok := w.store().State(); ok {
		t.Error("thiếu file")
	}
	w.writeOK(webState(repoID, "PAY-1", "h-design"))
	if _, ok := approvalOf(t, NewWebGateStore(w.cache, nil, repoID, keyPay), BRD); ok {
		t.Error("không có khóa ghim")
	}
}

func TestWritesAreRejected(t *testing.T) {
	s := newWebFx(t).store()
	err := s.SaveApproval(Approval{Gate: BRD, Hash: "h", Approver: "an", At: tw})
	if !isGateErr(err) || !strings.HasPrefix(err.Error(), "Ở chế độ duyệt trên web, ") {
		t.Errorf("%v", err)
	}
	if !isGateErr(s.SaveSubmission(Submission{Gate: BRD, Hash: "h", At: tw})) || !isGateErr(s.Clear(BRD)) {
		t.Error("phải lỗi")
	}
}

func TestRefreshRereadsFile(t *testing.T) {
	w := newWebFx(t)
	w.writeOK(webState(repoID, "PAY-1", "h1"))
	s := w.store()
	hash := func() string { a, _ := approvalOf(t, s, DESIGN); return a.Hash }
	if hash() != "h1" {
		t.Fatal("h1")
	}
	w.writeOK(webState(repoID, "PAY-1", "h2"))
	if hash() != "h1" {
		t.Fatal("chưa Refresh vẫn h1")
	}
	s.Refresh()
	if hash() != "h2" {
		t.Fatal("sau Refresh h2")
	}
}

func TestStateJsonRoundTripsWithIsoInstants(t *testing.T) {
	s := webState(repoID, "PAY-1", "h")
	b, err := s.ToJSON()
	if err != nil || !strings.Contains(string(b), `"issuedAt":"2026-11-03T03:00:00Z"`) {
		t.Fatalf("%s %v", b, err)
	}
	got, err := snapshot.FeatureGateStateFromJSON(b)
	if err != nil || !got.IssuedAt.Equal(s.IssuedAt) || got.Feedback[0].Comment != "sửa bảng" || len(got.Approvals) != 2 {
		t.Errorf("%+v %v", got, err)
	}
}

func TestNilRepoOrKeyNeverThrows(t *testing.T) {
	w := newWebFx(t)
	w.writeOK(webState(repoID, "PAY-1", "h"))
	if _, ok := approvalOf(t, NewWebGateStore(w.cache, w.pub, "", keyPay), BRD); ok {
		t.Error("repo rỗng")
	}
	if _, ok, _ := NewWebGateStore(w.cache, w.pub, repoID, "").Submission(BRD); ok {
		t.Error("key rỗng")
	}
	if _, ok := ReadWebGateState(w.cache, w.pub, "", ""); ok {
		t.Error("rỗng")
	}
}
