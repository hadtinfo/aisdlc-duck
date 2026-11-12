package model

import (
	"os"
	"path/filepath"
	"sort"
)

// DbDraftFiles là các file migration nháp trong dir (db/ của tính năng): chỉ cấp trên cùng, symlink tính theo đích
// (đích là file thường thì có), sắp theo tên. Dùng chung cho hash cổng DESIGN và đầu vào xếp loại rủi ro để hai bên
// luôn chọn cùng một tập file. Không đọc được thư mục (vd. chưa có) thì nil.
func DbDraftFiles(dir string) []string {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range es {
		st, err := os.Stat(filepath.Join(dir, e.Name()))
		if err == nil && st.Mode().IsRegular() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = filepath.Join(dir, n)
	}
	return out
}
