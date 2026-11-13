package board

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"aisdlc/internal/web/httpx"
)

// Các kiểu JSON của GET /api/features/{repo}/{key}/attempts: các lần làm đã khép (rewind), mới nhất trước, chỉ đọc.
type attemptJSON struct {
	Attempt    int             `json:"attempt"`
	Rewind     rewindJSON      `json:"rewind"`
	Gates      []attemptGate   `json:"gates"`
	Steps      []attemptStep   `json:"steps"`
	ReviewCard json.RawMessage `json:"reviewCard,omitempty"`
}

type rewindJSON struct {
	Checkpoint string `json:"checkpoint"`
	By         string `json:"by"`
	At         string `json:"at"`
}

type attemptGate struct {
	Gate        string           `json:"gate"`
	Submissions []attemptSub     `json:"submissions"`
	Feedback    []attemptFeed    `json:"feedback"`
	Approvals   []attemptApprove `json:"approvals"`
}

type attemptSub struct {
	Hash      string        `json:"hash"`
	At        string        `json:"at"`
	By        string        `json:"by"`
	Artifacts []attemptFile `json:"artifacts"`
}

type attemptFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type attemptFeed struct {
	Kind    string `json:"kind"`
	By      string `json:"by"`
	At      string `json:"at"`
	Comment string `json:"comment"`
}

type attemptApprove struct {
	Approver  string  `json:"approver"`
	At        string  `json:"at"`
	RevokedAt *string `json:"revokedAt"`
}

type attemptStep struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Files       int    `json:"files"`
	Lines       int    `json:"lines"`
	CasesPassed int    `json:"casesPassed"`
	CasesTotal  int    `json:"casesTotal"`
}

var gateOrder = map[string]int{"BRD": 0, "DESIGN": 1, "CASES": 2, "REVIEW": 3}

// Attempts đọc các lần làm đã khép của một tính năng. Duyệt (approvals) thuộc lần làm của submission mà nó duyệt.
func (r *Reader) Attempts(ctx context.Context, repoID, key string) ([]attemptJSON, error) {
	out := []attemptJSON{}
	rows, err := r.Pool.Query(ctx, "SELECT id, coalesce(from_attempt, 0), coalesce(checkpoint, ''), coalesce(by_login, ''), at, "+
		"coalesce(snapshot::text, 'null') FROM feature_rewinds WHERE repo_id = $1 AND feature_key = $2 ORDER BY id DESC", repoID, key)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*attemptJSON{}
	var ids []int64
	for rows.Next() {
		var id int64
		var a attemptJSON
		var at time.Time
		var snap string
		if err := rows.Scan(&id, &a.Attempt, &a.Rewind.Checkpoint, &a.Rewind.By, &at, &snap); err != nil {
			rows.Close()
			return nil, err
		}
		a.Rewind.At = httpx.Instant(at.UTC())
		a.Gates, a.Steps = []attemptGate{}, []attemptStep{}
		a.fromSnapshot([]byte(snap))
		out = append(out, a)
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		byID[ids[i]] = &out[i]
	}
	if len(out) == 0 {
		return out, nil
	}
	perAttempt := map[int64]map[string]*attemptGate{}
	gate := func(id int64, g string) *attemptGate {
		m := perAttempt[id]
		if m == nil {
			m = map[string]*attemptGate{}
			perAttempt[id] = m
		}
		if m[g] == nil {
			m[g] = &attemptGate{Gate: g, Submissions: []attemptSub{}, Feedback: []attemptFeed{}, Approvals: []attemptApprove{}}
		}
		return m[g]
	}

	srows, err := r.Pool.Query(ctx, "SELECT rewind_id, gate, hash, submitted_at, submitted_by, artifacts::text FROM submissions "+
		"WHERE repo_id = $1 AND feature_key = $2 AND rewind_id IS NOT NULL ORDER BY id", repoID, key)
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var id int64
		var g string
		var s attemptSub
		var at time.Time
		var aj string
		if err := srows.Scan(&id, &g, &s.Hash, &at, &s.By, &aj); err != nil {
			srows.Close()
			return nil, err
		}
		s.At = httpx.Instant(at.UTC())
		if s.Artifacts, err = decodeArtifacts(aj); err != nil {
			srows.Close()
			return nil, fmt.Errorf("submission %s: %w", s.Hash, err)
		}
		gt := gate(id, g)
		gt.Submissions = append(gt.Submissions, s)
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return nil, err
	}

	frows, err := r.Pool.Query(ctx, "SELECT rewind_id, gate, kind, by_login, at, comment FROM feedback "+
		"WHERE repo_id = $1 AND feature_key = $2 AND rewind_id IS NOT NULL AND kind <> 'REWOUND' ORDER BY id", repoID, key)
	if err != nil {
		return nil, err
	}
	for frows.Next() {
		var id int64
		var g string
		var f attemptFeed
		var at time.Time
		if err := frows.Scan(&id, &g, &f.Kind, &f.By, &at, &f.Comment); err != nil {
			frows.Close()
			return nil, err
		}
		f.At = httpx.Instant(at.UTC())
		gt := gate(id, g)
		gt.Feedback = append(gt.Feedback, f)
	}
	frows.Close()
	if err := frows.Err(); err != nil {
		return nil, err
	}

	arows, err := r.Pool.Query(ctx, "SELECT s.rewind_id, a.gate, a.approver, a.at, a.revoked_at FROM approvals a "+
		"JOIN submissions s ON s.id = a.submission_id WHERE a.repo_id = $1 AND a.feature_key = $2 AND s.rewind_id IS NOT NULL ORDER BY a.id", repoID, key)
	if err != nil {
		return nil, err
	}
	for arows.Next() {
		var id int64
		var g string
		var ap attemptApprove
		var at time.Time
		var rev *time.Time
		if err := arows.Scan(&id, &g, &ap.Approver, &at, &rev); err != nil {
			arows.Close()
			return nil, err
		}
		ap.At = httpx.Instant(at.UTC())
		if rev != nil {
			s := httpx.Instant(rev.UTC())
			ap.RevokedAt = &s
		}
		gt := gate(id, g)
		gt.Approvals = append(gt.Approvals, ap)
	}
	arows.Close()
	if err := arows.Err(); err != nil {
		return nil, err
	}

	for id, m := range perAttempt {
		a := byID[id]
		for _, g := range m {
			a.Gates = append(a.Gates, *g)
		}
		sort.Slice(a.Gates, func(i, j int) bool { return gateOrder[a.Gates[i].Gate] < gateOrder[a.Gates[j].Gate] })
	}
	return out, nil
}

func decodeArtifacts(aj string) ([]attemptFile, error) {
	var raw []struct {
		Path    string  `json:"path"`
		Content *string `json:"content"`
	}
	if err := json.Unmarshal([]byte(aj), &raw); err != nil {
		return nil, err
	}
	out := make([]attemptFile, 0, len(raw))
	for _, a := range raw {
		f := attemptFile{Path: a.Path}
		if a.Content != nil {
			b, err := base64.StdEncoding.DecodeString(*a.Content)
			if err != nil {
				if b, err = base64.RawStdEncoding.DecodeString(*a.Content); err != nil {
					return nil, fmt.Errorf("artifact %s: %w", a.Path, err)
				}
			}
			f.Content = string(b)
		}
		out = append(out, f)
	}
	return out, nil
}

// fromSnapshot lấy bước và thẻ review từ snapshot lúc rewind; snapshot hỏng/thiếu thì để trống.
func (a *attemptJSON) fromSnapshot(raw []byte) {
	var s struct {
		Steps []struct {
			ID      string   `json:"id"`
			Status  string   `json:"status"`
			CaseIDs []string `json:"caseIds"`
			Record  *struct {
				Check *struct {
					Files       int   `json:"files"`
					Lines       int   `json:"lines"`
					CasesPassed int64 `json:"casesPassed"`
				} `json:"check"`
			} `json:"record"`
		} `json:"steps"`
		ReviewCards map[string]json.RawMessage `json:"reviewCards"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return
	}
	for _, st := range s.Steps {
		v := attemptStep{ID: st.ID, Status: st.Status, CasesTotal: len(st.CaseIDs)}
		if st.Record != nil && st.Record.Check != nil {
			v.Files, v.Lines, v.CasesPassed = st.Record.Check.Files, st.Record.Check.Lines, int(st.Record.Check.CasesPassed)
		}
		a.Steps = append(a.Steps, v)
	}
	a.ReviewCard = s.ReviewCards["REVIEW"]
}
