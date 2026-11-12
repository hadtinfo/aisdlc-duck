package gate

import (
	"bytes"
	"encoding/json"
	"os"

	"aisdlc/internal/fsx"
	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
	"aisdlc/internal/spec"
)

func emptyFindings() []spec.Finding { return []spec.Finding{} }

// gatesData là dạng lưu trên đĩa của gates.json.
type gatesData struct {
	Submissions map[Gate]Submission `json:"submissions"`
	Approvals   map[Gate]Approval   `json:"approvals"`
}

// LocalGateStore lưu trong gates.json của tính năng.
type LocalGateStore struct {
	file  string
	cache *gatesData
}

// NewLocalGateStore tạo store cho tính năng p.
func NewLocalGateStore(p model.FeaturePaths) *LocalGateStore {
	return &LocalGateStore{file: p.Gates()}
}

func (s *LocalGateStore) Refresh() error { s.cache = nil; return nil }

func (s *LocalGateStore) cached() (*gatesData, error) {
	if s.cache == nil {
		d, err := s.load()
		if err != nil {
			return nil, err
		}
		s.cache = d
	}
	return s.cache, nil
}

func (s *LocalGateStore) Submission(g Gate) (Submission, bool, error) {
	d, err := s.cached()
	if err != nil {
		return Submission{}, false, err
	}
	v, ok := d.Submissions[g]
	return v, ok, nil
}

func (s *LocalGateStore) Approval(g Gate) (Approval, bool, error) {
	d, err := s.cached()
	if err != nil {
		return Approval{}, false, err
	}
	v, ok := d.Approvals[g]
	return v, ok, nil
}

func (s *LocalGateStore) SaveSubmission(sub Submission) error {
	d, err := s.load()
	if err != nil {
		return err
	}
	d.Submissions[sub.Gate] = sub
	return s.commit(d)
}

func (s *LocalGateStore) SaveApproval(a Approval) error {
	d, err := s.load()
	if err != nil {
		return err
	}
	d.Approvals[a.Gate] = a
	return s.commit(d)
}

func (s *LocalGateStore) Clear(g Gate) error {
	d, err := s.load()
	if err != nil {
		return err
	}
	delete(d.Submissions, g)
	delete(d.Approvals, g)
	return s.commit(d)
}

func (s *LocalGateStore) commit(d *gatesData) error {
	if err := s.save(d); err != nil {
		return err
	}
	s.cache = d
	return nil
}

func (s *LocalGateStore) load() (*gatesData, error) {
	d := &gatesData{}
	b, err := os.ReadFile(s.file)
	if err != nil {
		if os.IsNotExist(err) {
			d.Submissions, d.Approvals = map[Gate]Submission{}, map[Gate]Approval{}
			return d, nil
		}
		return nil, errf(i18n.Tf("gates.json hỏng: %s", s.file))
	}
	// khóa lạ bị bỏ qua, như Jackson với FAIL_ON_UNKNOWN_PROPERTIES tắt
	if err := json.Unmarshal(b, d); err != nil {
		return nil, errf(i18n.Tf("gates.json hỏng: %s", s.file))
	}
	if d.Submissions == nil {
		d.Submissions = map[Gate]Submission{}
	}
	if d.Approvals == nil {
		d.Approvals = map[Gate]Approval{}
	}
	return d, nil
}

func (s *LocalGateStore) save(d *gatesData) error {
	// findings luôn là mảng, không phải null
	subs := make(map[Gate]Submission, len(d.Submissions))
	for g, v := range d.Submissions {
		if v.Findings == nil {
			v.Findings = emptyFindings()
		}
		subs[g] = v
	}
	b, err := json.Marshal(gatesData{Submissions: subs, Approvals: d.Approvals})
	if err != nil {
		return errf(i18n.Tf("Không ghi được %s: %s", s.file, err.Error()))
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return errf(i18n.Tf("Không ghi được %s: %s", s.file, err.Error()))
	}
	if err := fsx.WriteAtomic(s.file, buf.Bytes(), 0o644); err != nil {
		return errf(err.Error())
	}
	return nil
}
