package gates

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"aisdlc/internal/gate"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/web/auth"
	"aisdlc/internal/web/review"
)

// Kho submission / approval / feedback của cổng trên web (GateRepository.java). Mọi hàm nhận db là pool hoặc tx: thao
// tác duyệt chạy trong một tx có khóa theo tính năng (store.LockFeature) và đọc lại trạng thái trong chính tx đó.
//
// Submission "còn hiệu lực" của một cổng = bản mới nhất, nộp sau feedback mới nhất của cổng đó (trả lại/mở lại xóa
// bản nộp như store.clear ở giai đoạn 1; bản ghi vẫn giữ). Approval "còn hiệu lực" = chưa thu hồi và, nếu là nhập từ
// local, đã được xác nhận.

type db = auth.DB

const liveSubmission = " AND s.submitted_at > coalesce((SELECT max(f.at) FROM feedback f WHERE f.repo_id = s.repo_id " +
	"AND f.feature_key = s.feature_key AND f.gate = s.gate AND f.rewind_id IS NULL), '-infinity') AND s.rewind_id IS NULL "

const liveApproval = " AND a.revoked_at IS NULL AND (a.channel <> 'import' OR a.confirmed_at IS NOT NULL) "

const apprCols = "a.id, a.submission_id, a.gate, a.hash, a.approver, a.channel, a.at, a.evaluated_tree, a.rule"

type subRow struct {
	ID       int64
	Gate     string
	Hash     string
	Findings []finding
	By       string
	At       time.Time
}

type apprRow struct {
	ID, SubmissionID              int64
	Gate, Hash, Approver, Channel string
	At                            time.Time
	EvaluatedTree, Rule           *string
}

// PendingImport là approval nhập từ local chưa xác nhận (chưa có hiệu lực).
type PendingImport struct {
	ID               int64
	Gate             string
	Hash             string
	OriginalApprover *string
	OriginalAt       *time.Time
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func saveSubmission(ctx context.Context, d db, repo, feature string, g gate.Gate, hash string, fs []finding,
	arts []artifact, by string, at time.Time) (int64, error) {
	fj, err := json.Marshal(fs)
	if err != nil {
		return 0, err
	}
	aj, err := json.Marshal(arts)
	if err != nil {
		return 0, err
	}
	var id int64
	err = d.QueryRow(ctx, "INSERT INTO submissions(repo_id, feature_key, gate, hash, findings, artifacts, submitted_by, "+
		"submitted_at) VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8) RETURNING id",
		repo, feature, string(g), hash, string(fj), string(aj), by, at).Scan(&id)
	return id, err
}

// latestSubmission: submission còn hiệu lực mới nhất của cổng; nil nếu không có.
func latestSubmission(ctx context.Context, d db, repo, feature string, g gate.Gate) (*subRow, error) {
	var s subRow
	var fj string
	err := d.QueryRow(ctx, "SELECT s.id, s.gate, s.hash, s.findings::text, s.submitted_by, s.submitted_at FROM submissions s "+
		"WHERE s.repo_id = $1 AND s.feature_key = $2 AND s.gate = $3"+liveSubmission+"ORDER BY s.id DESC LIMIT 1",
		repo, feature, string(g)).Scan(&s.ID, &s.Gate, &s.Hash, &fj, &s.By, &s.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(fj), &s.Findings); err != nil {
		return nil, fmt.Errorf("submission %d findings: %w", s.ID, err)
	}
	s.At = s.At.UTC()
	return &s, nil
}

// submissionCount: số lần nộp (kể cả bản đã bị trả lại) của cổng.
func submissionCount(ctx context.Context, d db, repo, feature string, g gate.Gate) (int, error) {
	var n int
	err := d.QueryRow(ctx, "SELECT count(*) FROM submissions WHERE repo_id = $1 AND feature_key = $2 AND gate = $3 AND rewind_id IS NULL",
		repo, feature, string(g)).Scan(&n)
	return n, err
}

// lastSubmissions: các lần nộp mới nhất của cổng kèm artifact, mới trước.
func lastSubmissions(ctx context.Context, d db, repo, feature string, g gate.Gate, limit int) ([]review.StoredSubmission, error) {
	rows, err := d.Query(ctx, "SELECT id, submitted_at, artifacts::text FROM submissions WHERE repo_id = $1 AND feature_key = $2 "+
		"AND gate = $3 AND rewind_id IS NULL ORDER BY id DESC LIMIT $4", repo, feature, string(g), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []review.StoredSubmission
	for rows.Next() {
		var s review.StoredSubmission
		var aj string
		if err := rows.Scan(&s.ID, &s.At, &aj); err != nil {
			return nil, err
		}
		s.At = s.At.UTC()
		var arts []artifact
		if err := json.Unmarshal([]byte(aj), &arts); err != nil {
			return nil, fmt.Errorf("submission %d artifacts: %w", s.ID, err)
		}
		for _, a := range arts {
			sa := review.StoredArtifact{Path: a.Path}
			if a.Content != nil {
				b, err := base64.StdEncoding.DecodeString(*a.Content)
				if err != nil {
					if b, err = base64.RawStdEncoding.DecodeString(*a.Content); err != nil {
						return nil, fmt.Errorf("submission %d artifact %s: %w", s.ID, a.Path, err)
					}
				}
				if b == nil {
					b = []byte{}
				}
				sa.Content = b
			}
			s.Artifacts = append(s.Artifacts, sa)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func scanFeedback(rows pgx.Rows) ([]review.FeedbackRow, error) {
	defer rows.Close()
	var out []review.FeedbackRow
	for rows.Next() {
		var f review.FeedbackRow
		if err := rows.Scan(&f.ID, &f.Gate, &f.Kind, &f.By, &f.At, &f.Comment); err != nil {
			return nil, err
		}
		f.At = f.At.UTC()
		out = append(out, f)
	}
	return out, rows.Err()
}

// feedbackBetween: feedback của cổng có thời điểm trong (from, to), cũ trước.
func feedbackBetween(ctx context.Context, d db, repo, feature string, g gate.Gate, from, to time.Time) ([]review.FeedbackRow, error) {
	rows, err := d.Query(ctx, "SELECT id, gate, kind, by_login, at, comment FROM feedback WHERE repo_id = $1 AND feature_key = $2 "+
		"AND gate = $3 AND at > $4 AND at < $5 AND rewind_id IS NULL AND kind <> 'REWOUND' ORDER BY id", repo, feature, string(g), from, to)
	if err != nil {
		return nil, err
	}
	return scanFeedback(rows)
}

func oneApproval(ctx context.Context, d db, sql string, args ...any) (*apprRow, error) {
	var a apprRow
	err := d.QueryRow(ctx, sql, args...).Scan(&a.ID, &a.SubmissionID, &a.Gate, &a.Hash, &a.Approver, &a.Channel, &a.At,
		&a.EvaluatedTree, &a.Rule)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.At = a.At.UTC()
	return &a, nil
}

// activeApproval: approval còn hiệu lực mới nhất của cổng; nil nếu không có.
func activeApproval(ctx context.Context, d db, repo, feature string, g gate.Gate) (*apprRow, error) {
	return oneApproval(ctx, d, "SELECT "+apprCols+" FROM approvals a WHERE a.repo_id = $1 AND a.feature_key = $2 "+
		"AND a.gate = $3"+liveApproval+"ORDER BY a.id DESC LIMIT 1", repo, feature, string(g))
}

// lastSuperseded: bản tự duyệt bị thay thế gần nhất của cổng (revoked_by = 'auto-supersede').
func lastSuperseded(ctx context.Context, d db, repo, feature string, g gate.Gate) (*apprRow, error) {
	return oneApproval(ctx, d, "SELECT "+apprCols+" FROM approvals a WHERE a.repo_id = $1 AND a.feature_key = $2 "+
		"AND a.gate = $3 AND a.channel = 'auto' AND a.revoked_by = 'auto-supersede' ORDER BY a.id DESC LIMIT 1",
		repo, feature, string(g))
}

// approvalInsert là một approval mới; Confirmed = false chỉ với bản nhập từ local chờ xác nhận.
type approvalInsert struct {
	SubmissionID               int64
	Repo, Feature              string
	Gate                       gate.Gate
	Hash, Approver, Channel    string
	EvaluatedTree              *string
	FlagsJSON, AssumptionsJSON string
	OriginalApprover           *string
	OriginalAt                 *time.Time
	Confirmed                  bool
	At                         time.Time
}

func saveApproval(ctx context.Context, d db, a approvalInsert) (int64, error) {
	var confirmed *time.Time
	if a.Confirmed {
		confirmed = &a.At
	}
	var id int64
	err := d.QueryRow(ctx, "INSERT INTO approvals(submission_id, repo_id, feature_key, gate, hash, approver, channel, at, "+
		"evaluated_tree, flags, assumptions_seen, original_approver, original_at, confirmed_at) "+
		"VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb,$12,$13,$14) RETURNING id",
		a.SubmissionID, a.Repo, a.Feature, string(a.Gate), a.Hash, a.Approver, a.Channel, a.At, a.EvaluatedTree,
		a.FlagsJSON, a.AssumptionsJSON, a.OriginalApprover, a.OriginalAt, confirmed).Scan(&id)
	return id, err
}

// saveAutoApproval: server tự duyệt (approver = channel = auto, kèm rule "flow:<level>"), có hiệu lực ngay.
func saveAutoApproval(ctx context.Context, d db, submissionID int64, repo, feature string, g gate.Gate, hash, rule string,
	at time.Time) (int64, error) {
	var id int64
	err := d.QueryRow(ctx, "INSERT INTO approvals(submission_id, repo_id, feature_key, gate, hash, approver, channel, rule, at, "+
		"confirmed_at) VALUES ($1,$2,$3,$4,$5,'auto','auto',$6,$7,$7) RETURNING id",
		submissionID, repo, feature, string(g), hash, rule, at).Scan(&id)
	return id, err
}

func gateNames(gs []gate.Gate) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = string(g)
	}
	return out
}

func collectStrings(rows pgx.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if out == nil {
		out = []string{}
	}
	return out, err
}

// supersedeAuto thay thế bản tự duyệt còn hiệu lực của các cổng (revoked_by = 'auto-supersede'); bản duyệt tay không
// đổi. Trả các cổng bị thay thế.
func supersedeAuto(ctx context.Context, d db, repo, feature string, gs []gate.Gate, at time.Time) ([]string, error) {
	if len(gs) == 0 {
		return []string{}, nil
	}
	rows, err := d.Query(ctx, "UPDATE approvals SET revoked_at = $1, revoked_by = 'auto-supersede' WHERE repo_id = $2 "+
		"AND feature_key = $3 AND channel = 'auto' AND gate = ANY($4) AND revoked_at IS NULL RETURNING gate",
		at, repo, feature, gateNames(gs))
	return collectStrings(rows, err)
}

// supersededGates: cổng mà bản duyệt gần nhất bị thay thế tự động (auto-supersede).
func supersededGates(ctx context.Context, d db, repo, feature string) (map[string]bool, error) {
	rows, err := d.Query(ctx, "SELECT DISTINCT ON (gate) gate, coalesce(revoked_by, '') FROM approvals WHERE repo_id = $1 "+
		"AND feature_key = $2 ORDER BY gate, id DESC", repo, feature)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var g, by string
		if err := rows.Scan(&g, &by); err != nil {
			return nil, err
		}
		if by == "auto-supersede" {
			out[g] = true
		}
	}
	return out, rows.Err()
}

// revoke thu hồi mọi approval chưa thu hồi (kể cả nhập chưa xác nhận) của các cổng; trả các cổng bị thu hồi.
func revoke(ctx context.Context, d db, repo, feature string, gs []gate.Gate, by string, at time.Time) ([]string, error) {
	if len(gs) == 0 {
		return []string{}, nil
	}
	rows, err := d.Query(ctx, "UPDATE approvals SET revoked_at = $1, revoked_by = $2 WHERE repo_id = $3 AND feature_key = $4 "+
		"AND gate = ANY($5) AND revoked_at IS NULL RETURNING gate", at, by, repo, feature, gateNames(gs))
	return collectStrings(rows, err)
}

// saveFeedback: kind REJECTED | REOPENED; feedback mới làm submission cũ hơn của cổng hết hiệu lực.
func saveFeedback(ctx context.Context, d db, repo, feature string, g gate.Gate, kind, by, comment string, at time.Time) (int64, error) {
	var id int64
	err := d.QueryRow(ctx, "INSERT INTO feedback(repo_id, feature_key, gate, kind, by_login, comment, at) "+
		"VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id", repo, feature, string(g), kind, by, comment, at).Scan(&id)
	return id, err
}

// addInstruction ghi chỉ dẫn cho agent (bảng instructions; ControlRepository.addInstruction của Task 8 dùng chung).
func addInstruction(ctx context.Context, d db, repo, feature, by, text, source string, feedbackID *int64, at time.Time) (int64, error) {
	var id int64
	err := d.QueryRow(ctx, "INSERT INTO instructions(repo_id, feature_key, by_login, text, source, feedback_id, at) "+
		"VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id", repo, feature, by, text, source, feedbackID, at).Scan(&id)
	return id, err
}

// pendingImports: nhập từ local chưa xác nhận, chưa thu hồi (cũ trước).
func pendingImports(ctx context.Context, d db, repo, feature string) ([]PendingImport, error) {
	rows, err := d.Query(ctx, "SELECT id, gate, hash, original_approver, original_at FROM approvals WHERE repo_id = $1 "+
		"AND feature_key = $2 AND channel = 'import' AND confirmed_at IS NULL AND revoked_at IS NULL ORDER BY id", repo, feature)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PendingImport{}
	for rows.Next() {
		var p PendingImport
		if err := rows.Scan(&p.ID, &p.Gate, &p.Hash, &p.OriginalApprover, &p.OriginalAt); err != nil {
			return nil, err
		}
		p.OriginalAt = utc(p.OriginalAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

// confirmImport xác nhận bản nhập mới nhất đang chờ của cổng (confirmed_at, at, approver = người xác nhận); các bản
// nhập chờ cũ hơn của cổng bị thu hồi. nil nếu không có gì chờ.
func confirmImport(ctx context.Context, d db, repo, feature string, g gate.Gate, by string, at time.Time) (*PendingImport, error) {
	ps, err := pendingImports(ctx, d, repo, feature)
	if err != nil {
		return nil, err
	}
	var latest *PendingImport
	for i := range ps {
		if ps[i].Gate == string(g) {
			latest = &ps[i]
		}
	}
	if latest == nil {
		return nil, nil
	}
	if _, err := d.Exec(ctx, "UPDATE approvals SET confirmed_at = $1, at = $1, approver = $2 WHERE id = $3", at, by, latest.ID); err != nil {
		return nil, err
	}
	if _, err := d.Exec(ctx, "UPDATE approvals SET revoked_at = $1, revoked_by = $2 WHERE repo_id = $3 AND feature_key = $4 "+
		"AND gate = $5 AND channel = 'import' AND confirmed_at IS NULL AND revoked_at IS NULL", at, by, repo, feature, string(g)); err != nil {
		return nil, err
	}
	return latest, nil
}

// submittedPaths: đường dẫn artifact (không nội dung) của submission còn hiệu lực mới nhất mỗi cổng.
func submittedPaths(ctx context.Context, d db, repo, feature string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, g := range allGates {
		s, err := latestSubmission(ctx, d, repo, feature, g)
		if err != nil {
			return nil, err
		}
		if s == nil {
			continue
		}
		rows, err := d.Query(ctx, "SELECT e->>'path' FROM submissions s, jsonb_array_elements(s.artifacts) WITH ORDINALITY "+
			"AS t(e, n) WHERE s.id = $1 ORDER BY n", s.ID)
		if out[string(g)], err = collectStrings(rows, err); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func appr(a *apprRow) snapshot.Appr {
	out := snapshot.Appr{Gate: a.Gate, Hash: a.Hash, Approver: a.Approver, At: a.At, Channel: a.Channel}
	if a.EvaluatedTree != nil {
		out.EvaluatedTree = *a.EvaluatedTree
	}
	if a.Rule != nil {
		out.Rule = *a.Rule
	}
	return out
}

// state là trạng thái ký gửi CLI: mỗi cổng tối đa một submission và một approval còn hiệu lực (theo thứ tự cổng), toàn
// bộ feedback (cũ trước); cổng không còn bản hiệu lực mà bản gần nhất bị thay thế khi nâng mức thì gửi kèm bản đó
// (Superseded, chỉ để hiển thị). Chế độ khác web → danh sách rỗng.
func state(ctx context.Context, d db, repo, feature, mode string, now time.Time) (snapshot.FeatureGateState, error) {
	st := snapshot.FeatureGateState{Repo: repo, Feature: feature, Mode: mode, IssuedAt: now,
		Submissions: []snapshot.Sub{}, Approvals: []snapshot.Appr{}, Feedback: []snapshot.Fb{}}
	if mode != ModeWeb {
		return st, nil
	}
	st.Attempt = 1
	if err := d.QueryRow(ctx, "SELECT attempt FROM features WHERE repo_id = $1 AND feature_key = $2", repo, feature).
		Scan(&st.Attempt); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return st, err
	}
	sup, err := supersededGates(ctx, d, repo, feature)
	if err != nil {
		return st, err
	}
	for _, g := range allGates {
		s, err := latestSubmission(ctx, d, repo, feature, g)
		if err != nil {
			return st, err
		}
		if s != nil {
			st.Submissions = append(st.Submissions, snapshot.Sub{Gate: s.Gate, Hash: s.Hash, At: s.At, Findings: toSpec(s.Findings)})
		}
		a, err := activeApproval(ctx, d, repo, feature, g)
		if err != nil {
			return st, err
		}
		if a != nil {
			st.Approvals = append(st.Approvals, appr(a))
		} else if sup[string(g)] {
			l, err := lastSuperseded(ctx, d, repo, feature, g)
			if err != nil {
				return st, err
			}
			if l != nil {
				st.Superseded = append(st.Superseded, appr(l))
			}
		}
	}
	rows, err := d.Query(ctx, "SELECT id, gate, kind, by_login, at, comment FROM feedback WHERE repo_id = $1 AND feature_key = $2 "+
		"AND rewind_id IS NULL AND kind <> 'REWOUND' ORDER BY id", repo, feature)
	if err != nil {
		return st, err
	}
	fbs, err := scanFeedback(rows)
	if err != nil {
		return st, err
	}
	for _, f := range fbs {
		st.Feedback = append(st.Feedback, snapshot.Fb{ID: f.ID, Gate: f.Gate, Kind: f.Kind, By: f.By, At: f.At, Comment: f.Comment})
	}
	return st, nil
}
