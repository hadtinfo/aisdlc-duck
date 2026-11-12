package webclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"aisdlc/internal/gate"
	"aisdlc/internal/i18n"
	"aisdlc/internal/webclient"
)

// Mọi request tới web mang Accept-Language đúng "vi" hoặc "en" theo locale mặc định của tiến trình.
func TestRequestsCarryAcceptLanguage(t *testing.T) {
	t.Cleanup(func() { i18n.SetDefault(i18n.VI) })
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Accept-Language"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &webclient.Client{BaseURL: srv.URL, Token: "t"}
	for _, l := range []i18n.Locale{i18n.VI, i18n.EN} {
		i18n.SetDefault(l)
		if _, err := c.Do(context.Background(), http.MethodGet, "/api/cli/control", nil); err != nil {
			t.Fatal(err)
		}
		if err := c.PostJSON(context.Background(), "/api/ingest/x", map[string]string{"a": "b"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"vi", "vi", "en", "en"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d: Accept-Language = %q, muốn %q", i, got[i], want[i])
		}
	}
}

// feedback.md do gate.Service ghi theo locale của repo: đọc được tiêu đề tiếng Anh lẫn tiếng Việt.
func TestLastFeedbackReadsEnglishHeader(t *testing.T) {
	p := filepath.Join(t.TempDir(), "feedback.md")
	md := "## BRD — trả lại bởi an lúc 2026-11-01T00:00:00Z\n\ncũ\n\n" +
		"## BRD — rejected by Jane Doe at 2026-11-02T00:00:00Z\n\nfix the goal\n\n" +
		"## DESIGN — reopened by bob at 2026-11-03T00:00:00Z\n\n"
	if err := os.WriteFile(p, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	fb, ok, err := webclient.LastFeedback(p, gate.BRD)
	if err != nil || !ok || fb.Kind != "REJECTED" || fb.By != "Jane Doe" || fb.Comment != "fix the goal" {
		t.Errorf("BRD = %+v %v %v", fb, ok, err)
	}
	fb, ok, err = webclient.LastFeedback(p, gate.DESIGN)
	if err != nil || !ok || fb.Kind != "REOPENED" || fb.By != "bob" {
		t.Errorf("DESIGN = %+v %v %v", fb, ok, err)
	}
}
