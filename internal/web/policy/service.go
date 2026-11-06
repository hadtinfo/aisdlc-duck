// Package policy là chính sách rủi ro theo repo trên web (PolicyController/PolicyService/PolicyRepository.java):
// nhập lần đầu từ CLI, API ký cho CLI, xem, lịch sử, xuất bản, kiểm tra và phân loại thử cho UI.
package policy

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"aisdlc/internal/risk"
	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/signing"
)

// ImportReason là lý do ghi cho phiên bản nhập lần đầu.
const ImportReason = "Nhập từ aisdlc/risk-policy.yaml"

// Service là phiên bản chính sách rủi ro của repo.
type Service struct {
	Pool *pgxpool.Pool
	Keys *signing.Keys
	Auth *auth.Service // ghi audit (nil: bỏ qua)
	Now  func() time.Time
}

// Conflict: có bản mới hơn (hoặc bằng) khi xuất bản.
type Conflict struct{ Latest int }

func (c *Conflict) Error() string {
	return "Đã có phiên bản mới hơn (v" + strconv.Itoa(c.Latest) + "); tải lại để xem."
}

// Row là một hàng risk_policy_versions.
type Row struct {
	Version   int
	Body      string
	Hash      string
	Author    string
	Reason    string
	Source    string
	CreatedAt time.Time
}

const cols = "version, body, hash, author, reason, source, created_at"

func scanRow(row pgx.Row) (Row, error) {
	var r Row
	err := row.Scan(&r.Version, &r.Body, &r.Hash, &r.Author, &r.Reason, &r.Source, &r.CreatedAt)
	return r, err
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func latest(ctx context.Context, db querier, repo string) (*Row, error) {
	r, err := scanRow(db.QueryRow(ctx, "SELECT "+cols+" FROM risk_policy_versions WHERE repo_id = $1 ORDER BY version DESC LIMIT 1", repo))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// lock tuần tự hóa nhập/xuất bản của cùng repo (khóa giao dịch).
func lock(ctx context.Context, tx pgx.Tx, repo string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('risk-policy' || chr(10) || $1, 0))", repo)
	return err
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) inTx(ctx context.Context, f func(tx pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// withVersion gán số phiên bản của web vào chính sách để khớp với policyVersion CLI báo lại.
func withVersion(p risk.RiskPolicy, v int) risk.RiskPolicy {
	p.Version = v
	return p
}

// insert ghi phiên bản; false khi (repo, version) đã có.
func insert(ctx context.Context, tx pgx.Tx, repo string, version int, p risk.RiskPolicy, author, reason, source string) (bool, error) {
	v := withVersion(p, version)
	y, err := v.ToYAML()
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, "INSERT INTO risk_policy_versions(repo_id, version, body, hash, author, reason, source) "+
		"VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING", repo, version, y, v.Hash(), author, reason, source)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Service) audit(ctx context.Context, tx pgx.Tx, e auth.AuditEntry) error {
	if s.Auth == nil {
		return nil
	}
	return s.Auth.WriteAudit(ctx, tx, e)
}

// ImportFirst nhập lần đầu. YAML hỏng → *risk.PolicyError; đã có phiên bản → không đổi gì (created=false).
func (s *Service) ImportFirst(ctx context.Context, repo, yaml, author, ip string) (created bool, version int, err error) {
	p, err := risk.ParsePolicy(yaml)
	if err != nil {
		return false, 0, err
	}
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if err := lock(ctx, tx, repo); err != nil {
			return err
		}
		live, err := latest(ctx, tx, repo)
		if err != nil {
			return err
		}
		if live != nil {
			version = live.Version
			return nil
		}
		ok, err := insert(ctx, tx, repo, 1, p, author, ImportReason, "import")
		if err != nil {
			return err
		}
		created, version = ok, 1
		if !ok {
			return errors.New("policy: nhập lần đầu bị trùng dù đã khóa")
		}
		return s.audit(ctx, tx, auth.AuditEntry{Login: author, IP: ip, Channel: "cli", Action: "policy.import",
			Repo: repo, Target: "1", Detail: map[string]any{"version": 1}})
	})
	return created, version, err
}

// CliPayload là payload ký cho CLI: {repoId, version, yaml}; nil khi repo chưa có chính sách.
type CliPayload struct {
	RepoID  string `json:"repoId"`
	Version int    `json:"version"`
	YAML    string `json:"yaml"`
}

// CliPayloadOf đọc bản mới nhất của repo.
func (s *Service) CliPayloadOf(ctx context.Context, repo string) (*CliPayload, error) {
	r, err := latest(ctx, s.Pool, repo)
	if err != nil || r == nil {
		return nil, err
	}
	return &CliPayload{RepoID: repo, Version: r.Version, YAML: r.Body}, nil
}

// ChangeJSON là một PolicyChange như Jackson ghi (before/after null khi không có).
type ChangeJSON struct {
	Kind      string  `json:"kind"`
	Key       string  `json:"key"`
	Before    *string `json:"before"`
	After     *string `json:"after"`
	Loosening bool    `json:"loosening"`
}

func changes(l []risk.PolicyChange) []ChangeJSON {
	out := make([]ChangeJSON, 0, len(l))
	for _, c := range l {
		x := ChangeJSON{Kind: c.Kind, Key: c.Key, Loosening: c.Loosening}
		if c.Before != "" {
			b := c.Before
			x.Before = &b
		}
		if c.After != "" {
			a := c.After
			x.After = &a
		}
		out = append(out, x)
	}
	return out
}

// View là GET /api/policy.
type View struct {
	Version       int             `json:"version"`
	Policy        json.RawMessage `json:"policy"`
	Hash          string          `json:"hash"`
	YAML          string          `json:"yaml"`
	PresetUpdate  map[string]any  `json:"presetUpdate"`
	PresetRuleIDs []string        `json:"presetRuleIds"`
	FeatureCounts map[string]int  `json:"featureCounts"`
}

// ViewOf trả nil khi repo chưa có chính sách.
func (s *Service) ViewOf(ctx context.Context, repo string) (*View, error) {
	r, err := latest(ctx, s.Pool, repo)
	if err != nil || r == nil {
		return nil, err
	}
	p, err := risk.ParsePolicy(r.Body)
	if err != nil {
		return nil, err
	}
	pu, err := presetUpdate(p)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	rows, err := s.Pool.Query(ctx, "SELECT risk_policy_version, count(*) FROM features WHERE repo_id = $1 "+
		"AND risk_policy_version IS NOT NULL GROUP BY risk_policy_version ORDER BY risk_policy_version", repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v int
		var n int
		if err := rows.Scan(&v, &n); err != nil {
			return nil, err
		}
		counts[strconv.Itoa(v)] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &View{Version: r.Version, Policy: json.RawMessage(p.ToJSON()), Hash: r.Hash, YAML: r.Body,
		PresetUpdate: pu, PresetRuleIDs: presetRuleIDs(p), FeatureCounts: counts}, nil
}

// VersionJSON là một mục của GET /api/policy/versions.
type VersionJSON struct {
	Version   int          `json:"version"`
	Author    string       `json:"author"`
	Reason    string       `json:"reason"`
	Source    string       `json:"source"`
	CreatedAt string       `json:"createdAt"`
	Changes   []ChangeJSON `json:"changes"`
}

// Versions liệt kê các phiên bản, mới nhất trước, kèm thay đổi so với bản liền trước.
func (s *Service) Versions(ctx context.Context, repo string) ([]VersionJSON, error) {
	rs, err := s.Pool.Query(ctx, "SELECT "+cols+" FROM risk_policy_versions WHERE repo_id = $1 ORDER BY version DESC", repo)
	if err != nil {
		return nil, err
	}
	var rows []Row
	for rs.Next() {
		var r Row
		if err := rs.Scan(&r.Version, &r.Body, &r.Hash, &r.Author, &r.Reason, &r.Source, &r.CreatedAt); err != nil {
			rs.Close()
			return nil, err
		}
		rows = append(rows, r)
	}
	rs.Close()
	if err := rs.Err(); err != nil {
		return nil, err
	}
	out := make([]VersionJSON, 0, len(rows))
	for i, r := range rows {
		ch := []risk.PolicyChange{}
		if i+1 < len(rows) {
			prev, err := risk.ParsePolicy(rows[i+1].Body)
			if err != nil {
				return nil, err
			}
			cur, err := risk.ParsePolicy(r.Body)
			if err != nil {
				return nil, err
			}
			ch = risk.DiffPolicies(prev, cur)
		}
		out = append(out, VersionJSON{Version: r.Version, Author: r.Author, Reason: r.Reason, Source: r.Source,
			CreatedAt: httpx.Instant(r.CreatedAt), Changes: changes(ch)})
	}
	return out, nil
}

func hasPreset(p risk.RiskPolicy) bool {
	if p.Preset == nil || p.Preset.Name == "" {
		return false
	}
	for _, n := range risk.PresetNames() {
		if n == p.Preset.Name {
			return true
		}
	}
	return false
}

// presetUpdate: thông báo mẫu mới; nil khi không có preset hoặc đã dùng bản mới nhất đi kèm. "policy" là bản mẫu mới
// (JSON như GET /api/policy) để UI áp từng thay đổi vào nháp.
func presetUpdate(live risk.RiskPolicy) (map[string]any, error) {
	if !hasPreset(live) {
		return nil, nil
	}
	name := live.Preset.Name
	to := risk.PresetLatestVersion(name)
	from := live.Preset.Version
	if to == from {
		return nil, nil
	}
	latestP, err := risk.LatestPreset(name)
	if err != nil {
		return nil, err
	}
	baseMissing := false
	base, err := risk.LoadPreset(name, from)
	if err != nil {
		var pe *risk.PolicyError
		if !errors.As(err, &pe) {
			return nil, err
		}
		base, baseMissing = live, true
	}
	m := map[string]any{"name": name, "from": from, "to": to,
		"changes": changes(risk.DiffPolicies(base, latestP)), "policy": json.RawMessage(latestP.ToJSON())}
	if baseMissing {
		m["baseMissing"] = true
	}
	return m, nil
}

// presetRuleIDs: id rule của đúng phiên bản mẫu mà chính sách ghi trong preset; rỗng khi không có hoặc không đi kèm.
func presetRuleIDs(live risk.RiskPolicy) []string {
	out := []string{}
	if !hasPreset(live) {
		return out
	}
	p, err := risk.LoadPreset(live.Preset.Name, live.Preset.Version)
	if err != nil {
		return out
	}
	for _, r := range p.Rules {
		out = append(out, r.ID)
	}
	return out
}

// Publish xuất bản phiên bản kế tiếp; trả {version, loosening}. Lỗi chính sách → *risk.PolicyError, bản cũ → *Conflict.
func (s *Service) Publish(ctx context.Context, repo string, base int, policyJSON string, reason string, fromPreset bool,
	author, ip string) (map[string]any, error) {
	p, err := risk.PolicyFromJSON(policyJSON)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if err := lock(ctx, tx, repo); err != nil {
			return err
		}
		live, err := latest(ctx, tx, repo)
		if err != nil {
			return err
		}
		cur := 0
		loosening := []ChangeJSON{}
		if live != nil {
			cur = live.Version
		}
		if base != cur {
			return &Conflict{Latest: cur}
		}
		if live != nil {
			old, err := risk.ParsePolicy(live.Body)
			if err != nil {
				return err
			}
			for _, c := range changes(risk.DiffPolicies(old, p)) {
				if c.Loosening {
					loosening = append(loosening, c)
				}
			}
		}
		next := cur + 1
		source := "web"
		if fromPreset {
			source = "preset-update"
		}
		ok, err := insert(ctx, tx, repo, next, p, author, reason, source)
		if err != nil {
			return err
		}
		if !ok {
			return &Conflict{Latest: next}
		}
		if err := s.audit(ctx, tx, auth.AuditEntry{Login: author, IP: ip, Channel: "web", Action: "policy.publish",
			Repo: repo, Target: strconv.Itoa(next), Detail: map[string]any{"version": next, "source": source,
				"loosening": len(loosening)}}); err != nil {
			return err
		}
		out = map[string]any{"version": next, "loosening": loosening}
		return nil
	})
	return out, err
}

// Validate trả lỗi kiểm tra (nếu có) và thay đổi so với bản đang hiệu lực của repo (rỗng khi repo=nil hoặc chưa có).
func (s *Service) Validate(ctx context.Context, repo *string, policyJSON string) (map[string]any, error) {
	p, err := risk.PolicyFromJSON(policyJSON)
	if err != nil {
		var pe *risk.PolicyError
		if errors.As(err, &pe) {
			return map[string]any{"errors": pe.Errors, "changes": []ChangeJSON{}}, nil
		}
		return nil, err
	}
	ch := []risk.PolicyChange{}
	if repo != nil {
		r, err := latest(ctx, s.Pool, *repo)
		if err != nil {
			return nil, err
		}
		if r != nil {
			old, err := risk.ParsePolicy(r.Body)
			if err != nil {
				return nil, err
			}
			ch = risk.DiffPolicies(old, p)
		}
	}
	return map[string]any{"errors": []string{}, "changes": changes(ch)}, nil
}

type inputsJSON struct {
	Text       string   `json:"text"`
	Paths      []string `json:"paths"`
	Tables     []string `json:"tables"`
	Endpoints  []string `json:"endpoints"`
	Labels     []string `json:"labels"`
	AgentLevel string   `json:"agentLevel"`
}

type matchJSON struct {
	Rule  string `json:"rule"`
	Level string `json:"level"`
	On    string `json:"on"`
}

type resultJSON struct {
	Level      string      `json:"level"`
	RuleLevel  string      `json:"ruleLevel"`
	Matched    []matchJSON `json:"matched"`
	AgentLevel *string     `json:"agentLevel"`
}

// Classify phân loại thử bằng đúng bộ phân loại của risk.
func Classify(policyJSON, inputs string) (map[string]any, error) {
	p, err := risk.PolicyFromJSON(policyJSON)
	if err != nil {
		return nil, err
	}
	var in inputsJSON
	if err := json.Unmarshal([]byte(inputs), &in); err != nil {
		return nil, &risk.PolicyError{Errors: []string{"inputs không hợp lệ: " + err.Error()}}
	}
	r := risk.Classify(p, risk.RiskInputs{Text: in.Text, Paths: in.Paths, Tables: in.Tables, Endpoints: in.Endpoints,
		Labels: in.Labels, AgentLevel: in.AgentLevel})
	res := resultJSON{Level: r.Level, RuleLevel: r.RuleLevel, Matched: []matchJSON{}}
	for _, m := range r.Matched {
		res.Matched = append(res.Matched, matchJSON{m.Rule, m.Level, m.On})
	}
	if r.AgentLevel != "" {
		a := r.AgentLevel
		res.AgentLevel = &a
	}
	reqs := []map[string]any{}
	for _, q := range p.RequirementsFor(r.Level) {
		params := q.Params
		if params == nil {
			params = map[string]any{}
		}
		reqs = append(reqs, map[string]any{"id": q.ID, "name": risk.RequirementDisplayName(q), "params": params})
	}
	return map[string]any{"result": res, "requirements": reqs}, nil
}
