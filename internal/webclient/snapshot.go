package webclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"aisdlc/internal/gate"
	"aisdlc/internal/gitx"
	"aisdlc/internal/lang"
	"aisdlc/internal/model"
	"aisdlc/internal/review"
	"aisdlc/internal/review/cardbuild"
	"aisdlc/internal/spec"
	"aisdlc/internal/step"
	"aisdlc/internal/yamlx"
)

// Giới hạn kích thước snapshot (FeatureSnapshot.java).
const (
	MaxSnapshotBytes = 8 * 1024 * 1024
	MaxArtifactBytes = 256 * 1024
	MaxDiffBytes     = 512 * 1024
	expectBodyChars  = 120
)

// SnapshotInput là đầu vào dựng snapshot (SnapshotBuilder.SnapshotInput). Cases nil = chưa có cases.yaml.
type SnapshotInput struct {
	Paths      model.FeaturePaths
	Config     model.RepoConfig
	Meta       model.FeatureMeta
	Flow       model.Flow
	Gates      *gate.Service
	Store      gate.GateStore
	Specs      []spec.StepSpec
	Records    []step.StepRecord
	Cases      *spec.CasesSpec
	Git        *gitx.Repo
	Adapter    lang.Adapter
	RepoID     string
	Branch     string
	ClientID   string
	CapturedAt time.Time
	Sender     string
	WebMode    bool
}

// FeatureSnapshot là trạng thái một tính năng, hợp đồng v1 giữa CLI và web (FeatureSnapshot.java).
type FeatureSnapshot struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Repo          SnapRepo                    `json:"repo"`
	ClientID      string                      `json:"clientId"`
	CapturedAt    Instant                     `json:"capturedAt"`
	Sender        string                      `json:"sender"`
	Feature       SnapFeature                 `json:"feature"`
	Gates         []GateView                  `json:"gates"`
	Steps         []StepView                  `json:"steps"`
	Cases         []CaseView                  `json:"cases"`
	OpenQuestions []OpenQuestion              `json:"openQuestions"`
	Artifacts     *Ordered[Artifact]          `json:"artifacts"`
	Eval          json.RawMessage             `json:"eval"`
	Review        json.RawMessage             `json:"review"`
	ReviewCards   *Ordered[review.ReviewCard] `json:"reviewCards,omitempty"`
	Truncated     bool                        `json:"truncated"`
	Tree          string                      `json:"tree,omitempty"`
}

// SnapRepo là repo và nhánh.
type SnapRepo struct {
	ID     string `json:"id"`
	Branch string `json:"branch"`
}

// SnapFeature là thông tin tính năng; Owner rỗng ghi null như Java.
type SnapFeature struct {
	Key, Title, Flow, Owner string
	CreatedAt               Instant
}

// MarshalJSON giữ thứ tự và null như record Java.
func (f SnapFeature) MarshalJSON() ([]byte, error) {
	return marshalNoEscape(struct {
		Key       string  `json:"key"`
		Title     string  `json:"title"`
		Flow      string  `json:"flow"`
		Owner     *string `json:"owner"`
		CreatedAt Instant `json:"createdAt"`
	}{f.Key, f.Title, f.Flow, nullable(f.Owner), f.CreatedAt})
}

// GateView là trạng thái một cổng.
type GateView struct {
	Gate         gate.Gate       `json:"gate"`
	State        gate.GateState  `json:"state"`
	CurrentHash  string          `json:"currentHash"`
	Submission   *SubmissionView `json:"submission"`
	Approval     *ApprovalView   `json:"approval"`
	LastFeedback *Feedback       `json:"lastFeedback"`
}

// SubmissionView là lần nộp.
type SubmissionView struct {
	Hash     string         `json:"hash"`
	At       Instant        `json:"at"`
	Findings []spec.Finding `json:"findings"`
}

// ApprovalView là bản duyệt; Rule ("flow:<mức>") chỉ có ở bản tự duyệt.
type ApprovalView struct {
	Hash     string  `json:"hash"`
	Approver string  `json:"approver"`
	At       Instant `json:"at"`
	Rule     string  `json:"rule,omitempty"`
}

// Diff là diff của bước (đã cắt theo MaxDiffBytes).
type Diff struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

// StepView là một bước.
type StepView struct {
	ID      string           `json:"id"`
	Title   string           `json:"title"`
	Scope   []string         `json:"scope"`
	Budget  *spec.Budget     `json:"budget"`
	CaseIDs []string         `json:"caseIds"`
	Status  step.StepStatus  `json:"status"`
	Record  *step.StepRecord `json:"record"`
	Diff    *Diff            `json:"diff"`
}

// CaseView là một case.
type CaseView struct {
	ID      string        `json:"id"`
	Step    string        `json:"step"`
	Kind    spec.CaseKind `json:"kind"`
	Title   string        `json:"title"`
	Request string        `json:"request"`
	Expect  string        `json:"expect"`
	Raw     string        `json:"raw"`
}

// OpenQuestion là một câu hỏi mở của BRD hoặc design.
type OpenQuestion struct {
	Gate gate.Gate `json:"gate"`
	Text string    `json:"text"`
}

// Artifact là nội dung một file của tính năng (đã cắt theo MaxArtifactBytes).
type Artifact struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

// Ordered là map giữ thứ tự chèn khi ghi JSON (LinkedHashMap của Java).
type Ordered[V any] struct {
	keys []string
	vals map[string]V
}

func newOrdered[V any]() *Ordered[V] { return &Ordered[V]{vals: map[string]V{}} }

// Put thêm hoặc thay giá trị.
func (o *Ordered[V]) Put(k string, v V) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// Get trả giá trị (zero nếu thiếu).
func (o *Ordered[V]) Get(k string) V { return o.vals[k] }

// Has: có khóa k.
func (o *Ordered[V]) Has(k string) bool { _, ok := o.vals[k]; return ok }

// Keys theo thứ tự chèn.
func (o *Ordered[V]) Keys() []string { return append([]string(nil), o.keys...) }

// Len là số khóa.
func (o *Ordered[V]) Len() int { return len(o.keys) }

// MarshalJSON ghi object theo thứ tự chèn.
func (o *Ordered[V]) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := marshalNoEscape(k)
		if err != nil {
			return nil, err
		}
		vb, err := marshalNoEscape(o.vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// JSON là snapshot dạng JSON như Jackson ghi (không escape HTML).
func (s *FeatureSnapshot) JSON() ([]byte, error) {
	b, err := marshalNoEscape(s)
	if err != nil {
		return nil, fmt.Errorf("Không serialize được snapshot: %v", err)
	}
	return b, nil
}

// BuildSnapshot dựng snapshot của tính năng và trả JSON (đã vừa MaxSnapshotBytes).
func BuildSnapshot(in SnapshotInput) ([]byte, error) {
	s, err := Build(in)
	if err != nil {
		return nil, err
	}
	return s.JSON()
}

// Build dựng FeatureSnapshot từ các file trạng thái của tính năng (SnapshotBuilder.build), rồi cắt cho vừa
// MaxSnapshotBytes.
func Build(in SnapshotInput) (*FeatureSnapshot, error) {
	ctx := context.Background()
	p := in.Paths
	truncated := false
	artifacts, err := buildArtifacts(p, &truncated)
	if err != nil {
		return nil, err
	}
	steps, err := buildSteps(ctx, in, &truncated)
	if err != nil {
		return nil, err
	}
	gates, err := buildGates(in)
	if err != nil {
		return nil, err
	}
	oq, err := openQuestions(p)
	if err != nil {
		return nil, err
	}
	eval, err := rawJSON(filepath.Join(p.EvalDir(), "report.json"))
	if err != nil {
		return nil, err
	}
	rev, err := rawJSON(p.ReviewAgent())
	if err != nil {
		return nil, err
	}
	tree, err := snapshotTree(ctx, in)
	if err != nil {
		return nil, err
	}
	snap := &FeatureSnapshot{
		SchemaVersion: 1,
		Repo:          SnapRepo{ID: in.RepoID, Branch: in.Branch},
		ClientID:      in.ClientID,
		CapturedAt:    Instant(in.CapturedAt),
		Sender:        in.Sender,
		Feature: SnapFeature{Key: string(in.Meta.Key), Title: in.Meta.Title, Flow: string(in.Flow),
			Owner: in.Meta.Owner, CreatedAt: Instant(in.Meta.CreatedAt)},
		Gates:         gates,
		Steps:         steps,
		Cases:         buildCases(in.Cases),
		OpenQuestions: oq,
		Artifacts:     artifacts,
		Eval:          eval,
		Review:        rev,
		ReviewCards:   reviewCards(in),
		Truncated:     truncated,
		Tree:          tree,
	}
	return fitToLimit(snap)
}

// snapshotTree: cây code hiện tại trừ thư mục tính năng; rỗng khi không ở chế độ duyệt trên web (server chỉ cần
// tree để kiểm luật duyệt REVIEW) hoặc REVIEW còn khóa.
func snapshotTree(ctx context.Context, in SnapshotInput) (string, error) {
	if !in.WebMode {
		return "", nil
	}
	st, err := in.Gates.State(gate.REVIEW)
	if err != nil {
		return "", err
	}
	if st == gate.LockedByPrevious {
		return "", nil
	}
	return in.Git.SnapshotTreeExcluding(ctx, in.Paths.Rel(in.Paths.Dir()))
}

// reviewCards: thẻ của từng cổng đã tới lượt (khác LOCKED_BY_PREVIOUS); cổng nào lỗi thì bỏ; không có thì nil.
func reviewCards(in SnapshotInput) *Ordered[review.ReviewCard] {
	out := newOrdered[review.ReviewCard]()
	for _, g := range gate.Sequence(in.Flow) {
		var card review.ReviewCard
		err := safely(func() error {
			st, err := in.Gates.State(g)
			if err != nil {
				return err
			}
			if st == gate.LockedByPrevious {
				return errSkip
			}
			card, err = cardbuild.BuildCard(cardbuild.ReviewInput{Paths: in.Paths, Meta: in.Meta, Flow: in.Flow,
				Config: in.Config, Git: in.Git, Adapter: in.Adapter}, g)
			return err
		})
		if err == nil {
			out.Put(string(g), card)
		}
	}
	if out.Len() == 0 {
		return nil
	}
	return out
}

var errSkip = fmt.Errorf("bỏ qua")

// fitToLimit: vượt MaxSnapshotBytes thì bỏ diff từ bước cũ nhất cho tới khi vừa, cuối cùng bỏ review card.
func fitToLimit(snap *FeatureSnapshot) (*FeatureSnapshot, error) {
	size := func(s *FeatureSnapshot) (int, error) {
		b, err := s.JSON()
		return len(b), err
	}
	cur := snap
	n, err := size(cur)
	if err != nil {
		return nil, err
	}
	steps := append([]StepView(nil), snap.Steps...)
	for i := 0; i < len(steps) && n > MaxSnapshotBytes; i++ {
		if steps[i].Diff == nil || steps[i].Diff.Text == "" {
			continue
		}
		steps[i].Diff = &Diff{Text: "", Truncated: true}
		c := *snap
		c.Steps = append([]StepView(nil), steps...)
		c.Truncated = true
		cur = &c
		if n, err = size(cur); err != nil {
			return nil, err
		}
	}
	// bước cuối: vẫn quá lớn thì bỏ review card
	if n > MaxSnapshotBytes && cur.ReviewCards != nil {
		c := *cur
		c.ReviewCards = nil
		c.Truncated = true
		cur = &c
	}
	return cur, nil
}

func buildGates(in SnapshotInput) ([]GateView, error) {
	out := []GateView{}
	for _, g := range gate.Sequence(in.Flow) {
		st, err := in.Gates.State(g)
		if err != nil {
			return nil, err
		}
		hash, err := in.Gates.CurrentHash(g)
		if err != nil {
			return nil, err
		}
		v := GateView{Gate: g, State: st, CurrentHash: hash}
		sub, ok, err := in.Store.Submission(g)
		if err != nil {
			return nil, err
		}
		if ok {
			f := sub.Findings
			if f == nil {
				f = []spec.Finding{}
			}
			v.Submission = &SubmissionView{Hash: sub.Hash, At: Instant(sub.At), Findings: f}
		}
		a, ok, err := in.Store.Approval(g)
		if err != nil {
			return nil, err
		}
		if ok && !a.Superseded {
			v.Approval = &ApprovalView{Hash: a.Hash, Approver: a.Approver, At: Instant(a.At), Rule: a.Rule}
		}
		fb, ok, err := LastFeedback(in.Paths.Feedback(), g)
		if err != nil {
			return nil, err
		}
		if ok {
			v.LastFeedback = &fb
		}
		out = append(out, v)
	}
	return out, nil
}

func buildSteps(ctx context.Context, in SnapshotInput, truncated *bool) ([]StepView, error) {
	byID := map[string]step.StepRecord{}
	for _, r := range in.Records {
		byID[r.ID] = r
	}
	liveTree := ""
	out := []StepView{}
	for _, s := range in.Specs {
		r, has := byID[s.ID]
		var diff *Diff
		if has && r.StartTree != "" {
			end := r.EndTree
			if end == "" {
				if liveTree == "" {
					t, err := in.Git.SnapshotTree(ctx)
					if err != nil {
						return nil, err
					}
					liveTree = t
				}
				end = liveTree
			}
			diff = stepDiff(ctx, in, r.StartTree, end)
			if diff != nil && diff.Truncated {
				*truncated = true
			}
		}
		caseIDs := []string{}
		if in.Cases != nil {
			for _, c := range in.Cases.Cases {
				if c.Step == s.ID {
					caseIDs = append(caseIDs, c.ID)
				}
			}
		}
		v := StepView{ID: s.ID, Title: s.Title, Scope: s.Scope, Budget: s.Budget, CaseIDs: caseIDs,
			Status: step.StepNotStarted, Diff: diff}
		if has {
			rec := r
			v.Status, v.Record = r.Status, &rec
		}
		out = append(out, v)
	}
	return out, nil
}

// stepDiff là diff của bước, bỏ file trạng thái AISDLC và file unmanaged như step.Ignored; nil khi tree không còn
// trong object store (đã bị gc) hoặc git lỗi.
func stepDiff(ctx context.Context, in SnapshotInput, from, to string) *Diff {
	changes, err := in.Git.Diff(ctx, from, to)
	if err != nil {
		return nil
	}
	var keep []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			keep = append(keep, p)
		}
	}
	for _, c := range changes {
		if step.Ignored(in.Paths, in.Config, c) {
			continue
		}
		add(c.Path)
		add(c.OldPath)
	}
	patch, err := in.Git.PatchPaths(ctx, from, to, keep)
	if err != nil {
		return nil
	}
	patch = strings.ToValidUTF8(patch, "�")
	cut := truncateUTF8(patch, MaxDiffBytes)
	return &Diff{Text: cut, Truncated: len(cut) < len(patch)}
}

func buildCases(cs *spec.CasesSpec) []CaseView {
	out := []CaseView{}
	if cs == nil {
		return out
	}
	for _, c := range cs.Cases {
		request := ""
		if c.When != nil {
			request = c.When.Method + " " + c.When.Path
		}
		raw, err := yamlx.Dump(c)
		if err != nil {
			raw = nil
		}
		out = append(out, CaseView{ID: c.ID, Step: c.Step, Kind: c.Kind, Title: c.Title, Request: request,
			Expect: expect(c), Raw: string(raw)})
	}
	return out
}

func expect(c spec.CaseSpec) string {
	if c.Then == nil {
		return ""
	}
	body := ""
	if c.Then.Body != nil {
		if b, err := marshalNoEscape(c.Then.Body); err == nil {
			body = string(b)
		} else {
			body = fmt.Sprint(c.Then.Body)
		}
	}
	body = prefixUTF16(body, expectBodyChars)
	return strings.TrimSpace(fmt.Sprintf("%d %s", c.Then.Status, body))
}

// prefixUTF16 giữ tối đa n đơn vị UTF-16 đầu (String.substring của Java), không cắt giữa cặp surrogate.
func prefixUTF16(s string, n int) string {
	units := 0
	for i, r := range s {
		w := len(utf16.Encode([]rune{r}))
		if units+w > n {
			return s[:i]
		}
		units += w
	}
	return s
}

func openQuestions(p model.FeaturePaths) ([]OpenQuestion, error) {
	out := []OpenQuestion{}
	for _, g := range []gate.Gate{gate.BRD, gate.DESIGN} {
		file := p.Brd()
		if g == gate.DESIGN {
			file = p.DesignMd()
		}
		text, ok, err := readText(file)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, q := range spec.OpenQuestions(text) {
			out = append(out, OpenQuestion{Gate: g, Text: q})
		}
	}
	return out, nil
}

func buildArtifacts(p model.FeaturePaths, truncated *bool) (*Ordered[Artifact], error) {
	files := []string{p.Brd(), p.DesignMd(), p.DesignYaml(), p.Cases(), p.Feedback()}
	if es, err := os.ReadDir(p.DbDrafts()); err == nil {
		var names []string
		for _, e := range es {
			if st, err := os.Stat(filepath.Join(p.DbDrafts(), e.Name())); err == nil && st.Mode().IsRegular() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			files = append(files, filepath.Join(p.DbDrafts(), n))
		}
	}
	out := newOrdered[Artifact]()
	for _, f := range files {
		text, ok, err := readText(f)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		cut := truncateUTF8(text, MaxArtifactBytes)
		done := len(cut) < len(text)
		if done {
			*truncated = true
		}
		rel, err := filepath.Rel(p.Dir(), f)
		if err != nil {
			rel = filepath.Base(f)
		}
		out.Put(filepath.ToSlash(rel), Artifact{Content: cut, Truncated: done})
	}
	return out, nil
}

// readText đọc file thường (UTF-8, byte hỏng thành U+FFFD như Java); ok=false khi không có.
func readText(f string) (string, bool, error) {
	st, err := os.Stat(f)
	if err != nil || !st.Mode().IsRegular() {
		return "", false, nil
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return "", false, fmt.Errorf("Không đọc được %s: %v", f, err)
	}
	return strings.ToValidUTF8(string(b), "�"), true, nil
}

// rawJSON là nội dung JSON (gọn) của file; nil khi chưa có hoặc hỏng.
func rawJSON(f string) (json.RawMessage, error) {
	text, ok, err := readText(f)
	if err != nil || !ok {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(text)); err != nil {
		return nil, nil // file hỏng: coi như chưa có
	}
	return buf.Bytes(), nil
}

// truncateUTF8 cắt theo số byte UTF-8, không cắt giữa ký tự.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}
