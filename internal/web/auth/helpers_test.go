package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"aisdlc/internal/web"
	"aisdlc/internal/web/httpx"
	"aisdlc/internal/web/webtest"
)

// echoPaths là route giả trong mỗi vùng (mọi method → 200 {"login","via"}), để kiểm luật xác thực độc lập với
// các controller của task sau. Đường không có route (vd. /api/cli/x) qua được lọc thì 404 như Java.
var echoPaths = []string{"/api/zz", "/api/ingest/zz", "/api/cli/zz", "/api/ci/zz", "/api/gates/zz", "/api/control/zz"}

func newEnv(t *testing.T) (*webtest.Env, *webtest.Client) {
	t.Helper()
	env := webtest.New(t, func(d web.Deps) http.Handler {
		mux := web.NewHandler(d).(*chi.Mux)
		echo := func(w http.ResponseWriter, r *http.Request) {
			p, _ := httpx.PrincipalFrom(r.Context())
			httpx.WriteJSON(w, 200, map[string]string{"login": p.Login, "via": p.Via})
		}
		for _, p := range echoPaths {
			mux.HandleFunc(p, echo)
		}
		return mux
	})
	return env, env.Client(t)
}

func count(t *testing.T, env *webtest.Env, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := env.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func queryBool(t *testing.T, env *webtest.Env, sql string, args ...any) bool {
	t.Helper()
	var b bool
	if err := env.Pool.QueryRow(context.Background(), sql, args...).Scan(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

func queryStrings(t *testing.T, env *webtest.Env, sql string, args ...any) []string {
	t.Helper()
	rows, err := env.Pool.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

const snap = `{"schemaVersion":1}`

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
