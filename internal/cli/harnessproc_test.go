package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"aisdlc/internal/buildinfo"
	"aisdlc/internal/harness/harnessapi"
)

// exeFile tạo file chạy được dir/name.
func exeFile(t *testing.T, dir, name string) string {
	t.Helper()
	must(t, os.MkdirAll(dir, 0o755))
	p := filepath.Join(dir, name)
	must(t, os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755))
	return p
}

type locEnv struct {
	env  map[string]string
	exe  string
	path map[string]string // tên → đường dẫn trên PATH giả
}

func (l locEnv) locate() (string, error) {
	return locateHarness(func(k string) string { return l.env[k] }, func() (string, error) { return l.exe, nil },
		func(n string) (string, error) {
			if p, ok := l.path[n]; ok {
				return p, nil
			}
			return "", exec.ErrNotFound
		})
}

// Tìm binary: cạnh aisdlc (tên trần, tên theo nền tảng của plugin/lib, cả thư mục đích của symlink); PATH chỉ khi
// không có cạnh aisdlc; AISDLC_HARNESS chỉ là núm test/dev và bị bỏ qua khi chạy trong Claude Code (CLAUDECODE).
func TestLocateHarness(t *testing.T) {
	plat := harnessBin + "-" + runtime.GOOS + "-" + runtime.GOARCH
	onPath := exeFile(t, t.TempDir(), harnessBin)

	t.Run("cạnh aisdlc, tên trần", func(t *testing.T) {
		d := t.TempDir()
		want := exeFile(t, d, harnessBin)
		p, err := locEnv{exe: filepath.Join(d, "aisdlc"), path: map[string]string{harnessBin: onPath}}.locate()
		must(t, err)
		eq(t, p, want)
	})
	t.Run("cạnh aisdlc, tên theo nền tảng (plugin/lib)", func(t *testing.T) {
		d := t.TempDir()
		want := exeFile(t, d, plat)
		exeFile(t, d, harnessBin+"-plan9-mips") // nền tảng khác không được chọn
		p, err := locEnv{exe: filepath.Join(d, "aisdlc-"+runtime.GOOS+"-"+runtime.GOARCH), path: map[string]string{harnessBin: onPath}}.locate()
		must(t, err)
		eq(t, p, want)
	})
	t.Run("symlink: tìm ở thư mục đích", func(t *testing.T) {
		lib := t.TempDir()
		real := exeFile(t, lib, "aisdlc-"+runtime.GOOS+"-"+runtime.GOARCH)
		want := exeFile(t, lib, plat)
		bin := t.TempDir()
		link := filepath.Join(bin, "aisdlc")
		must(t, os.Symlink(real, link))
		p, err := locEnv{exe: link, path: map[string]string{harnessBin: onPath}}.locate()
		must(t, err)
		eq(t, p, want)
	})
	t.Run("không có cạnh aisdlc: PATH", func(t *testing.T) {
		p, err := locEnv{exe: filepath.Join(t.TempDir(), "aisdlc"), path: map[string]string{harnessBin: onPath}}.locate()
		must(t, err)
		eq(t, p, onPath)
	})
	t.Run("file không chạy được cạnh aisdlc bị bỏ qua", func(t *testing.T) {
		d := t.TempDir()
		must(t, os.WriteFile(filepath.Join(d, harnessBin), nil, 0o644))
		p, err := locEnv{exe: filepath.Join(d, "aisdlc"), path: map[string]string{harnessBin: onPath}}.locate()
		must(t, err)
		eq(t, p, onPath)
	})
	t.Run("AISDLC_HARNESS ngoài Claude Code", func(t *testing.T) {
		d := t.TempDir()
		exeFile(t, d, harnessBin)
		fake := exeFile(t, t.TempDir(), "gia")
		p, err := locEnv{env: map[string]string{HarnessEnv: fake}, exe: filepath.Join(d, "aisdlc")}.locate()
		must(t, err)
		eq(t, p, fake)
		_, err = locEnv{env: map[string]string{HarnessEnv: fake + "-khong-co"}, exe: filepath.Join(d, "aisdlc")}.locate()
		if err == nil || !strings.Contains(err.Error(), HarnessEnv) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("AISDLC_HARNESS bị bỏ qua trong Claude Code", func(t *testing.T) {
		d := t.TempDir()
		want := exeFile(t, d, harnessBin)
		fake := exeFile(t, t.TempDir(), "gia")
		p, err := locEnv{env: map[string]string{HarnessEnv: fake, "CLAUDECODE": "1"}, exe: filepath.Join(d, "aisdlc")}.locate()
		must(t, err)
		eq(t, p, want)
		// không có cạnh aisdlc, không có trên PATH: vẫn không dùng biến
		_, err = locEnv{env: map[string]string{HarnessEnv: fake, "CLAUDECODE": "1"}, exe: filepath.Join(t.TempDir(), "aisdlc")}.locate()
		if err == nil || !strings.Contains(err.Error(), "không tìm thấy aisdlc-harness: ") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("thiếu hết: lỗi không gợi ý biến môi trường", func(t *testing.T) {
		_, err := locEnv{exe: filepath.Join(t.TempDir(), "aisdlc")}.locate()
		if err == nil || !strings.Contains(err.Error(), "không tìm thấy aisdlc-harness: ") || strings.Contains(err.Error(), HarnessEnv) {
			t.Errorf("err = %v", err)
		}
	})
}

func withCLIVersion(t *testing.T, v string) {
	t.Helper()
	old := cliVersion
	t.Cleanup(func() { cliVersion = old })
	cliVersion = func() string { return v }
}

// aisdlc và aisdlc-harness khác bản build: fail closed, thông báo rõ; không dùng kết quả của harness lệch bản.
func TestHarnessVersionMismatchFailsClosed(t *testing.T) {
	withCLIVersion(t, "aaa111")
	results := `printf '{"run":{"exitCode":0,"cases":[{"id":"C1","step":"S1","outcome":"PASSED"}]},"verify":{"build":{"ok":true}},"version":"%s"}' "$V" > "$OUT/harness-result.json"; ` +
		`printf '{"feature":"DEMO-1","mode":"cases","cases":[{"id":"C1","step":"S1","outcome":"PASSED"}]}' > "$OUT/results.json"`
	for _, hv := range []string{"bbb222", ""} {
		bin := fakeHarness(t, "V="+hv+"; "+results)
		r := sampleRunner(t, context.Background(), bin)
		res, err := r.RunCases("DEMO-1", nil, "cases")
		must(t, err)
		eq(t, res.ExitCode, 2)
		allError(t, res.Cases, "aisdlc-harness khác bản build với aisdlc")
		contains(t, res.Cases[0].Message, "aaa111", "Cài lại plugin")
		if _, err := os.Stat(filepath.Join(r.OutDir("DEMO-1"), "results.json")); err == nil {
			t.Error("results.json của harness lệch bản vẫn còn")
		}
		vr, err := harnessVerifier{root: r.root, find: r.find}.Verify(context.Background(), "DEMO-1", r.OutDir("DEMO-1"))
		must(t, err)
		contains(t, vr.HarnessErr, "aisdlc-harness khác bản build với aisdlc")
		if vr.Run.ExitCode != 2 {
			t.Errorf("vr = %+v", vr)
		}
	}
	for _, c := range []struct{ cli, harness string }{{"aaa111", "aaa111"}, {"aaa111", buildinfo.Dev}, {buildinfo.Dev, "bbb222"}} {
		withCLIVersion(t, c.cli)
		r := sampleRunner(t, context.Background(), fakeHarness(t, "V="+c.harness+"; "+results))
		res, err := r.RunCases("DEMO-1", []string{"S1"}, "cases")
		must(t, err)
		if res.ExitCode != 0 || len(res.Cases) != 1 || res.Cases[0].Outcome != "PASSED" {
			t.Errorf("%+v: res = %+v", c, res)
		}
	}
}

// harness-result.json không có kết quả verify (cũng không panic): HarnessErr không rỗng.
func TestVerifyMissingResultHasHarnessErr(t *testing.T) {
	bin := fakeHarness(t, `printf '{"run":{"exitCode":0}}' > "$OUT/harness-result.json"`)
	r := sampleRunner(t, context.Background(), bin)
	vr, err := harnessVerifier{root: r.root, find: r.find}.Verify(context.Background(), "DEMO-1", r.OutDir("DEMO-1"))
	must(t, err)
	eq(t, vr.HarnessErr, "aisdlc-harness không trả kết quả verify")
	if vr.Run.ExitCode != 2 || vr.Coverage.NA == "" {
		t.Errorf("vr = %+v", vr)
	}
}

// Toàn bộ stdout/stderr của harness vào harness.log (không chỉ 20 dòng cuối) cùng binary đã dùng; thông báo lỗi là
// một dòng, lấy dòng `panic:`/`fatal error:` đầu tiên.
func TestHarnessOutputLoggedAndPanicLineExtracted(t *testing.T) {
	body := `i=1; while [ $i -le 40 ]; do echo "dong $i"; i=$((i+1)); done; ` +
		`echo "panic: kaboom" >&2; echo "" >&2; echo "goroutine 1 [running]:" >&2; echo "panic: lan hai" >&2; ` +
		`i=1; while [ $i -le 30 ]; do echo "khung $i" >&2; i=$((i+1)); done; exit 2`
	bin := fakeHarness(t, body)
	r := sampleRunner(t, context.Background(), bin)
	res, err := r.RunCases("DEMO-1", nil, "cases")
	must(t, err)
	eq(t, res.ExitCode, 2)
	log, err := os.ReadFile(filepath.Join(r.OutDir("DEMO-1"), HarnessLog))
	must(t, err)
	contains(t, string(log), "aisdlc-harness: "+bin, "dong 1\n", "dong 40", "panic: kaboom", "khung 30")

	vr, err := harnessVerifier{root: r.root, find: r.find}.Verify(context.Background(), "DEMO-1", r.OutDir("DEMO-1"))
	must(t, err)
	contains(t, vr.HarnessErr, "aisdlc-harness không ghi kết quả (exit status 2)", "panic: kaboom")
	if strings.Contains(vr.HarnessErr, "\n") || strings.Contains(vr.HarnessErr, "lan hai") || strings.Contains(vr.HarnessErr, "khung") {
		t.Errorf("HarnessErr nhiều dòng hoặc sai dòng: %q", vr.HarnessErr)
	}
	log, err = os.ReadFile(filepath.Join(r.OutDir("DEMO-1"), HarnessLog))
	must(t, err)
	contains(t, string(log), "aisdlc-harness: "+bin, "dong 1\n", "khung 30")
}

// Không có dòng panic/fatal: thông báo một dòng, lấy dòng cuối không rỗng.
func TestHarnessErrorLineFallback(t *testing.T) {
	eq(t, harnessErrorLine("a\n  fatal error: oom  \nb\n"), "fatal error: oom")
	eq(t, harnessErrorLine("x\npanic: p1 [recovered]\npanic: p2\n"), "panic: p1 [recovered]")
	eq(t, harnessErrorLine("x\ny\n\n"), "y")
	eq(t, harnessErrorLine(""), "")
}

// Kết quả harness thật ghi phiên bản build của nó.
func TestResultCarriesVersionField(t *testing.T) {
	out := t.TempDir()
	must(t, harnessapi.WriteResult(out, harnessapi.Result{Run: &harnessapi.RunResult{}, Version: "abc"}))
	r, err := harnessapi.ReadResult(out)
	must(t, err)
	eq(t, r.Version, "abc")
}
