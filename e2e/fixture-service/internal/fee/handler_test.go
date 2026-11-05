package fee

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	for _, c := range []struct {
		url    string
		status int
		body   string
	}{
		{"/api/v1/fees?amount=1000000", 200, `"fee":1500`},
		{"/api/v1/fees?amount=-1", 400, `VALIDATION_ERROR`},
		{"/api/v1/fees?amount=x", 400, `VALIDATION_ERROR`},
	} {
		rec := httptest.NewRecorder()
		Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.url, nil))
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s: %d %s", c.url, rec.Code, rec.Body)
		}
	}
}
