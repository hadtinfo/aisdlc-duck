package i18n

import (
	"strings"
	"sync"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Locale
	}{{"", VI}, {"vi", VI}, {" EN ", EN}}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil || got != c.want {
			t.Errorf("Parse(%q) = %q, %v; muốn %q", c.in, got, err, c.want)
		}
	}
	if _, err := Parse("fr"); err == nil || !strings.Contains(err.Error(), "fr") {
		t.Errorf("Parse(\"fr\") phải lỗi và nêu fr, được %v", err)
	}
}

func TestTranslate(t *testing.T) {
	en["Xin chào %s"] = "Hello %s"
	t.Cleanup(func() { delete(en, "Xin chào %s") })
	if got := EN.Tf("Xin chào %s", "An"); got != "Hello An" {
		t.Errorf("EN.Tf = %q", got)
	}
	if got := VI.Tf("Xin chào %s", "An"); got != "Xin chào An" {
		t.Errorf("VI.Tf = %q", got)
	}
	if got := EN.T("Không có trong catalog"); got != "Không có trong catalog" {
		t.Errorf("EN.T thiếu mục phải trả nguyên khóa, được %q", got)
	}
}

func TestDefaultConcurrent(t *testing.T) {
	t.Cleanup(func() { SetDefault(VI) })
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				SetDefault(EN)
			} else {
				SetDefault(VI)
			}
			if d := Default(); d != EN && d != VI {
				t.Errorf("Default() = %q", d)
			}
		}(i)
	}
	wg.Wait()
}

func TestFromEnv(t *testing.T) {
	get := func(v string, ok bool) func(string) (string, bool) {
		return func(k string) (string, bool) {
			if k != "AISDLC_LOCALE" {
				t.Errorf("đọc sai biến %q", k)
			}
			return v, ok
		}
	}
	if got := FromEnv(get("en", true)); got != EN {
		t.Errorf("en -> %q", got)
	}
	if got := FromEnv(get("xx", true)); got != VI {
		t.Errorf("xx -> %q", got)
	}
	if got := FromEnv(get("", false)); got != VI {
		t.Errorf("thiếu -> %q", got)
	}
}
