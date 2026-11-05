package step

import (
	"encoding/json"

	"aisdlc/internal/gitx"
	"aisdlc/internal/spec"
)

// StepStatus là trạng thái một bước; ghi như Jackson ghi enum.
type StepStatus string

const (
	StepNotStarted  StepStatus = "NOT_STARTED"
	StepInProgress  StepStatus = "IN_PROGRESS"
	StepAwaitingDev StepStatus = "AWAITING_DEV"
	StepAccepted    StepStatus = "ACCEPTED"
)

// StepCheck là kết quả kiểm khi kết thúc bước.
type StepCheck struct {
	CasesPassed    int64       `json:"casesPassed"`
	CasesNotPassed []string    `json:"casesNotPassed"`
	Files          int         `json:"files"`
	Lines          int         `json:"lines"`
	Budget         spec.Budget `json:"budget"`
	OverBudget     bool        `json:"overBudget"`
	OutOfScope     []string    `json:"outOfScope"`
}

// StepRecord là một dòng trong steps.json. Chuỗi rỗng ở StartTree/EndTree/AcceptedBy tương ứng null của Java;
// Check nil là null.
type StepRecord struct {
	ID         string
	Status     StepStatus
	StartTree  string
	EndTree    string
	Changes    []gitx.FileChange
	Check      *StepCheck
	AcceptedBy string
}

type changeJSON struct {
	Path    string          `json:"path"`
	OldPath *string         `json:"oldPath"`
	Type    gitx.ChangeType `json:"type"`
	Added   int             `json:"added"`
	Deleted int             `json:"deleted"`
	Binary  bool            `json:"binary"`
}

type recordJSON struct {
	ID         string       `json:"id"`
	Status     StepStatus   `json:"status"`
	StartTree  *string      `json:"startTree"`
	EndTree    *string      `json:"endTree"`
	Changes    []changeJSON `json:"changes"`
	Check      *StepCheck   `json:"check"`
	AcceptedBy *string      `json:"acceptedBy"`
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// MarshalJSON ghi tên trường như Jackson ghi record Java.
func (r StepRecord) MarshalJSON() ([]byte, error) {
	cs := make([]changeJSON, 0, len(r.Changes))
	for _, c := range r.Changes {
		cs = append(cs, changeJSON{c.Path, strPtr(c.OldPath), c.Type, c.Added, c.Deleted, c.Binary})
	}
	var chk *StepCheck
	if r.Check != nil {
		c := *r.Check
		if c.CasesNotPassed == nil {
			c.CasesNotPassed = []string{}
		}
		if c.OutOfScope == nil {
			c.OutOfScope = []string{}
		}
		chk = &c
	}
	return json.Marshal(recordJSON{r.ID, r.Status, strPtr(r.StartTree), strPtr(r.EndTree), cs, chk, strPtr(r.AcceptedBy)})
}

// UnmarshalJSON đọc steps.json, bỏ qua khóa lạ.
func (r *StepRecord) UnmarshalJSON(b []byte) error {
	var j recordJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	*r = StepRecord{ID: j.ID, Status: j.Status, StartTree: strVal(j.StartTree), EndTree: strVal(j.EndTree),
		Check: j.Check, AcceptedBy: strVal(j.AcceptedBy)}
	if j.Changes != nil {
		r.Changes = make([]gitx.FileChange, 0, len(j.Changes))
	}
	for _, c := range j.Changes {
		r.Changes = append(r.Changes, gitx.FileChange{Path: c.Path, OldPath: strVal(c.OldPath), Type: c.Type,
			Added: c.Added, Deleted: c.Deleted, Binary: c.Binary})
	}
	return nil
}
