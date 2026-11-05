package cli

import (
	"os"
	"path/filepath"
)

// FindRepo trả thư mục cha gần nhất (kể cả chính cwd) chứa aisdlc/config.yaml.
func FindRepo(cwd string) (string, bool) {
	p, err := filepath.Abs(cwd)
	if err != nil {
		p = cwd
	}
	p = filepath.Clean(p)
	for {
		if st, err := os.Stat(filepath.Join(p, "aisdlc", "config.yaml")); err == nil && st.Mode().IsRegular() {
			return p, true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", false
		}
		p = parent
	}
}
