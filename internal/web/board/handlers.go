package board

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"aisdlc/internal/gate"
	"aisdlc/internal/web/httpx"
)

const (
	featuresPrefix = "/api/features/"
	maxEventLimit  = 1000
)

type handlers struct {
	reader *Reader
	hub    *Hub
}

// Routes đăng ký GET /api/board, /api/features/** (chi tiết, /events, /stream, /resubmission, /control, /attempts) và /api/stream.
func Routes(r chi.Router, reader *Reader, hub *Hub) {
	h := &handlers{reader: reader, hub: hub}
	r.Get("/api/board", h.board)
	r.Get("/api/features/*", h.feature)
	r.Get("/api/stream", hub.ServeBoard)
}

// Card là một thẻ của bảng (thứ tự trường như BoardController.java).
type Card struct {
	RepoID        string       `json:"repoId"`
	Branch        *string      `json:"branch"`
	Key           string       `json:"key"`
	Title         *string      `json:"title"`
	Flow          *string      `json:"flow"`
	Owner         *string      `json:"owner"`
	Column        string       `json:"column"`
	Labels        []string     `json:"labels"`
	Eval          *EvalSummary `json:"eval"`
	SyncedAt      string       `json:"syncedAt"`
	ClockSkew     bool         `json:"clockSkew"`
	Session       string       `json:"session"`
	ApprovalsMode string       `json:"approvalsMode"`
	RiskLevel     *string      `json:"riskLevel"`
	Attempt       int          `json:"attempt"`
}

type inboxJSON struct {
	RepoID  string `json:"repoId"`
	Key     string `json:"key"`
	Kind    string `json:"kind"`
	Text    string `json:"text"`
	Command string `json:"command"`
}

type boardJSON struct {
	Me    *string     `json:"me"`
	Cards []Card      `json:"cards"`
	Inbox []inboxJSON `json:"inbox"`
}

var badRequest = httpx.Err(http.StatusBadRequest, httpx.Plain("Bad Request"))

// intParam như @RequestParam int có defaultValue của Spring: thiếu hoặc rỗng → def; bỏ mọi khoảng trắng; hex (0x, #)
// qua Integer.decode, còn lại Integer.valueOf; sai → 400.
func intParam(q url.Values, name string, def int) (int, error) {
	vs, ok := q[name]
	if !ok || len(vs) == 0 || vs[0] == "" {
		return def, nil
	}
	s := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, vs[0])
	if s == "" {
		return def, nil
	}
	neg := strings.HasPrefix(s, "-")
	body := strings.TrimPrefix(s, "-")
	var v int64
	var err error
	switch {
	case strings.HasPrefix(body, "0x"), strings.HasPrefix(body, "0X"):
		v, err = strconv.ParseInt(body[2:], 16, 64)
	case strings.HasPrefix(body, "#"):
		v, err = strconv.ParseInt(body[1:], 16, 64)
	default:
		v, err = strconv.ParseInt(s, 10, 32)
		neg = false
	}
	if neg {
		v = -v
	}
	if err != nil || v < -1<<31 || v > 1<<31-1 {
		return 0, badRequest
	}
	return int(v), nil
}

// textParam như getParameter: giá trị đầu, nil khi thiếu.
func textParam(q url.Values, name string) *string {
	if vs, ok := q[name]; ok && len(vs) > 0 {
		return &vs[0]
	}
	return nil
}

func (h *handlers) board(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	days, err := intParam(q, "hideDoneOlderThanDays", 7)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	repo, owner := textParam(q, "repo"), textParam(q, "owner")
	ctx := r.Context()
	out := boardJSON{Cards: []Card{}, Inbox: []inboxJSON{}}
	if p, ok := httpx.PrincipalFrom(ctx); ok {
		out.Me = &p.Login
	}
	emails, err := h.reader.EmailToLogin(ctx)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	sessions, err := h.reader.Sessions(ctx)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var doneCutoff *time.Time
	if days > 0 {
		c := h.reader.now().Add(-time.Duration(days) * 24 * time.Hour)
		doneCutoff = &c
	}
	rows, err := h.reader.All(ctx)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	// rows đã phủ trạng thái cổng của server cho repo chế độ web: cột, nhãn, hộp thư tính trên đó.
	for _, row := range rows {
		if repo != nil && !isBlank(*repo) && *repo != row.RepoID {
			continue
		}
		first := row.FirstSender
		ownerLogin := Owner(row.Tree, emails, &first)
		if owner != nil && !isBlank(*owner) && (ownerLogin == nil || *owner != *ownerLogin) {
			continue
		}
		column := Column(row.Tree)
		// mốc xong = lúc duyệt REVIEW (push lại không làm thẻ cũ hiện lại); thiếu thì lúc đồng bộ
		if column == "DONE" && doneCutoff != nil {
			doneAt, ok := DoneAt(row.Tree)
			if !ok {
				doneAt = row.SyncedAt
			}
			if doneAt.Before(*doneCutoff) {
				continue
			}
		}
		session, ok := sessions[SessionKey(row.RepoID, row.Key)]
		if !ok {
			session = "NONE"
		}
		out.Cards = append(out.Cards, Card{
			RepoID: row.RepoID, Branch: asTextOr(path(row.Tree, "repo", "branch"), nil), Key: row.Key,
			Title: asTextOr(path(row.Tree, "feature", "title"), nil), Flow: asTextOr(path(row.Tree, "feature", "flow"), nil),
			Owner: ownerLogin, Column: column, Labels: Labels(row.Tree), Eval: EvalSummaryOf(row.Tree),
			SyncedAt: httpx.Instant(row.SyncedAt), ClockSkew: row.ClockSkew, Session: session,
			ApprovalsMode: row.ApprovalsMode, RiskLevel: row.RiskLevel, Attempt: row.Attempt,
		})
		if out.Me != nil && ownerLogin != nil && *out.Me == *ownerLogin {
			for _, i := range Inbox(row.Tree, httpx.LocaleFrom(ctx)) {
				out.Inbox = append(out.Inbox, inboxJSON{row.RepoID, row.Key, i.Kind, i.Text, i.Command})
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func notFound() *httpx.Error {
	return httpx.Err(http.StatusNotFound, httpx.T("Không tìm thấy tính năng"))
}

// detailJSON là GET /api/features/{repo}/{key} (thứ tự trường như FeatureController.java).
type detailJSON struct {
	Snapshot       json.RawMessage     `json:"snapshot"`
	Column         string              `json:"column"`
	Labels         []string            `json:"labels"`
	Owner          *string             `json:"owner"`
	SyncedAt       string              `json:"syncedAt"`
	ClockSkew      bool                `json:"clockSkew"`
	LastSender     string              `json:"lastSender"`
	ApprovalsMode  string              `json:"approvalsMode"`
	SubmittedPaths map[string][]string `json:"submittedPaths"`
	PendingImports []PendingImport     `json:"pendingImports"`
	Attempt        int                 `json:"attempt"`
}

// feature: /api/features/{repoId...}/{key}[/events|/stream|/resubmission|/control]. repoId có dấu '/', nên khớp
// /api/features/* rồi tách: đoạn cuối là key, trừ hậu tố.
func (h *handlers) feature(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := intParam(q, "limit", 200)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	p := r.URL.Path
	if !strings.HasPrefix(p, featuresPrefix) {
		httpx.Fail(w, r, notFound())
		return
	}
	rest := p[len(featuresPrefix):]
	suffix := ""
	for _, s := range []string{"/events", "/stream", "/resubmission", "/control", "/attempts"} {
		if strings.HasSuffix(rest, s) {
			suffix, rest = s, rest[:len(rest)-len(s)]
			break
		}
	}
	slash := strings.LastIndexByte(rest, '/')
	if slash <= 0 || slash == len(rest)-1 {
		httpx.Fail(w, r, notFound())
		return
	}
	repoID, key := rest[:slash], rest[slash+1:]
	ctx := r.Context()
	row, err := h.reader.Find(ctx, repoID, key)
	if err == nil && row == nil {
		err = notFound()
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	switch suffix {
	case "/stream":
		h.hub.ServeFeature(w, r, repoID, key)
		return
	case "/resubmission":
		v, err := h.resubmission(r, repoID, key, textParam(q, "gate"), row.ApprovalsMode)
		writeResult(w, r, v, err)
		return
	case "/control":
		if h.reader.Control == nil {
			httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		v, err := h.reader.Control.View(ctx, repoID, key)
		writeResult(w, r, v, err)
		return
	case "/attempts":
		v, err := h.reader.Attempts(ctx, repoID, key)
		writeResult(w, r, v, err)
		return
	case "/events":
		evs, err := h.reader.Events(ctx, repoID, key, max(1, min(limit, maxEventLimit)))
		writeResult(w, r, evs, err)
		return
	}
	owner, err := h.reader.Owner(ctx, *row)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out := detailJSON{Snapshot: row.Snapshot, Column: Column(row.Tree), Labels: Labels(row.Tree), Owner: owner,
		SyncedAt: httpx.Instant(row.SyncedAt), ClockSkew: row.ClockSkew, LastSender: row.LastSender,
		ApprovalsMode: row.ApprovalsMode, SubmittedPaths: map[string][]string{}, PendingImports: []PendingImport{}, Attempt: row.Attempt}
	// Chế độ web: đường dẫn artifact (không nội dung) của bản nộp mới nhất mỗi cổng, để UI cho thấy đang duyệt gì.
	if row.ApprovalsMode == modeWeb && h.reader.Gates != nil {
		if out.SubmittedPaths, err = h.reader.Gates.SubmittedPaths(ctx, repoID, key); err == nil {
			out.PendingImports, err = h.reader.Gates.PendingImports(ctx, repoID, key)
		}
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if out.SubmittedPaths == nil {
			out.SubmittedPaths = map[string][]string{}
		}
		if out.PendingImports == nil {
			out.PendingImports = []PendingImport{}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func writeResult[T any](w http.ResponseWriter, r *http.Request, v T, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (h *handlers) resubmission(r *http.Request, repoID, key string, gateName *string, mode string) (any, error) {
	g := ""
	if gateName != nil {
		g = *gateName
	}
	switch gate.Gate(g) {
	case gate.BRD, gate.DESIGN, gate.CASES, gate.REVIEW:
	default:
		return nil, httpx.Err(http.StatusBadRequest, httpx.T("Cổng không hợp lệ"))
	}
	if mode != modeWeb || h.reader.Gates == nil {
		return map[string]int{"n": 0}, nil
	}
	return h.reader.Gates.Resubmission(r.Context(), repoID, key, g)
}
