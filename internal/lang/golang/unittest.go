package golang

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"aisdlc/internal/i18n"
	"aisdlc/internal/lang"
	"aisdlc/internal/model"
)

type testEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
}

// UnitTests chạy go test -json với coverprofile gộp cho cả module.
func (Runtime) UnitTests(ctx context.Context, repoRoot string, ut model.UnitTestConfig, outDir string) (lang.UnitResult, error) {
	mod, err := modulePath(repoRoot)
	if err != nil {
		return lang.UnitResult{Exit: 1, Output: err.Error()}, nil
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return lang.UnitResult{}, fmt.Errorf(i18n.T("không tìm thấy go trên PATH: %w"), err)
	}
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return lang.UnitResult{}, err
	}
	if err := os.MkdirAll(absOut, 0o755); err != nil {
		return lang.UnitResult{}, err
	}
	profile := filepath.Join(absOut, "unit.cov")
	args := []string{"test", "-json", "-cover", "-covermode=atomic", "-coverpkg=" + mod + "/...", "-coverprofile=" + profile}
	args = append(args, ut.Flags...)
	args = append(args, ut.Packages...)
	cmd := exec.CommandContext(ctx, goBin, args...)
	cmd.Dir = repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	exit := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if ctx.Err() != nil {
			return lang.UnitResult{}, ctx.Err()
		}
		if !errors.As(err, &ee) {
			return lang.UnitResult{}, err
		}
		exit = ee.ExitCode()
		if exit == 0 {
			exit = 1
		}
	}

	var text strings.Builder
	failedTests := map[string]bool{}
	pkgHasFailedTest := map[string]bool{}
	failedPkgs := map[string]bool{}
	sc := bufio.NewScanner(&stdout)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		var ev testEvent
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &ev) != nil {
			text.Write(line)
			text.WriteByte('\n')
			continue
		}
		switch ev.Action {
		case "output":
			text.WriteString(ev.Output)
		case "fail":
			if ev.Test != "" {
				failedTests[ev.Package+"."+ev.Test] = true
				pkgHasFailedTest[ev.Package] = true
			} else if ev.Package != "" {
				failedPkgs[ev.Package] = true
			}
		}
	}
	text.WriteString(stderr.String())

	failed := []string{}
	for k := range failedTests {
		failed = append(failed, k)
	}
	for p := range failedPkgs {
		if !pkgHasFailedTest[p] {
			failed = append(failed, p)
		}
	}
	sort.Strings(failed)
	res := lang.UnitResult{Exit: exit, Failed: failed, Output: text.String()}
	if st, err := os.Stat(profile); err == nil && !st.IsDir() {
		res.Profile = profile
	}
	return res, nil
}
