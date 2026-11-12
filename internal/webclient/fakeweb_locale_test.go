package webclient_test

import (
	"context"
	"net/http"
	"testing"

	"aisdlc/internal/i18n"
	"aisdlc/internal/webclient"
	"aisdlc/internal/webclient/fakeweb"
)

// Với fake web server: request của CLI mang Accept-Language theo locale của repo (vi mặc định, en khi repo en).
func TestFakeWebSeesAcceptLanguage(t *testing.T) {
	t.Cleanup(func() { i18n.SetDefault(i18n.VI) })
	web := fakeweb.New(t)
	c := &webclient.Client{BaseURL: web.URL(), Token: "t"}
	for _, l := range []i18n.Locale{i18n.VI, i18n.EN} {
		i18n.SetDefault(l)
		web.Reset()
		if _, err := c.Do(context.Background(), http.MethodGet, "/api/cli/control", nil); err != nil {
			t.Fatal(err)
		}
		reqs := web.Requests()
		if len(reqs) != 1 || reqs[0].AcceptLanguage != string(l) {
			t.Errorf("%s: requests = %+v", l, reqs)
		}
	}
}
