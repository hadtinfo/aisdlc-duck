package gates

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/spec"
	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/javaish"
)

// Đọc/kiểm body JSON của các API cổng (GateRequests.java); sai thì trả *httpx.ApiError 400/413.

// MaxBytes là giới hạn body của API cổng (nộp kèm toàn văn artifact).
const MaxBytes = 8 << 20

// tooLarge là lỗi 413 của API cổng.
func tooLarge() *httpx.ApiError {
	return httpx.NewError(http.StatusRequestEntityTooLarge, "Payload quá lớn")
}

func bad(msg string) error { return httpx.NewError(http.StatusBadRequest, msg) }

// body đọc body thành object JSON (số giữ dạng json.Number).
func body(r *http.Request) (map[string]any, error) {
	b, err := httpx.ReadBody(r)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, bad("Body phải là object JSON") // Jackson readTree(rỗng) = MissingNode
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, bad("JSON không hợp lệ")
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, bad("Body phải là object JSON")
	}
	return m, nil
}

// isBlank như String.isBlank (Character.isWhitespace).
func isBlank(s string) bool { return javaish.IsBlank(s) }

// text là chuỗi bắt buộc: khác rỗng, không chỉ khoảng trắng, không NUL (GateRequests.text).
func text(b map[string]any, field string) (string, error) {
	s, ok := b[field].(string)
	if !ok || isBlank(s) {
		return "", bad("Thiếu " + field)
	}
	return noNul(s, field)
}

// optText: thiếu/null → nil; có thì phải là chuỗi, không NUL.
func optText(b map[string]any, field string) (*string, error) {
	v, ok := b[field]
	if !ok || v == nil {
		return nil, nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, bad(field + " phải là chuỗi")
	}
	if _, err := noNul(s, field); err != nil {
		return nil, err
	}
	return &s, nil
}

// param là tham số query bắt buộc, khác rỗng, không NUL.
func param(v, field string) (string, error) {
	if isBlank(v) {
		return "", bad("Thiếu " + field)
	}
	return noNul(v, field)
}

func noNul(s, field string) (string, error) {
	if strings.ContainsRune(s, 0) {
		return "", bad(field + " chứa ký tự NUL (U+0000)")
	}
	return s, nil
}

var allGates = []gate.Gate{gate.BRD, gate.DESIGN, gate.CASES, gate.REVIEW}

// parseGate như Gate.valueOf.
func parseGate(name string) (gate.Gate, error) {
	for _, g := range allGates {
		if string(g) == name {
			return g, nil
		}
	}
	return "", bad("Cổng không hợp lệ: " + name)
}

// finding lưu trong submissions.findings: message null giữ null như Java.
type finding struct {
	Code    string  `json:"code"`
	Message *string `json:"message"`
}

func findings(v any, present bool) ([]finding, error) {
	out := []finding{}
	if !present || v == nil {
		return out, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, bad("findings phải là mảng")
	}
	for _, e := range arr {
		o, ok := e.(map[string]any)
		if !ok {
			return nil, bad("finding phải là object {code, message}")
		}
		code, ok := o["code"].(string)
		if !ok {
			return nil, bad("finding thiếu code")
		}
		if _, err := noNul(code, "code"); err != nil {
			return nil, err
		}
		f := finding{Code: code}
		if msg, ok := o["message"].(string); ok {
			if _, err := noNul(msg, "message"); err != nil {
				return nil, err
			}
			f.Message = &msg
		}
		out = append(out, f)
	}
	return out, nil
}

// artifact nộp: Content là base64 hoặc nil (file vắng).
type artifact struct {
	Path    string  `json:"path"`
	Content *string `json:"content"`
}

type artifacts struct {
	list     []artifact
	contents []model.Content
}

// hash như Hashing.sha256 của các nội dung theo thứ tự nộp.
func (a artifacts) hash() string { return model.SHA256Contents(a.contents) }

// parseArtifacts: mảng {path, content(base64|null)} bắt buộc, khác rỗng, không trùng đường dẫn.
func parseArtifacts(v any) (artifacts, error) {
	arr, ok := v.([]any)
	if !ok {
		return artifacts{}, bad("Thiếu artifacts (mảng)")
	}
	if len(arr) == 0 {
		return artifacts{}, bad("artifacts rỗng")
	}
	var out artifacts
	seen := map[string]bool{}
	for _, e := range arr {
		o, _ := e.(map[string]any)
		path, err := text(o, "path")
		if err != nil {
			return artifacts{}, err
		}
		if seen[path] {
			return artifacts{}, bad("artifacts trùng đường dẫn: " + path)
		}
		seen[path] = true
		c, present := o["content"]
		a := artifact{Path: path}
		var data []byte
		if present && c != nil {
			s, ok := c.(string)
			if !ok {
				return artifacts{}, bad("content phải là base64 hoặc null")
			}
			if data, err = decodeBase64(s); err != nil {
				return artifacts{}, bad("content không phải base64: " + path)
			}
			a.Content = &s
		}
		out.list = append(out.list, a)
		out.contents = append(out.contents, model.Content{Path: path, Data: data})
	}
	return out, nil
}

// decodeBase64 như Base64.getDecoder().decode: bảng chữ chuẩn, đệm '=' tùy chọn nhưng nếu có phải đúng, không nhận
// xuống dòng. Kết quả luôn khác nil (chuỗi rỗng → nội dung rỗng, khác file vắng).
func decodeBase64(s string) ([]byte, error) {
	if strings.ContainsAny(s, "\r\n") {
		return nil, base64.CorruptInputError(0)
	}
	enc := base64.RawStdEncoding
	if strings.HasSuffix(s, "=") || len(s)%4 == 0 {
		enc = base64.StdEncoding
	}
	b, err := enc.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if b == nil {
		b = []byte{}
	}
	return b, nil
}

// riskLevel: {"risk": {"level"}} tùy chọn (mức trong risk.yaml); sai kiểu → 400.
func riskLevel(v any, present bool) (*string, error) {
	if !present || v == nil {
		return nil, nil
	}
	o, ok := v.(map[string]any)
	if !ok {
		return nil, bad("risk phải là object")
	}
	lv, present := o["level"]
	if !present || lv == nil {
		return nil, nil
	}
	s, ok := lv.(string)
	if !ok || isBlank(s) || utf16Len(s) > 100 || strings.ContainsRune(s, 0) {
		return nil, bad("risk.level phải là chuỗi")
	}
	return &s, nil
}

// toSpec đổi findings đã lưu sang spec.Finding (message null → "").
func toSpec(fs []finding) []spec.Finding {
	out := make([]spec.Finding, 0, len(fs))
	for _, f := range fs {
		m := ""
		if f.Message != nil {
			m = *f.Message
		}
		out = append(out, spec.Finding{Code: f.Code, Message: m})
	}
	return out
}
