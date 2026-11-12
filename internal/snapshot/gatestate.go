package snapshot

import (
	"encoding/json"
	"fmt"
	"time"

	"aisdlc/internal/i18n"
	"aisdlc/internal/spec"
)

// FeatureGateState là trạng thái cổng của một tính năng do server web ký (nằm trong SignedEnvelope). Superseded:
// bản tự duyệt bị thay thế khi nâng mức, của cổng chưa có bản duyệt hiệu lực (chỉ để hiển thị, không tính là đã
// duyệt); tùy chọn: phong bì cũ không có khóa này, và khóa bị bỏ khi rỗng.
type FeatureGateState struct {
	Repo        string    `json:"repo"`
	Feature     string    `json:"feature"`
	Mode        string    `json:"mode"`
	IssuedAt    time.Time `json:"issuedAt"`
	Submissions []Sub     `json:"submissions"`
	Approvals   []Appr    `json:"approvals"`
	Feedback    []Fb      `json:"feedback"`
	Superseded  []Appr    `json:"superseded,omitempty"`
}

// Sub là một lần nộp cổng.
type Sub struct {
	Gate     string         `json:"gate"`
	Hash     string         `json:"hash"`
	At       time.Time      `json:"at"`
	Findings []spec.Finding `json:"findings"`
}

// Appr là một bản duyệt. Rule: lý do tự duyệt ("flow:<level>") khi Channel = "auto", rỗng với bản duyệt tay.
type Appr struct {
	Gate          string    `json:"gate"`
	Hash          string    `json:"hash"`
	Approver      string    `json:"approver"`
	At            time.Time `json:"at"`
	EvaluatedTree string    `json:"evaluatedTree"`
	Channel       string    `json:"channel"`
	Rule          string    `json:"rule,omitempty"`
}

// Fb có Kind: REJECTED | REOPENED.
type Fb struct {
	ID      int64     `json:"id"`
	Gate    string    `json:"gate"`
	Kind    string    `json:"kind"`
	By      string    `json:"by"`
	At      time.Time `json:"at"`
	Comment string    `json:"comment"`
}

// ToJSON serialize; danh sách nil thành []. Lỗi theo locale mặc định của tiến trình (CLI); web dùng ToJSONIn.
func (g FeatureGateState) ToJSON() ([]byte, error) { return g.ToJSONIn(i18n.Default()) }

// ToJSONIn như ToJSON, lỗi theo locale loc (web không dùng locale toàn cục).
func (g FeatureGateState) ToJSONIn(loc i18n.Locale) ([]byte, error) {
	if g.Submissions == nil {
		g.Submissions = []Sub{}
	}
	if g.Approvals == nil {
		g.Approvals = []Appr{}
	}
	if g.Feedback == nil {
		g.Feedback = []Fb{}
	}
	subs := make([]Sub, len(g.Submissions))
	copy(subs, g.Submissions)
	for i := range subs {
		if subs[i].Findings == nil {
			subs[i].Findings = []spec.Finding{}
		}
	}
	g.Submissions = subs
	b, err := json.Marshal(g)
	if err != nil {
		return nil, fmt.Errorf(loc.T("Không serialize được trạng thái cổng: %v"), err)
	}
	return b, nil
}

// FeatureGateStateFromJSON parse JSON; lỗi khi hỏng.
func FeatureGateStateFromJSON(b []byte) (FeatureGateState, error) {
	var g *FeatureGateState
	if err := json.Unmarshal(b, &g); err != nil {
		return FeatureGateState{}, fmt.Errorf(i18n.T("Trạng thái cổng hỏng: %v"), err)
	}
	if g == nil {
		return FeatureGateState{}, fmt.Errorf("%s", i18n.T("Trạng thái cổng hỏng: rỗng"))
	}
	return *g, nil
}
