package run

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"aisdlc/internal/fsx"
)

// SchemaSnapshot là ảnh chụp schema: tên bảng (chữ thường) -> các dòng "column:TYPE:NULL|NOT NULL" đã sắp xếp.
type SchemaSnapshot struct {
	Tables map[string][]string `json:"tables"`
}

// NewSchemaSnapshot chuẩn hóa: tên bảng chữ thường, cột sắp xếp.
func NewSchemaSnapshot(tables map[string][]string) SchemaSnapshot {
	norm := make(map[string][]string, len(tables))
	for t, cols := range tables {
		sorted := append([]string{}, cols...)
		sort.Strings(sorted)
		key := strings.ToLower(t)
		norm[key] = append(norm[key], sorted...)
		sort.Strings(norm[key])
	}
	return SchemaSnapshot{Tables: norm}
}

// ReadSchemaSnapshot đọc và chuẩn hóa file schema.json.
func ReadSchemaSnapshot(path string) (SchemaSnapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SchemaSnapshot{}, fmt.Errorf("Không đọc được %s: %v", path, err)
	}
	var s SchemaSnapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return SchemaSnapshot{}, fmt.Errorf("Không đọc được %s: %v", path, err)
	}
	return NewSchemaSnapshot(s.Tables), nil
}

// Write ghi JSON thụt 2 dấu cách (atomic).
func (s SchemaSnapshot) Write(path string) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(NewSchemaSnapshot(s.Tables)); err != nil {
		return fmt.Errorf("Không ghi được %s: %v", path, err)
	}
	return fsx.WriteAtomic(path, bytes.TrimRight(buf.Bytes(), "\n"), 0o644)
}

// Diff trả "+table.col…" cho cột có thật mà expected không có, "-table.col…" cho cột expected có mà thực tế thiếu.
func (s SchemaSnapshot) Diff(expected SchemaSnapshot) []string {
	names := map[string]bool{}
	for t := range s.Tables {
		names[t] = true
	}
	for t := range expected.Tables {
		names[t] = true
	}
	sorted := make([]string, 0, len(names))
	for t := range names {
		sorted = append(sorted, t)
	}
	sort.Strings(sorted)
	out := []string{}
	has := func(list []string, c string) bool {
		for _, x := range list {
			if x == c {
				return true
			}
		}
		return false
	}
	for _, t := range sorted {
		actual, exp := s.Tables[t], expected.Tables[t]
		for _, c := range actual {
			if !has(exp, c) {
				out = append(out, "+"+t+"."+c)
			}
		}
		for _, c := range exp {
			if !has(actual, c) {
				out = append(out, "-"+t+"."+c)
			}
		}
	}
	return out
}
