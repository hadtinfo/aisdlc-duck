package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"aisdlc/internal/gitx"
	"aisdlc/internal/model"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient"
	"aisdlc/internal/webclient/fakeweb"
)

// Nhiều phiên song song: `web flush` do hook mở nhận `--feature` là tính năng của phiên gọi, và làm mới cổng và điều
// khiển của tính năng đó ngoài .aisdlc/active.

// twoSessionWeb: phiên a làm PAY-2, phiên b làm PAY-1 (init sau nên .aisdlc/active = PAY-1); web giả bật, khóa ghim.
func twoSessionWeb(t *testing.T) (*testRepo, *fakeweb.Server, string) {
	r := parallelRepo(t)
	r.initAs("a", "PAY-2")
	r.initAs("b", "PAY-1")
	delete(r.env, "AISDLC_SESSION")
	k, ok, err := model.ReadActive(r.root)
	must(t, err)
	if !ok || k != "PAY-1" {
		t.Fatalf("active = %q, %v; cần PAY-1", k, ok)
	}
	repoID := snapshot.RepoID(context.Background(), gitx.New(r.root), r.root)
	web := fakeweb.New(t)
	web.SetFeature(repoID, "PAY-2")
	home := t.TempDir()
	r.env["HOME"] = home
	r.env["AISDLC_WEB_URL"] = web.URL()
	r.env["AISDLC_WEB_TOKEN"] = "ingest-tok"
	must(t, webclient.Pin(home, web.Pub))
	return r, web, repoID
}

func TestFlushRefreshWritesControlOfSessionFeatureNotActive(t *testing.T) {
	r, web, repoID := twoSessionWeb(t)
	web.Update(func(s *fakeweb.Server) {
		s.Control = snapshot.FeatureControl{Repo: repoID, Feature: "PAY-2", IssuedAt: fakeweb.T, Pause: pauseAn()}
	})
	ctx := func() *Context { return r.context(noConsole(), "", &bytes.Buffer{}, &bytes.Buffer{}) }
	cache := snapshot.ControlCacheFile(r.root, "PAY-2")

	// chỉ active (như trước): PAY-2 không được làm mới
	flushRefresh(ctx(), r.root, "")
	eq(t, exists(cache), false)

	flushRefresh(ctx(), r.root, "PAY-2")
	got := NewWebControl(NewWebCache(ctx(), r.root)).Cached("PAY-2")
	if got == nil || got.Pause == nil || got.Pause.By != "an" {
		t.Fatalf("cache điều khiển PAY-2: %+v", got)
	}
	var features []string
	for _, q := range web.RequestsFor("GET", "/api/cli/control") {
		features = append(features, q.Query)
	}
	contains(t, joinLines(features), "feature=PAY-1", "feature=PAY-2")
}

func TestHookSpawnsFlushWithSessionFeature(t *testing.T) {
	r, _, _ := twoSessionWeb(t)
	r.env["AISDLC_WEB_URL"] = "http://127.0.0.1:1" // web bật nhưng chết: hook chỉ ghi outbox và mở flush
	r.spawned, r.spawnedFeatures = nil, nil
	in := hookJSON(map[string]any{"session_id": "a", "hook_event_name": "PostToolUse", "cwd": r.root},
		"Edit", "file_path", filepath.Join(r.root, "README.md"))
	r.hook("post-tool-use", in)
	eq(t, len(r.spawnedFeatures), 1)
	eq(t, r.spawnedFeatures[0], model.FeatureKey("PAY-2"))
}

func TestFlushFeatureArg(t *testing.T) {
	for _, c := range []struct {
		args []string
		want model.FeatureKey
	}{
		{nil, ""},
		{[]string{"--feature", "PAY-2"}, "PAY-2"},
		{[]string{"--feature=PAY-3"}, "PAY-3"},
		{[]string{"--bogus", "x", "--feature", "PAY-4", "extra"}, "PAY-4"},
		{[]string{"--feature", "../etc"}, ""},
		{[]string{"--feature"}, ""},
	} {
		eq(t, flushFeatureArg(c.args), c.want)
	}
	eq(t, len(webclient.FlushArgs("")), 2)
	eq(t, joinLines(webclient.FlushArgs("PAY-2")), "web\nflush\n--feature\nPAY-2")
}

// `web flush` im lặng và exit 0 với mọi đối số, kể cả cờ lạ.
func TestWebFlushToleratesAnyArgs(t *testing.T) {
	r, _, _ := twoSessionWeb(t)
	r.env["AISDLC_WEB_URL"] = "http://127.0.0.1:1"
	for _, args := range [][]string{{"--feature", "PAY-2"}, {"--bogus"}, {"--feature", "nope!"}, {"-x", "y", "z"},
		{"--help"}} {
		code := r.run(append([]string{"web", "flush"}, args...)...)
		eq(t, code, 0)
		eq(t, r.out, "")
		eq(t, r.err, "")
	}
}

func joinLines(s []string) string {
	var b bytes.Buffer
	for i, x := range s {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(x)
	}
	return b.String()
}
