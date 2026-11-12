package policy_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

var viChars = regexp.MustCompile(`(?i)[àáạảãâầấậẩẫăằắặẳẵèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]`)

// Lý do nhập lần đầu (lưu tiếng Việt) và lỗi 409/413 theo locale người xem; 413 theo users.locale của phiên.
func TestPolicyTextFollowsViewerLocale(t *testing.T) {
	f := newFixture(t)
	repo := f.repo()
	p := f.seeded(t, repo)
	cookie := "AISDLC_SESSION=" + f.ownerS.ID
	get := func(lang string) []map[string]any {
		h := map[string]string{"Cookie": cookie}
		if lang != "" {
			h["Accept-Language"] = lang
		}
		var vs []map[string]any
		if err := json.Unmarshal([]byte(f.w.Do("GET", "/api/policy/versions?repo="+enc(repo), "", h).Body), &vs); err != nil {
			t.Fatal(err)
		}
		return vs
	}
	if r := get("en")[0]["reason"]; r != "Imported from aisdlc/risk-policy.yaml" {
		t.Fatal(r)
	}
	if r := get("")[0]["reason"]; r != "Nhập từ aisdlc/risk-policy.yaml" {
		t.Fatal("vi giữ nguyên:", r)
	}

	post := func(body string, h map[string]string) (int, string) {
		h["Cookie"], h["X-CSRF-Token"], h["Content-Type"] = cookie, f.ownerS.CSRF, "application/json"
		r := f.w.Do("POST", "/api/policy/publish", body, h)
		return r.Status, r.Body
	}
	b, _ := json.Marshal(map[string]any{"repo": repo, "baseVersion": 1, "reason": "x", "policy": p})
	if s, body := post(string(b), map[string]string{}); s != 201 {
		t.Fatal(s, body)
	}
	if s, body := post(string(b), map[string]string{"Accept-Language": "en"}); s != 409 || viChars.MatchString(body) || !strings.Contains(body, "A newer version exists (v2)") {
		t.Fatal(s, body)
	}

	// 413 theo Content-Length (LimitBodyWith chạy sau xác thực): phiên đã chọn en thắng Accept-Language vi
	put := f.w.Do("PUT", "/api/me/locale", `{"locale":"en"}`, map[string]string{"Cookie": cookie, "X-CSRF-Token": f.ownerS.CSRF, "Content-Type": "application/json"})
	if put.Status != 200 {
		t.Fatal(put.Status, put.Body)
	}
	big := `{"pad":"` + strings.Repeat("x", 300*1024) + `"}`
	if s, body := post(big, map[string]string{"Accept-Language": "vi"}); s != 413 || body != `{"error":"Payload too large"}` {
		t.Fatal(s, body)
	}
}
