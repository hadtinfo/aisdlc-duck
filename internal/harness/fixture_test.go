package harness

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/harness/harnesstest"
	"aisdlc/internal/lang/golang"
	"aisdlc/internal/model"
)

const feeCases = `cases:
  - id: F1
    step: S1
    kind: happy
    title: Phí 0,15% của 1.000.000 là 1.500
    given: { sql: [], stubs: [] }
    when:  { method: GET, path: "/api/v1/fees?amount=1000000" }
    then:
      status: 200
      body: { fee: 1500 }
  - id: F2
    step: S1
    kind: error
    title: Số tiền âm thì trả 400
    given: { sql: [], stubs: [] }
    when:  { method: GET, path: "/api/v1/fees?amount=-1" }
    then:
      status: 400
      body: { code: VALIDATION_ERROR }
`

// Dịch vụ mẫu e2e (e2e/fixture-service) chạy được qua harness thật.
func TestFixtureServiceFeeCases(t *testing.T) {
	harnesstest.RequireDocker(t)
	root := harnesstest.CopyDir(t, harnesstest.FixtureDir())
	dir := filepath.Join(root, "aisdlc/features/FEE-T")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cases.yaml"), []byte(feeCases), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := model.LoadRepoConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	infra := &recInfra{}
	h := &Harness{Root: root, Config: cfg, Adapter: golang.Adapter{}, Runtime: golang.Runtime{}, Infra: infra, Now: time.Now}
	out := outDir(t)
	vr, err := h.Verify(context.Background(), "FEE-T", out)
	if err != nil {
		t.Fatal(err)
	}
	if !vr.Build.OK || vr.Unit.Exit != 0 || vr.HarnessErr != "" || vr.TimedOut {
		t.Fatalf("vr = %+v\nlog:\n%s", vr, readLog(t, out))
	}
	if got := rows(vr.Run.Cases); !slices.Equal(got, []string{"F1/S1/PASSED", "F2/S1/PASSED"}) {
		t.Errorf("cases = %v\nlog:\n%s", got, readLog(t, out))
	}
	if vr.Coverage.NA != "" {
		t.Fatalf("coverage NA = %q", vr.Coverage.NA)
	}
	const feeGo = "internal/fee/fee.go"
	if !vr.Coverage.Lines[feeGo][feeLine(t, root)] {
		t.Errorf("dòng tính phí chưa được phủ: %v", vr.Coverage.Lines[feeGo])
	}
	infra.requireCleaned(t)
}

func feeLine(t *testing.T, root string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "internal/fee/fee.go"))
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, "amount * 15 / 10000") {
			return i + 1
		}
	}
	t.Fatal("không thấy dòng tính phí")
	return 0
}
