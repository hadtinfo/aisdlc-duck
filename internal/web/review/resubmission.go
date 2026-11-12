package review

import (
	"bytes"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"aisdlc/internal/i18n"
	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/javaish"
)

// MaxPatchChars là tổng độ dài patch tối đa trả về (đơn vị ký tự UTF-16 như String.length của Java); quá thì các patch
// sau bị bỏ (truncated).
const MaxPatchChars = 200 * 1024

// MinTitle: tiêu đề mục ngắn hơn không dùng để gắn góp ý.
const MinTitle = 3

// StoredArtifact là một artifact của lần nộp; Content nil = file vắng.
type StoredArtifact struct {
	Path    string
	Content []byte
}

// StoredSubmission là một lần nộp kèm artifact (đúng thứ tự nộp).
type StoredSubmission struct {
	ID        int64
	At        time.Time
	Artifacts []StoredArtifact
}

// FeedbackRow là một lần trả lại/mở lại cổng.
type FeedbackRow struct {
	ID             int64
	Gate, Kind, By string
	At             time.Time
	Comment        string
}

// FileDiff là một file trong kết quả so; Patch nil với file nhị phân hoặc khi đã vượt MaxPatchChars.
type FileDiff struct {
	Path    string  `json:"path"`
	Status  string  `json:"status"` // ADDED | REMOVED | SAME | CHANGED
	Added   int     `json:"added"`
	Removed int     `json:"removed"`
	Patch   *string `json:"patch"`
}

// FeedbackMatch là một góp ý giữa hai lần nộp và việc nó có được xử lý không.
type FeedbackMatch struct {
	ID      int64    `json:"id"`
	Kind    string   `json:"kind"`
	By      string   `json:"by"`
	At      string   `json:"at"`
	Comment string   `json:"comment"`
	Status  string   `json:"status"` // UNMATCHED | NO_CHANGE_SEEN | RELATED_CHANGED
	Matched []string `json:"matched"`
}

// Result là kết quả GET /resubmission khi có ít nhất hai lần nộp (thứ tự trường như Java).
type Result struct {
	N          int             `json:"n"`
	PreviousAt string          `json:"previousAt"`
	LatestAt   string          `json:"latestAt"`
	Truncated  bool            `json:"truncated,omitempty"`
	Files      []FileDiff      `json:"files"`
	Feedback   []FeedbackMatch `json:"feedback"`
}

type fileish struct {
	path  string
	bytes []byte
	text  *string // nil: nhị phân
}

type section struct{ title, body string }

// Compare so hai lần nộp gần nhất của một cổng và gắn góp ý (của cổng, nằm giữa hai lần nộp) với thay đổi
// (ResubmissionService.compare). Thuần, không DB.
func Compare(gate string, n int, previous, latest StoredSubmission, feedback []FeedbackRow, loc i18n.Locale) Result {
	oldFiles, oldOrder := index(previous)
	newFiles, newOrder := index(latest)
	paths := append([]string{}, newOrder...)
	for _, p := range oldOrder {
		if _, ok := newFiles[p]; !ok {
			paths = append(paths, p)
		}
	}

	out := Result{N: n, PreviousAt: httpx.Instant(previous.At), LatestAt: httpx.Instant(latest.At),
		Files: []FileDiff{}, Feedback: []FeedbackMatch{}}
	status := map[string]string{}
	total := 0
	for _, p := range paths {
		o, c := oldFiles[p], newFiles[p]
		st := "CHANGED"
		switch {
		case o == nil:
			st = "ADDED"
		case c == nil:
			st = "REMOVED"
		case bytes.Equal(o.bytes, c.bytes):
			st = "SAME"
		}
		status[p] = st
		f := FileDiff{Path: p, Status: st}
		if (o == nil || o.text != nil) && (c == nil || c.text != nil) {
			patch := ""
			if st != "SAME" {
				var a, b *string
				if o != nil {
					a = o.text
				}
				if c != nil {
					b = c.text
				}
				patch = Unified(p, a, b, loc)
			}
			cnt := CountLines(patch)
			f.Added, f.Removed = cnt.Added, cnt.Removed
			if l := utf16Len(patch); out.Truncated || total+l > MaxPatchChars {
				out.Truncated = true
			} else {
				total += l
				f.Patch = &patch
			}
		}
		out.Files = append(out.Files, f)
	}

	for _, fb := range feedback {
		if gate != fb.Gate || !fb.At.After(previous.At) || !fb.At.Before(latest.At) {
			continue
		}
		matched := []string{}
		mentioned := match(fb.Comment, oldFiles, oldOrder, newFiles, paths, status, &matched)
		m := FeedbackMatch{ID: fb.ID, Kind: fb.Kind, By: fb.By, At: httpx.Instant(fb.At), Comment: fb.Comment, Matched: matched}
		switch {
		case !mentioned:
			m.Status = "UNMATCHED"
		case len(matched) == 0:
			m.Status = "NO_CHANGE_SEEN"
		default:
			m.Status = "RELATED_CHANGED"
		}
		out.Feedback = append(out.Feedback, m)
	}
	return out
}

// match trả true nếu comment nhắc tới ít nhất một file/mục; matched nhận những cái đã đổi.
func match(comment string, oldFiles map[string]*fileish, oldOrder []string, newFiles map[string]*fileish, paths []string,
	status map[string]string, matched *[]string) bool {
	lc := strings.ToLower(comment)
	mentioned := false
	for _, p := range paths {
		name := p[strings.LastIndexByte(p, '/')+1:]
		if strings.Contains(lc, strings.ToLower(p)) || (name != "" && strings.Contains(lc, strings.ToLower(name))) {
			mentioned = true
			if status[p] != "SAME" {
				*matched = append(*matched, p)
			}
		}
	}
	for _, p := range oldOrder {
		o := oldFiles[p]
		if o.text == nil || !isMarkdown(o.path) {
			continue
		}
		var newSecs []section
		if c := newFiles[o.path]; c != nil && c.text != nil {
			newSecs = sections(*c.text)
		}
		seen := map[string]bool{}
		for _, s := range sections(*o.text) {
			if utf16Len(s.title) < MinTitle || !strings.Contains(lc, strings.ToLower(s.title)) {
				continue
			}
			if seen[s.title] {
				continue
			}
			seen[s.title] = true
			mentioned = true
			same := false
			for _, ns := range newSecs {
				if ns.title == s.title && ns.body == s.body {
					same = true
					break
				}
			}
			if !same {
				*matched = append(*matched, o.path+"#"+s.title)
			}
		}
	}
	return mentioned
}

func isMarkdown(path string) bool {
	l := strings.ToLower(path)
	return strings.HasSuffix(l, ".md") || strings.HasSuffix(l, ".markdown")
}

// sections: mục = từ dòng tiêu đề tới tiêu đề kế tiếp cùng cấp hoặc cao hơn (gồm cả mục con).
func sections(text string) []section {
	lines := Lines(text)
	var out []section
	for i, l := range lines {
		lv := level(l)
		if lv == 0 {
			continue
		}
		j := i + 1
		for j < len(lines) && (level(lines[j]) == 0 || level(lines[j]) > lv) {
			j++
		}
		out = append(out, section{title(l), strings.Join(lines[i:j], "\n")})
	}
	return out
}

func level(line string) int {
	k := 0
	for k < len(line) && line[k] == '#' {
		k++
	}
	if k >= 1 && k <= 6 && k < len(line) && line[k] == ' ' {
		return k
	}
	return 0
}

func title(line string) string {
	t := javaTrim(line[level(line):])
	for strings.HasSuffix(t, "#") {
		t = t[:len(t)-1]
	}
	return javaTrim(t)
}

// index: artifact có nội dung theo đường dẫn (file vắng bị bỏ), kèm thứ tự.
func index(s StoredSubmission) (map[string]*fileish, []string) {
	m := map[string]*fileish{}
	var order []string
	for _, a := range s.Artifacts {
		if a.Content == nil {
			continue
		}
		if _, ok := m[a.Path]; !ok {
			order = append(order, a.Path)
		}
		m[a.Path] = &fileish{path: a.Path, bytes: a.Content, text: decode(a.Content)}
	}
	return m, order
}

// decode: UTF-8 hợp lệ, không có NUL → chuỗi; ngược lại nil (nhị phân).
func decode(b []byte) *string {
	if bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) {
		return nil
	}
	s := string(b)
	return &s
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func javaTrim(s string) string { return javaish.Trim(s) }
