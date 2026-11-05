package step

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
)

// CodeReadyChecker là phần của gate.Service mà StepService dùng.
type CodeReadyChecker interface {
	CodeReady() (bool, error)
}

// Service quản lý các bước Sx của một tính năng (steps.json).
type Service struct {
	paths  model.FeaturePaths
	git    *gitx.Repo
	specs  []spec.StepSpec
	cfg    model.RepoConfig
	extras *ScopeExtras
	gates  CodeReadyChecker
	now    func() time.Time
}

// NewService tạo Service. Gọi WithGates trước Start; chưa gắn cổng thì Start từ chối (an toàn).
func NewService(p model.FeaturePaths, git *gitx.Repo, specs []spec.StepSpec, cfg model.RepoConfig, now func() time.Time) *Service {
	return &Service{paths: p, git: git, specs: append([]spec.StepSpec(nil), specs...), cfg: cfg,
		extras: NewScopeExtras(p), now: now}
}

// WithGates gắn dịch vụ cổng dùng để kiểm "đã được code".
func (s *Service) WithGates(g CodeReadyChecker) *Service {
	s.gates = g
	return s
}

func (s *Service) Start(id string) (StepRecord, error) {
	ready := false
	if s.gates != nil {
		var err error
		if ready, err = s.gates.CodeReady(); err != nil {
			return StepRecord{}, err
		}
	}
	if !ready {
		return StepRecord{}, errf("Chưa được code: các cổng trước code chưa được duyệt.")
	}
	idx, err := s.indexOf(id)
	if err != nil {
		return StepRecord{}, err
	}
	recs, err := s.load()
	if err != nil {
		return StepRecord{}, err
	}
	existing := find(recs, id)
	if existing != nil && existing.Status == StepAwaitingDev {
		redo := StepRecord{ID: id, Status: StepInProgress, StartTree: existing.StartTree}
		replace(recs, redo)
		return redo, s.save(recs)
	}
	for _, r := range recs {
		if r.Status == StepInProgress || r.Status == StepAwaitingDev {
			return StepRecord{}, errf("Bước " + r.ID + " đang dở (" + string(r.Status) + "); xong bước đó trước.")
		}
	}
	if existing != nil {
		return StepRecord{}, errf("Bước " + id + " đã được làm.")
	}
	if idx > 0 {
		prev := s.specs[idx-1].ID
		if r := find(recs, prev); r == nil || r.Status != StepAccepted {
			return StepRecord{}, errf("Bước " + prev + " chưa được dev chấp nhận; chưa thể bắt đầu " + id + ".")
		}
	}
	tree, err := s.snapshot()
	if err != nil {
		return StepRecord{}, err
	}
	rec := StepRecord{ID: id, Status: StepInProgress, StartTree: tree}
	recs = append(recs, rec)
	return rec, s.save(recs)
}

func (s *Service) Finish(id string, res run.CaseRunResult) (StepRecord, error) {
	if _, err := s.indexOf(id); err != nil {
		return StepRecord{}, err
	}
	recs, err := s.load()
	if err != nil {
		return StepRecord{}, err
	}
	cur, err := require(recs, id, StepInProgress)
	if err != nil {
		return StepRecord{}, err
	}
	ctx := context.Background()
	end, err := s.snapshot()
	if err != nil {
		return StepRecord{}, err
	}
	diff, err := s.git.Diff(ctx, cur.StartTree, end)
	if err != nil {
		return StepRecord{}, err
	}
	changes := []gitx.FileChange{}
	files, lines := 0, 0
	for _, c := range diff {
		if !Ignored(s.paths, s.cfg, c) {
			changes = append(changes, c)
			files++
			lines += c.Lines()
		}
	}
	sp, _ := s.spec(id)
	var budget spec.Budget
	if sp.Budget != nil {
		budget = *sp.Budget
	}
	f := s.cfg.BudgetWarnFactor
	over := float64(files) > float64(budget.Files)*f || float64(lines) > float64(budget.Lines)*f
	allowed, err := s.AllowedGlobs(id)
	if err != nil {
		return StepRecord{}, err
	}
	inScope := CompileScope(allowed)
	out := []string{}
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, c := range changes {
		if !inScope(c.Path) {
			add(c.Path)
		}
		if c.OldPath != "" && !inScope(c.OldPath) {
			add(c.OldPath)
		}
	}
	check := &StepCheck{CasesPassed: res.Passed(), CasesNotPassed: res.NotPassedIDs(), Files: files, Lines: lines,
		Budget: budget, OverBudget: over, OutOfScope: out}
	done := StepRecord{ID: id, Status: StepAwaitingDev, StartTree: cur.StartTree, EndTree: end, Changes: changes, Check: check}
	replace(recs, done)
	return done, s.save(recs)
}

func (s *Service) Accept(id, approver string) (StepRecord, error) {
	if _, err := s.indexOf(id); err != nil {
		return StepRecord{}, err
	}
	recs, err := s.load()
	if err != nil {
		return StepRecord{}, err
	}
	cur, err := require(recs, id, StepAwaitingDev)
	if err != nil {
		return StepRecord{}, err
	}
	acc := cur
	acc.Status = StepAccepted
	acc.AcceptedBy = approver
	replace(recs, acc)
	return acc, s.save(recs)
}

// Current trả bước đang IN_PROGRESS hoặc AWAITING_DEV (bước đầu tiên nếu có nhiều).
func (s *Service) Current() (StepRecord, bool, error) {
	recs, err := s.load()
	if err != nil {
		return StepRecord{}, false, err
	}
	for _, r := range recs {
		if r.Status == StepInProgress || r.Status == StepAwaitingDev {
			return r, true, nil
		}
	}
	return StepRecord{}, false, nil
}

func (s *Service) All() ([]StepRecord, error) { return s.load() }

// AllowedGlobs: scope của bước, alwaysAllowed và scope-extra.json (bỏ trùng, giữ thứ tự).
func (s *Service) AllowedGlobs(stepID string) ([]string, error) {
	sp, err := s.spec(stepID)
	if err != nil {
		return nil, err
	}
	extra, err := s.extras.Paths()
	if err != nil {
		return nil, err
	}
	return dedup(sp.Scope, s.cfg.AlwaysAllowed, extra), nil
}

// UsesScopeExtras: bước có file chỉ nằm trong phạm vi nhờ dev cho phép thêm (scope allow, kể cả ALLOW_SCOPE từ web).
func (s *Service) UsesScopeExtras(id string) (bool, error) {
	recs, err := s.load()
	if err != nil {
		return false, err
	}
	r := find(recs, id)
	if r == nil || r.Changes == nil {
		return false, nil
	}
	extras, err := s.extras.Paths()
	if err != nil {
		return false, err
	}
	if len(extras) == 0 {
		return false, nil
	}
	sp, err := s.spec(id)
	if err != nil {
		return false, err
	}
	inBase := CompileScope(dedup(sp.Scope, s.cfg.AlwaysAllowed))
	inExtras := CompileScope(extras)
	for _, c := range r.Changes {
		ps := []string{c.Path}
		if c.OldPath != "" {
			ps = append(ps, c.OldPath)
		}
		for _, p := range ps {
			if !inBase(p) && inExtras(p) {
				return true, nil
			}
		}
	}
	return false, nil
}

// Ignored: thay đổi không tính vào bước, khi mọi đường dẫn của nó đều bị IgnoredPath bỏ qua.
func Ignored(p model.FeaturePaths, cfg model.RepoConfig, c gitx.FileChange) bool {
	return IgnoredPath(p, cfg, c.Path) && (c.OldPath == "" || IgnoredPath(p, cfg, c.OldPath))
}

// IgnoredPath: file trạng thái của tính năng (aisdlc/features/<KEY>/) hoặc khớp unmanaged.
func IgnoredPath(p model.FeaturePaths, cfg model.RepoConfig, path string) bool {
	return strings.HasPrefix(path, p.Rel(p.Dir())+"/") || ScopeMatches(cfg.Unmanaged, path)
}

func dedup(lists ...[]string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, l := range lists {
		for _, g := range l {
			if !seen[g] {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	return out
}

func (s *Service) indexOf(id string) (int, error) {
	for i, sp := range s.specs {
		if sp.ID == id {
			return i, nil
		}
	}
	return 0, errf("Không có bước " + id + " trong thiết kế.")
}

func (s *Service) spec(id string) (spec.StepSpec, error) {
	i, err := s.indexOf(id)
	if err != nil {
		return spec.StepSpec{}, err
	}
	return s.specs[i], nil
}

func find(recs []StepRecord, id string) *StepRecord {
	for i := range recs {
		if recs[i].ID == id {
			return &recs[i]
		}
	}
	return nil
}

func require(recs []StepRecord, id string, status StepStatus) (StepRecord, error) {
	r := find(recs, id)
	if r == nil {
		return StepRecord{}, errf("Bước " + id + " chưa bắt đầu.")
	}
	if r.Status != status {
		return StepRecord{}, errf("Bước " + id + " đang ở trạng thái " + string(r.Status) + ", cần " + string(status) + ".")
	}
	return *r, nil
}

func replace(recs []StepRecord, rec StepRecord) {
	for i := range recs {
		if recs[i].ID == rec.ID {
			recs[i] = rec
		}
	}
}

func (s *Service) load() ([]StepRecord, error) {
	file := s.paths.Steps()
	b, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return []StepRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	var recs []StepRecord
	if err := json.Unmarshal(b, &recs); err != nil {
		return nil, fmt.Errorf("steps.json hỏng: %s: %w", file, err)
	}
	if recs == nil {
		recs = []StepRecord{}
	}
	return recs, nil
}

func (s *Service) save(recs []StepRecord) error { return writeJSON(s.paths.Steps(), recs) }

// snapshot chụp cây working tree; gitx đã bỏ file tạm atomic và tự thử lại khi git quét trúng chúng.
func (s *Service) snapshot() (string, error) {
	return s.git.SnapshotTree(context.Background())
}
