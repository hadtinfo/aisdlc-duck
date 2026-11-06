// Package spa phục vụ UI React đã build (nhúng trong binary) và index.html cho các đường của SPA
// (port SpaController.java + phục vụ static của Spring).
package spa

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Embedded là handler cho UI nhúng (internal/web/spa/dist; make ui chép bản build thật vào; chưa build thì phục vụ trang placeholder dựng sẵn).
func Embedded() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // dist luôn có trong embed
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return handler(sub, []byte(placeholderHTML)) // chưa build UI (checkout sạch): trang thay thế, go build vẫn chạy
	}
	return Handler(sub)
}

// placeholderHTML được phục vụ khi dist/index.html chưa có (chưa chạy make ui).
const placeholderHTML = `<!doctype html><html lang="vi"><meta charset="utf-8"><title>AISDLC</title><body><div id="root">UI chưa build: chạy make ui</div></body></html>`

// isSPAPath khớp đúng các mẫu của SpaController: "/", "/{path:^(?!api$)[^.]*$}" (một đoạn, không dấu chấm, khác "api"),
// "/f/**" và "/risk/**" (repoId có dấu chấm nên không áp quy tắc không dấu chấm).
func isSPAPath(p string) bool {
	if p == "/" || p == "/f" || p == "/risk" || strings.HasPrefix(p, "/f/") || strings.HasPrefix(p, "/risk/") {
		return true
	}
	seg := strings.TrimPrefix(p, "/")
	return seg != "" && seg != "api" && !strings.ContainsAny(seg, "./")
}

// Handler phục vụ fsys: đường SPA nhận index.html, còn lại là tệp tĩnh có thật; không có thì 404. Chỉ GET/HEAD.
// Không bao giờ nhận /api/** (router đã trả 404 JSON trước khi tới đây).
func Handler(fsys fs.FS) http.Handler { return handler(fsys, nil) }

// handler như Handler; index khác nil là nội dung index.html dự phòng khi fsys không có.
func handler(fsys fs.FS, index []byte) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		p := r.URL.Path
		if p == "/api" || strings.HasPrefix(p, "/api/") {
			http.NotFound(w, r)
			return
		}
		if isSPAPath(p) {
			serveIndex(w, r, fsys, index)
			return
		}
		name := strings.TrimPrefix(path.Clean(p), "/")
		if st, err := fs.Stat(fsys, name); err != nil || st.IsDir() || name == "index.html" {
			if name == "index.html" && (err == nil || index != nil) {
				serveIndex(w, r, fsys, index)
				return
			}
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS, fallback []byte) {
	b, err := fs.ReadFile(fsys, "index.html")
	if err != nil && fallback != nil {
		b, err = fallback, nil
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(b)
}
