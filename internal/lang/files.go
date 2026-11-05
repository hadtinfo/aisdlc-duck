package lang

import (
	"context"
	"os"
	"path/filepath"

	"aisdlc/internal/gitx"
)

// FileSource: tập file để so (base tree hoặc working tree).
type FileSource interface {
	List(ctx context.Context) ([]string, error)
	Read(ctx context.Context, path string) ([]byte, error)
}

type treeSource struct {
	git     *gitx.Repo
	treeish string
}

// TreeSource đọc file từ một tree/commit của git.
func TreeSource(git *gitx.Repo, treeish string) FileSource {
	return treeSource{git, treeish}
}

func (s treeSource) List(ctx context.Context) ([]string, error) {
	return s.git.ListTree(ctx, s.treeish)
}

func (s treeSource) Read(ctx context.Context, path string) ([]byte, error) {
	content, _, err := s.git.FileAt(ctx, s.treeish, path)
	return []byte(content), err
}

type dirSource struct {
	root  string
	files []string
}

// DirSource đọc các file đã liệt kê từ thư mục root (working tree).
func DirSource(root string, files []string) FileSource {
	return dirSource{root, files}
}

func (s dirSource) List(context.Context) ([]string, error) {
	return append([]string(nil), s.files...), nil
}

func (s dirSource) Read(_ context.Context, path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.root, filepath.FromSlash(path)))
}
