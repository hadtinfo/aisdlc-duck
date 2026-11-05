package step

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"aisdlc/internal/model"
)

// ScopeEntry là một path ngoài phạm vi mà dev đã cho phép.
type ScopeEntry struct {
	Path     string    `json:"path"`
	Approver string    `json:"approver"`
	At       time.Time `json:"at"`
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

// Add thêm path (bỏ qua nếu đã có).
func (s *ScopeExtras) Add(relPath, approver string, at time.Time) error {
	all, err := s.load()
	if err != nil {
		return err
	}
	for _, e := range all {
		if e.Path == relPath {
			return writeJSON(s.file, all)
		}
	}
	all = append(all, ScopeEntry{relPath, approver, at.UTC()})
	return writeJSON(s.file, all)
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
		return nil, fmt.Errorf("scope-extra.json hỏng: %s: %w", s.file, err)
	}
	if all == nil {
		all = []ScopeEntry{}
	}
	return all, nil
}
