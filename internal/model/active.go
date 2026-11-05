package model

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"aisdlc/internal/fsx"
)

const maxSessionName = 100

func readKeyFile(f string) (FeatureKey, bool, error) {
	b, err := os.ReadFile(f)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("Không đọc được %s: %v", f, err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", false, nil
	}
	k, err := ParseFeatureKey(s)
	if err != nil {
		return "", false, err
	}
	return k, true, nil
}

func writeKeyFile(f string, k FeatureKey) error {
	return fsx.WriteAtomic(f, []byte(string(k)+"\n"), 0o644)
}

func activeFile(repoRoot string) string { return filepath.Join(repoRoot, ".aisdlc", "active") }

// ReadActive đọc tính năng đang làm; ok=false khi chưa có.
func ReadActive(repoRoot string) (FeatureKey, bool, error) {
	return readKeyFile(activeFile(repoRoot))
}

// WriteActive ghi tính năng đang làm.
func WriteActive(repoRoot string, k FeatureKey) error {
	return writeKeyFile(activeFile(repoRoot), k)
}

func sessionsDir(repoRoot string) string { return filepath.Join(repoRoot, ".aisdlc", "sessions") }

// SessionFileName giữ [A-Za-z0-9_-], ký tự khác thành "_" (mỗi đơn vị UTF-16 một dấu, như bản Java), cắt 100 ký tự.
func SessionFileName(id string) string {
	var sb strings.Builder
	for _, r := range id {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			sb.WriteRune(r)
			continue
		}
		n := 1
		if r > 0xFFFF && utf8.ValidRune(r) {
			n = 2
		}
		sb.WriteString(strings.Repeat("_", n))
	}
	s := sb.String()
	if len(s) > maxSessionName {
		s = s[:maxSessionName]
	}
	return s
}

// ReadSession đọc tính năng gắn với session.
func ReadSession(repoRoot, id string) (FeatureKey, bool, error) {
	return readKeyFile(filepath.Join(sessionsDir(repoRoot), SessionFileName(id)))
}

// WriteSession gắn session với tính năng.
func WriteSession(repoRoot, id string, k FeatureKey) error {
	return writeKeyFile(filepath.Join(sessionsDir(repoRoot), SessionFileName(id)), k)
}

// AllSessions trả mọi gắn kết, khóa theo tên file đã làm sạch.
func AllSessions(repoRoot string) (map[string]FeatureKey, error) {
	out := map[string]FeatureKey{}
	d := sessionsDir(repoRoot)
	entries, err := os.ReadDir(d)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Không đọc được %s: %v", d, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		k, ok, err := readKeyFile(filepath.Join(d, n))
		if err != nil {
			return nil, err
		}
		if ok {
			out[n] = k
		}
	}
	return out, nil
}
