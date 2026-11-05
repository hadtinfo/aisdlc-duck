package gate

import (
	"os"
	"strconv"
	"time"

	"aisdlc/internal/model"
	"aisdlc/internal/spec"
)

// Service tính trạng thái và thực hiện nộp/duyệt các cổng của một tính năng.
type Service struct {
	paths model.FeaturePaths
	flow  model.Flow
	cfg   model.RepoConfig
	store GateStore
	now   func() time.Time
}

// NewService tạo Service; now thay cho Clock của Java.
func NewService(p model.FeaturePaths, flow model.Flow, cfg model.RepoConfig, store GateStore, now func() time.Time) *Service {
	return &Service{paths: p, flow: flow, cfg: cfg, store: store, now: now}
}

// call là bộ nhớ đệm trong phạm vi một lần gọi công khai.
type call struct {
	states map[Gate]GateState
	hashes map[Gate]string
}

func (s *Service) begin() (*call, error) {
	if err := s.store.Refresh(); err != nil {
		return nil, err
	}
	return &call{states: map[Gate]GateState{}, hashes: map[Gate]string{}}, nil
}

func (s *Service) hash(g Gate, c *call) (string, error) {
	if h, ok := c.hashes[g]; ok {
		return h, nil
	}
	cs, err := g.ArtifactContents(s.paths, s.flow)
	if err != nil {
		return "", errf(err.Error())
	}
	h := model.SHA256Contents(cs)
	c.hashes[g] = h
	return h, nil
}

// State là trạng thái của cổng.
func (s *Service) State(g Gate) (GateState, error) {
	c, err := s.begin()
	if err != nil {
		return "", err
	}
	return s.state(g, c)
}

func (s *Service) state(g Gate, c *call) (GateState, error) {
	if err := s.requireInFlow(g); err != nil {
		return "", err
	}
	if st, ok := c.states[g]; ok {
		return st, nil
	}
	st, err := s.compute(g, c)
	if err != nil {
		return "", err
	}
	c.states[g] = st
	return st, nil
}

func (s *Service) compute(g Gate, c *call) (GateState, error) {
	prev, err := s.previousOpen(g, c)
	if err != nil {
		return "", err
	}
	if !prev {
		return LockedByPrevious, nil
	}
	current, err := s.hash(g, c)
	if err != nil {
		return "", err
	}
	a, ok, err := s.active(g)
	if err != nil {
		return "", err
	}
	if ok {
		if a.Hash == current {
			return Approved, nil
		}
		return Stale, nil
	}
	sub, ok, err := s.store.Submission(g)
	if err != nil {
		return "", err
	}
	if ok {
		if len(sub.Findings) > 0 {
			return SubmittedWithFindings, nil
		}
		if sub.Hash != current {
			return NotSubmitted, nil
		}
		return AwaitingApproval, nil
	}
	return NotSubmitted, nil
}

// CurrentHash là hash artifact hiện tại của cổng (bỏ qua trạng thái các cổng trước). Chỉ đọc.
func (s *Service) CurrentHash(g Gate) (string, error) {
	if err := s.requireInFlow(g); err != nil {
		return "", err
	}
	c, err := s.begin()
	if err != nil {
		return "", err
	}
	return s.hash(g, c)
}

// Flow là luồng của tính năng.
func (s *Service) Flow() model.Flow { return s.flow }

// HasApproval: có bản ghi duyệt còn hiệu lực (APPROVED hoặc STALE; bản tự duyệt đã bị thay thế không tính).
func (s *Service) HasApproval(g Gate) (bool, error) {
	if err := s.store.Refresh(); err != nil {
		return false, err
	}
	_, ok, err := s.active(g)
	return ok, err
}

// Approval là bản ghi duyệt đang lưu, kể cả bản tự duyệt đã bị thay thế (để hiển thị). Chỉ đọc.
func (s *Service) Approval(g Gate) (Approval, bool, error) {
	if err := s.store.Refresh(); err != nil {
		return Approval{}, false, err
	}
	return s.store.Approval(g)
}

func (s *Service) active(g Gate) (Approval, bool, error) {
	a, ok, err := s.store.Approval(g)
	if err != nil || !ok || a.Superseded {
		return Approval{}, false, err
	}
	return a, true, nil
}

// ApprovalMatches: hash artifact hiện tại của cổng trùng hash lúc duyệt. Chỉ đọc.
func (s *Service) ApprovalMatches(g Gate) (bool, error) {
	if err := s.requireInFlow(g); err != nil {
		return false, err
	}
	c, err := s.begin()
	if err != nil {
		return false, err
	}
	a, ok, err := s.active(g)
	if err != nil || !ok {
		return false, err
	}
	h, err := s.hash(g, c)
	if err != nil {
		return false, err
	}
	return a.Hash == h, nil
}

// IsOpen: cổng đã APPROVED.
func (s *Service) IsOpen(g Gate) (bool, error) {
	c, err := s.begin()
	if err != nil {
		return false, err
	}
	return s.open(g, c)
}

// CodeReady: các cổng cần để viết code đã mở.
func (s *Service) CodeReady() (bool, error) {
	if s == nil { // typed-nil qua interface: fail-closed
		return false, nil
	}
	c, err := s.begin()
	if err != nil {
		return false, err
	}
	need := []Gate{CASES}
	if s.flow == model.FlowFull {
		need = []Gate{DESIGN, CASES}
	}
	for _, g := range need {
		ok, err := s.open(g, c)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func (s *Service) open(g Gate, c *call) (bool, error) {
	st, err := s.state(g, c)
	return st == Approved, err
}

// Submit nộp cổng kèm các phát hiện.
func (s *Service) Submit(g Gate, findings []spec.Finding) (Submission, error) {
	if err := s.requireInFlow(g); err != nil {
		return Submission{}, err
	}
	c, err := s.begin()
	if err != nil {
		return Submission{}, err
	}
	if err := s.requireUnlocked(g, c); err != nil {
		return Submission{}, err
	}
	h, err := s.hash(g, c)
	if err != nil {
		return Submission{}, err
	}
	sub := Submission{Gate: g, Hash: h, At: s.now().UTC(), Findings: append([]spec.Finding{}, findings...)}
	if err := s.store.SaveSubmission(sub); err != nil {
		return Submission{}, err
	}
	return sub, nil
}

// Approve duyệt tay; channel rỗng là terminal / không rõ.
func (s *Service) Approve(g Gate, approver, channel string) (Approval, error) {
	return s.record(g, approver, "", channel)
}

// AutoApprove tự duyệt theo flow (rule = "flow:<level>"): cùng điều kiện như duyệt tay, người duyệt "auto".
// Cổng REVIEW luôn duyệt tay.
func (s *Service) AutoApprove(g Gate, rule string) (Approval, error) {
	if g == REVIEW {
		return Approval{}, errf("Cổng REVIEW luôn duyệt tay.")
	}
	return s.record(g, ApprovalAuto, rule, "")
}

// SupersedeAuto đánh dấu superseded các bản tự duyệt còn hiệu lực của candidates (khi nâng mức). Bản duyệt tay
// không đổi. Trả các cổng đã thay thế, theo thứ tự luồng.
func (s *Service) SupersedeAuto(candidates ...Gate) ([]Gate, error) {
	if err := s.store.Refresh(); err != nil {
		return nil, err
	}
	var out []Gate
	for _, g := range Sequence(s.flow) {
		in := false
		for _, c := range candidates {
			if c == g {
				in = true
			}
		}
		if !in {
			continue
		}
		a, ok, err := s.active(g)
		if err != nil {
			return nil, err
		}
		if !ok || !a.IsAuto() {
			continue
		}
		if err := s.store.SaveApproval(a.AsSuperseded()); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

func (s *Service) requireUnlocked(g Gate, c *call) error {
	prev, err := s.previousOpen(g, c)
	if err != nil {
		return err
	}
	if !prev {
		return errf("Cổng " + string(g) + " bị khóa: cổng trước chưa được duyệt.")
	}
	return nil
}

func (s *Service) record(g Gate, approver, rule, channel string) (Approval, error) {
	if err := s.requireInFlow(g); err != nil {
		return Approval{}, err
	}
	c, err := s.begin()
	if err != nil {
		return Approval{}, err
	}
	if err := s.requireUnlocked(g, c); err != nil {
		return Approval{}, err
	}
	sub, ok, err := s.store.Submission(g)
	if err != nil {
		return Approval{}, err
	}
	if !ok {
		return Approval{}, errf("Cổng " + string(g) + " chưa được nộp.")
	}
	if len(sub.Findings) > 0 {
		return Approval{}, errf("Cổng " + string(g) + " còn " + strconv.Itoa(len(sub.Findings)) + " phát hiện chưa xử lý.")
	}
	current, err := s.hash(g, c)
	if err != nil {
		return Approval{}, err
	}
	if current != sub.Hash {
		return Approval{}, errf("Artifact của cổng " + string(g) + " đã đổi kể từ lúc nộp; hãy nộp lại.")
	}
	a := Approval{Gate: g, Hash: current, Approver: approver, At: s.now().UTC(), Rule: rule, Channel: channel}
	if err := s.store.SaveApproval(a); err != nil {
		return Approval{}, err
	}
	return a, nil
}

func (s *Service) instant() string { return s.now().UTC().Format(time.RFC3339Nano) }

// Reject trả cổng lại kèm lý do, ghi feedback.md và xóa nộp/duyệt.
func (s *Service) Reject(g Gate, approver, comment string) error {
	if err := s.requireInFlow(g); err != nil {
		return err
	}
	if err := s.appendFeedback("## " + string(g) + " — trả lại bởi " + approver + " lúc " + s.instant() + "\n\n" + comment + "\n\n"); err != nil {
		return err
	}
	return s.store.Clear(g)
}

// Reopen mở lại cổng, xóa nộp/duyệt và ghi feedback.md.
func (s *Service) Reopen(g Gate, approver string) error {
	if err := s.requireInFlow(g); err != nil {
		return err
	}
	if err := s.store.Clear(g); err != nil {
		return err
	}
	return s.appendFeedback("## " + string(g) + " — mở lại bởi " + approver + " lúc " + s.instant() + "\n\n")
}

// FirstNotOpen là cổng đầu tiên chưa mở; false khi mọi cổng đã mở.
func (s *Service) FirstNotOpen() (Gate, bool, error) {
	c, err := s.begin()
	if err != nil {
		return "", false, err
	}
	for _, g := range Sequence(s.flow) {
		ok, err := s.open(g, c)
		if err != nil {
			return "", false, err
		}
		if !ok {
			return g, true, nil
		}
	}
	return "", false, nil
}

func (s *Service) previousOpen(g Gate, c *call) (bool, error) {
	for _, x := range Sequence(s.flow) {
		if x == g {
			return true, nil
		}
		ok, err := s.open(x, c)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func (s *Service) requireInFlow(g Gate) error {
	for _, x := range Sequence(s.flow) {
		if x == g {
			return nil
		}
	}
	return errf("Cổng " + string(g) + " không thuộc luồng " + string(s.flow) + ".")
}

func (s *Service) appendFeedback(text string) error {
	if err := os.MkdirAll(s.paths.Dir(), 0o755); err != nil {
		return errf("Không ghi được feedback.md: " + err.Error())
	}
	f, err := os.OpenFile(s.paths.Feedback(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return errf("Không ghi được feedback.md: " + err.Error())
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		return errf("Không ghi được feedback.md: " + err.Error())
	}
	return nil
}
