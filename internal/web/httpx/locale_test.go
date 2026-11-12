package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aisdlc/internal/i18n"
	"aisdlc/internal/web/httpx"
)

func TestParseAcceptLanguage(t *testing.T) {
	cases := map[string]i18n.Locale{
		"":                         i18n.VI,
		"en":                       i18n.EN,
		"vi":                       i18n.VI,
		"EN-us":                    i18n.EN,
		"en_GB":                    i18n.EN,
		"vi-VN,vi;q=0.9,en;q=0.8":  i18n.VI,
		"fr-FR,fr;q=0.9,en;q=0.8":  i18n.VI, // chỉ tag đầu tiên quyết định
		"fr, de":                   i18n.VI,
		"vi;q=0.5, en;q=0.9":       i18n.VI, // bỏ qua q
		"en;q=0.1, vi":             i18n.EN,
		"en-US,en;q=0.9":           i18n.EN,
		"*":                        i18n.VI,
		";;;,,":                    i18n.VI,
		"english":                  i18n.VI,
		" en ; q = 0.7 , vi;q=0.6": i18n.EN,
	}
	for h, want := range cases {
		if got := httpx.ParseAcceptLanguage(h); got != want {
			t.Errorf("ParseAcceptLanguage(%q) = %s, muốn %s", h, got, want)
		}
	}
}

func TestLocalizeAndWriteErrorTranslate(t *testing.T) {
	h := httpx.Localize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.Fail(w, r, httpx.Err(400, httpx.Tf("Thiếu %s", "repo")))
	}))
	for lang, want := range map[string]string{"en": `{"error":"Missing repo"}`, "vi": `{"error":"Thiếu repo"}`, "": `{"error":"Thiếu repo"}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		if lang != "" {
			r.Header.Set("Accept-Language", lang)
		}
		h.ServeHTTP(w, r)
		if got := strings.TrimSpace(w.Body.String()); got != want {
			t.Errorf("Accept-Language %q: %s, muốn %s", lang, got, want)
		}
	}
}

func TestErrorKeepsVietnameseMessage(t *testing.T) {
	e := httpx.Err(400, httpx.Tf("Thiếu %s", "repo"))
	if e.Error() != "Thiếu repo" || e.Text.In(i18n.EN) != "Missing repo" {
		t.Fatal(e.Error(), e.Text.In(i18n.EN))
	}
	if p := httpx.Err(400, httpx.Plain("Bad Request")); p.Text.In(i18n.EN) != "Bad Request" || p.Error() != "Bad Request" {
		t.Fatal(p)
	}
	if httpx.LocaleFrom(httptest.NewRequest("GET", "/", nil).Context()) != i18n.VI {
		t.Fatal("thiếu locale trên ctx phải là vi")
	}
}

// ApiError của httpx.go (DecodeJSON, giới hạn body) vẫn là câu tiếng Việt; Fail dịch lúc ghi.
func TestFailTranslatesApiError(t *testing.T) {
	cases := []struct {
		err  error
		lang string
		want string
	}{
		{httpx.TooLargeFor(8 << 20), "en", `{"error":"Payload exceeds 8 MB"}`},
		{httpx.TooLargeFor(1000), "en", `{"error":"Payload exceeds 1000 bytes"}`},
		{httpx.TooLargeFor(8 << 20), "vi", `{"error":"Payload vượt 8 MB"}`},
		{httpx.NewError(400, "JSON không hợp lệ"), "en", `{"error":"Invalid JSON"}`},
		{httpx.NewError(400, "JSON không hợp lệ"), "", `{"error":"JSON không hợp lệ"}`},
		{httpx.NewError(400, "câu ngoài catalog"), "en", `{"error":"câu ngoài catalog"}`},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		if c.lang != "" {
			r = r.WithContext(httpx.WithLocale(r.Context(), httpx.ParseAcceptLanguage(c.lang)))
		}
		httpx.Fail(w, r, c.err)
		if got := strings.TrimSpace(w.Body.String()); got != c.want {
			t.Errorf("%v/%s: %s, muốn %s", c.err, c.lang, got, c.want)
		}
	}
	w := httptest.NewRecorder()
	httpx.Fail(w, httptest.NewRequest("GET", "/", nil), http.ErrBodyNotAllowed)
	if w.Code != 500 || strings.TrimSpace(w.Body.String()) != `{"error":"internal"}` {
		t.Fatal(w.Code, w.Body)
	}
}

// LimitBody từ chối theo Content-Length: câu 413 theo locale của request.
func TestLimitBodyTooLargeLocalized(t *testing.T) {
	h := httpx.Localize(httpx.LimitBody(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	for lang, want := range map[string]string{"en": `{"error":"Payload exceeds 10 bytes"}`, "": `{"error":"Payload vượt 10 byte"}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 50)))
		if lang != "" {
			r.Header.Set("Accept-Language", lang)
		}
		h.ServeHTTP(w, r)
		if w.Code != 413 || strings.TrimSpace(w.Body.String()) != want {
			t.Errorf("%q: %d %s, muốn %s", lang, w.Code, w.Body, want)
		}
	}
}

func TestFailWritesStableCode(t *testing.T) {
	h := httpx.Localize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.Fail(w, r, httpx.ErrCode(409, "SOME_CODE", httpx.Tf("Thiếu %s", "repo")))
	}))
	for lang, want := range map[string]string{
		"en": `{"code":"SOME_CODE","error":"Missing repo"}`,
		"vi": `{"code":"SOME_CODE","error":"Thiếu repo"}`,
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Language", lang)
		h.ServeHTTP(w, r)
		if got := strings.TrimSpace(w.Body.String()); w.Code != 409 || got != want {
			t.Errorf("Accept-Language %q: %d %s, muốn 409 %s", lang, w.Code, got, want)
		}
	}
}
