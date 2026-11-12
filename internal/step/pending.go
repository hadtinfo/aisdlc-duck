package step

import (
	"strings"
	"time"

	"aisdlc/internal/i18n"
	"aisdlc/internal/snapshot"
)

const (
	PendingApplied = "APPLIED"
	PendingStale   = "STALE"
)

// PendingResult là kết quả xử lý một lệnh chờ.
type PendingResult struct {
	ID     int64
	Status string
	Reason string // rỗng khi APPLIED (Java: null)
}

// ApplyPending áp dụng các lệnh chưa có trong done; trả kết quả các lệnh vừa xử lý.
func ApplyPending(c snapshot.FeatureControl, steps *Service, extras *ScopeExtras, done map[int64]bool, now time.Time) []PendingResult {
	out := []PendingResult{}
	for _, a := range c.Actions {
		if done[a.ID] {
			continue
		}
		out = append(out, applyOne(a, steps, extras, now))
	}
	return out
}

func applyOne(a snapshot.Action, steps *Service, extras *ScopeExtras, now time.Time) PendingResult {
	if a.ExpiresAt != nil && now.After(*a.ExpiresAt) {
		return stale(a, i18n.T("Lệnh đã hết hạn (7 ngày)"))
	}
	switch a.Kind {
	case "ACCEPT_STEP":
		return accept(a, steps)
	case "ALLOW_SCOPE":
		return allow(a, steps, extras)
	}
	return stale(a, i18n.Tf("Loại lệnh không hỗ trợ: %s", a.Kind))
}

func accept(a snapshot.Action, steps *Service) PendingResult {
	recs, err := steps.All()
	if err != nil {
		return stale(a, err.Error())
	}
	r := find(recs, a.Step)
	if r == nil {
		return stale(a, i18n.Tf("Bước %s chưa được làm", a.Step))
	}
	sameTree := a.EndTree != "" && a.EndTree == r.EndTree
	if r.Status == StepAccepted && sameTree {
		return applied(a)
	}
	if r.Status != StepAwaitingDev {
		return stale(a, i18n.Tf("Bước %s đang ở trạng thái %s, cần AWAITING_DEV", a.Step, string(r.Status)))
	}
	if !sameTree {
		return stale(a, i18n.Tf("Bước %s đã đổi sau khi dev bấm (cây code khác)", a.Step))
	}
	if _, err := steps.Accept(a.Step, a.By+" (web)"); err != nil {
		return stale(a, err.Error())
	}
	return applied(a)
}

func allow(a snapshot.Action, steps *Service, extras *ScopeExtras) PendingResult {
	if !ValidScopePath(a.Path) {
		return stale(a, i18n.Tf("Đường dẫn không hợp lệ: %s", a.Path))
	}
	if _, err := steps.AllowedGlobs(a.Step); err != nil { // lỗi nếu bước không có trong thiết kế
		return stale(a, err.Error())
	}
	if err := extras.Add(a.Path, a.By+" (web)", a.At); err != nil {
		return stale(a, err.Error())
	}
	return applied(a)
}

// ValidScopePath: đường dẫn file cụ thể, chuẩn hóa, tương đối repo (không glob, không ".aisdlc").
// Dùng chung cho máy dev và server web.
func ValidScopePath(p string) bool {
	if strings.TrimSpace(p) == "" || len([]rune(p)) > 500 || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "./") ||
		strings.Contains(p, "\\") {
		return false
	}
	if strings.ContainsAny(p, "*?[]{}!:\x00") {
		return false
	}
	segs := strings.Split(p, "/")
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return segs[0] != ".aisdlc"
}

func applied(a snapshot.Action) PendingResult { return PendingResult{ID: a.ID, Status: PendingApplied} }

func stale(a snapshot.Action, reason string) PendingResult {
	return PendingResult{ID: a.ID, Status: PendingStale, Reason: reason}
}
