package step

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"time"

	"aisdlc/internal/i18n"
	"aisdlc/internal/model"
)

// ScopeEntry là một path ngoài phạm vi mà dev đã cho phép.
type ScopeEntry struct {
	Path     string    `json:"path"`
	Approver string    `json:"approver"`
	At       time.Time `json:"at"`
	Step     string    `json:"step,omitempty"` // bước đang mở lúc cho phép; rỗng nếu không rõ
}

// ScopeExtras là danh sách path ngoài phạm vi mà dev đã cho phép; lưu trong scope-extra.json.
type ScopeExtras struct{ file string }

func NewScopeExtras(p model.FeaturePaths) *ScopeExtras { return &ScopeExtras{file: p.ScopeExtra()} }

// Paths trả các path đã cho phép, theo thứ tự thêm.
func (s *ScopeExtras) Paths() ([]string, error) {
	all, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(all))
	for _, e := range all {
		out = append(out, e.Path)
	}
	return out, nil
}

// Add thêm path (bỏ qua nếu đã có); stepID là bước đang mở, rỗng nếu không có.
func (s *ScopeExtras) Add(relPath, approver, stepID string, at time.Time) error {
	all, err := s.load()
	if err != nil {
		return err
	}
	for _, e := range all {
		if e.Path == relPath {
			return writeJSON(s.file, all)
		}
	}
	all = append(all, ScopeEntry{Path: relPath, Approver: approver, At: at.UTC(), Step: stepID})
	return writeJSON(s.file, all)
}

// RemoveSteps xoá các mục thuộc bước trong ids; giữ mục không gắn bước.
func (s *ScopeExtras) RemoveSteps(ids []string) error {
	all, err := s.load()
	if err != nil {
		return err
	}
	kept := make([]ScopeEntry, 0, len(all))
	for _, e := range all {
		if e.Step != "" && slices.Contains(ids, e.Step) {
			continue
		}
		kept = append(kept, e)
	}
	return writeJSON(s.file, kept)
}

func (s *ScopeExtras) load() ([]ScopeEntry, error) {
	b, err := os.ReadFile(s.file)
	if errors.Is(err, os.ErrNotExist) {
		return []ScopeEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var all []ScopeEntry
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, &corruptError{i18n.Tf("scope-extra.json hỏng: %s", s.file), err}
	}
	if all == nil {
		all = []ScopeEntry{}
	}
	return all, nil
}
