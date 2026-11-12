package board_test

import (
	"regexp"
	"strings"
	"testing"

	"aisdlc/internal/web/webtest"
)

// Chữ do server sinh (hộp thư, lỗi API) theo locale của người xem: phiên → users.locale đã chọn, rồi Accept-Language;
// token (CLI/CI) → Accept-Language. Không lẫn tiếng Việt vào luồng tiếng Anh; luồng vi giữ nguyên.

var viChars = regexp.MustCompile(`(?i)[àáạảãâầấậẩẫăằắặẳẵèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]`)

// localeSnap: cổng BRD chờ duyệt, bước S1 chờ accept có case chưa đạt và file ngoài phạm vi, eval trượt, câu hỏi mở.
const localeSnap = `{"schemaVersion":1,"repo":{"id":"r/loc","branch":"feature/LOC-1"},"clientId":"A",
 "capturedAt":"2026-11-05T09:00:00Z","sender":"x","feature":{"key":"LOC-1","title":"Locale","flow":"FULL","owner":"a@corp"},
 "gates":[{"gate":"BRD","state":"AWAITING_APPROVAL"},{"gate":"DESIGN","state":"STALE"}],
 "steps":[{"id":"S1","status":"AWAITING_DEV","record":{"check":{"casesNotPassed":["C1"],"outOfScope":["x.go"]}}}],
 "cases":[],"openQuestions":[{"gate":"BRD","text":"Q1?"}],
 "artifacts":{},"eval":{"passed":false,"hard":[]},"review":null,"truncated":false}`

func inboxTexts(t *testing.T, r webtest.Resp) []string {
	t.Helper()
	var out []string
	for _, i := range at(decode(t, r), "inbox").([]any) {
		out = append(out, at(i, "text").(string))
	}
	if len(out) < 6 {
		t.Fatalf("hộp thư thiếu mục: %v", out)
	}
	return out
}

func assertEnglish(t *testing.T, what string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if viChars.MatchString(s) {
			t.Errorf("%s còn tiếng Việt: %q", what, s)
		}
	}
}

func TestServerTextFollowsViewerLocale(t *testing.T) {
	f := newIT(t)
	if r := f.post(t, "/api/ingest/snapshot", localeSnap, f.tokenA); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	board := func(h map[string]string) webtest.Resp { return f.w.Do("GET", "/api/board", "", h) }
	bearer := map[string]string{"Authorization": "Bearer " + f.tokenA}

	// token, không header → vi như cũ
	vi := inboxTexts(t, board(bearer))
	if !strings.Contains(strings.Join(vi, "\n"), "Cổng BRD chờ bạn duyệt") {
		t.Fatal(vi)
	}
	// token + Accept-Language: en → tiếng Anh, lệnh giữ nguyên
	en := inboxTexts(t, board(map[string]string{"Authorization": "Bearer " + f.tokenA, "Accept-Language": "en"}))
	assertEnglish(t, "hộp thư (token, en)", en...)
	if !strings.Contains(strings.Join(en, "\n"), "Gate BRD is waiting for your approval") ||
		!strings.Contains(strings.Join(en, "\n"), "`aisdlc reopen design`") {
		t.Fatal(en)
	}
	// lỗi API theo Accept-Language
	nf := f.w.Do("GET", "/api/features/r/loc/NOPE", "", map[string]string{"Authorization": "Bearer " + f.tokenA, "Accept-Language": "en-US,en;q=0.9"})
	if nf.Status != 404 || nf.Body != `{"error":"Feature not found"}` {
		t.Fatal(nf.Status, nf.Body)
	}
	if r := f.get("/api/features/r/loc/NOPE", f.tokenA); r.Body != `{"error":"Không tìm thấy tính năng"}` {
		t.Fatal(r.Body)
	}

	// phiên: chưa chọn → Accept-Language; chọn en → en kể cả khi trình duyệt gửi vi
	s := f.w.Session(f.tokenA)
	cookie := "AISDLC_SESSION=" + s.ID
	if me := f.w.GetSession("/api/me", s); me.JSON(t)["locale"] != nil || !strings.Contains(me.Body, `"locale":null`) {
		t.Fatal("chưa chọn phải là null:", me.Body)
	}
	assertEnglish(t, "hộp thư (phiên chưa chọn, Accept-Language en)",
		inboxTexts(t, board(map[string]string{"Cookie": cookie, "Accept-Language": "en"}))...)

	put := func(body string, csrf string) webtest.Resp {
		h := map[string]string{"Cookie": cookie, "Content-Type": "application/json"}
		if csrf != "" {
			h["X-CSRF-Token"] = csrf
		}
		return f.w.Do("PUT", "/api/me/locale", body, h)
	}
	if r := put(`{"locale":"en"}`, ""); r.Status != 403 {
		t.Fatal("thiếu CSRF phải 403:", r.Status, r.Body)
	}
	if r := put(`{"locale":"fr"}`, s.CSRF); r.Status != 400 || r.Body != `{"error":"locale phải là vi hoặc en"}` {
		t.Fatal(r.Status, r.Body)
	}
	if r := put(`{"locale":"en"}`, s.CSRF); r.Status != 200 || r.JSON(t)["locale"] != "en" {
		t.Fatal(r.Status, r.Body)
	}
	if me := f.w.GetSession("/api/me", s); me.JSON(t)["locale"] != "en" {
		t.Fatal(me.Body)
	}
	sessEN := inboxTexts(t, board(map[string]string{"Cookie": cookie, "Accept-Language": "vi"}))
	assertEnglish(t, "hộp thư (phiên locale=en)", sessEN...)
	if r := put(`{"locale":"de"}`, s.CSRF); r.Status != 400 || r.Body != `{"error":"locale must be vi or en"}` {
		t.Fatal("lỗi API theo locale đã chọn:", r.Status, r.Body)
	}
	nf = f.w.Do("GET", "/api/features/r/loc/NOPE", "", map[string]string{"Cookie": cookie})
	if nf.Body != `{"error":"Feature not found"}` {
		t.Fatal(nf.Body)
	}
	// token của cùng user không theo lựa chọn của phiên: chỉ theo Accept-Language
	if got := inboxTexts(t, board(bearer)); strings.Join(got, "\n") != strings.Join(vi, "\n") {
		t.Fatal("token không header phải giữ vi:", got)
	}
	// chọn lại vi
	if r := put(`{"locale":"vi"}`, s.CSRF); r.Status != 200 || r.JSON(t)["locale"] != "vi" {
		t.Fatal(r.Status, r.Body)
	}
	if got := inboxTexts(t, board(map[string]string{"Cookie": cookie, "Accept-Language": "en"})); strings.Join(got, "\n") != strings.Join(vi, "\n") {
		t.Fatal("phiên locale=vi thắng Accept-Language:", got)
	}
}
