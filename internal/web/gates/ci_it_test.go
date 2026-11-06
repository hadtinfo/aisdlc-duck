package gates_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/eval"
	"aisdlc/internal/gate"
	"aisdlc/internal/web/webtest"
)

// Port CiIT (2): GET /api/ci/approvals (Bearer ci): chỉ approval còn hiệu lực, JSON thường. Thêm
// TestCiApprovalsJSONShape: hợp đồng với HttpCiApprovalSource (CLI Java) và eval.CIApprovals (CLI Go, giai đoạn 4b).

func TestCiReturnsLiveApprovalsOnlyNotRevokedNorUnconfirmedImports(t *testing.T) {
	fx := newGfx(t, "ci")
	ci := webtest.CliToken(t, fx.e.Pool, "ci-token", "ci-it")
	f := fx.feat(t, "r/ci-1", "K-1", "FULL")
	f.openGates("BRD", "DESIGN", "CASES")
	if r := f.action("reject", fx.ownerS, "CASES", str("sai case")); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	// CASES nhập từ local, chưa xác nhận
	imp := strings.Replace(f.submissionBody("CASES", "[]"), `"findings"`, `"approver":"ci.owner","at":"2026-11-01T10:00:00Z","findings"`, 1)
	f.submitOk("CASES") // nộp lại sau khi trả lại để có bản nộp mới
	if r := fx.w.PostBearer("/api/cli/import", imp, fx.ingest); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}

	r := fx.w.GetBearer("/api/ci/approvals?repo=r/ci-1&feature=K-1", ci)
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	j := r.JSON(t)
	if j["mode"] != "web" || j["pinned"] != true {
		t.Fatal(j)
	}
	as := j["approvals"].([]any)
	if len(as) != 2 || as[0].(map[string]any)["gate"] != "BRD" || as[1].(map[string]any)["gate"] != "DESIGN" {
		t.Fatal(as)
	}
	a := as[1].(map[string]any)
	if a["hash"] != f.hash("DESIGN") || a["approver"] != "ci.owner" || a["at"] == "" || a["at"] == nil {
		t.Fatal(a)
	}
	if _, ok := a["evaluatedTree"]; !ok {
		t.Fatal("thiếu evaluatedTree", a)
	}
	if _, ok := j["sig"]; ok {
		t.Fatal("CI nhận JSON thường, không ký")
	}

	// repo local: chế độ local, không có approval
	l := fx.w.GetBearer("/api/ci/approvals?repo=r/ci-khong&feature=K-1", ci).JSON(t)
	if l["mode"] != "local" || l["pinned"] != false || len(l["approvals"].([]any)) != 0 {
		t.Fatal(l)
	}
	// lead đã chuyển repo về local: pinned true, mode local
	fx.exec(t, "INSERT INTO repo_settings(repo_id, approvals, set_by) VALUES ('r/ci-lead-local', 'local', 'lead')")
	pl := fx.w.GetBearer("/api/ci/approvals?repo=r/ci-lead-local&feature=K-1", ci).JSON(t)
	if pl["mode"] != "local" || pl["pinned"] != true {
		t.Fatal(pl)
	}
	if r := fx.w.GetBearer("/api/ci/approvals?repo=r/ci-1", ci); r.Status != 400 {
		t.Fatal(r.Status, r.Body)
	}
	if r := fx.w.GetBearer("/api/ci/approvals?repo=r%00&feature=K", ci); r.Status != 400 {
		t.Fatal(r.Status, r.Body)
	}
}

func TestCiOtherTokenKindsAreForbidden(t *testing.T) {
	fx := newGfx(t, "ci")
	path := "/api/ci/approvals?repo=r/ci-1&feature=K-1"
	for i, st := range []int{fx.w.GetBearer(path, fx.ingest).Status, fx.w.GetBearer(path, fx.approveOwner).Status,
		fx.w.GetSession(path, fx.ownerS).Status} {
		if st != 403 {
			t.Fatal(i, st)
		}
	}
	if st := fx.w.GetBearer(path, "khong-co").Status; st != 401 {
		t.Fatal(st)
	}
}

// parseCiApprovals đọc phản hồi y như HttpCiApprovalSource.fetch (CLI Java) và dựng eval.CIApprovals (CLI Go).
func parseCiApprovals(t *testing.T, body []byte) eval.CIApprovals {
	t.Helper()
	var n struct {
		Mode      *string           `json:"mode"`
		Pinned    any               `json:"pinned"`
		Approvals []json.RawMessage `json:"approvals"`
	}
	if err := json.Unmarshal(body, &n); err != nil {
		t.Fatal(err)
	}
	out := eval.CIApprovals{Mode: "local", Approvals: map[gate.Gate]gate.Approval{}, EvaluatedTree: map[gate.Gate]string{}}
	if n.Mode != nil {
		out.Mode = *n.Mode
	}
	if b, ok := n.Pinned.(bool); ok {
		out.Pinned = b
	} else {
		out.Pinned = out.Mode == "web"
	}
	for _, raw := range n.Approvals {
		var a struct {
			Gate, Hash, Approver string
			At, Channel, Rule    *string
			EvaluatedTree        *string `json:"evaluatedTree"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			t.Fatal(err)
		}
		at, err := time.Parse(time.RFC3339Nano, *a.At)
		if err != nil {
			t.Fatal(err)
		}
		rule, channel := "", ""
		if a.Rule != nil {
			rule = *a.Rule
		}
		if a.Channel != nil {
			channel = *a.Channel
		}
		g := gate.Gate(a.Gate)
		out.Approvals[g] = gate.ApprovalFromWeb(g, a.Hash, a.Approver, at, channel, rule)
		if a.EvaluatedTree != nil {
			out.EvaluatedTree[g] = *a.EvaluatedTree
		}
	}
	return out
}

func TestCiApprovalsJSONShape(t *testing.T) {
	fx := newGfx(t, "cs")
	ci := webtest.CliToken(t, fx.e.Pool, "ci-token", "cs-ci")
	f := fx.autoFeat(t, "r/cs-1", "K-1", genericPolicy(t), cards("low", "[]"))
	f.submitOk("BRD") // tự duyệt (channel auto, rule flow:low)
	f.submitOk("DESIGN")
	f.approveOk("DESIGN") // duyệt tay trên web

	r := fx.w.GetBearer("/api/ci/approvals?repo=r/cs-1&feature=K-1", ci)
	if r.Status != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		t.Fatal(r.Status, r.Header, r.Body)
	}
	// thứ tự khóa như LinkedHashMap của CiController; evaluatedTree/rule null vẫn có mặt
	keys := func(raw []byte) []string {
		dec := json.NewDecoder(bytes.NewReader(raw))
		var out []string
		depth := 0
		for {
			tok, err := dec.Token()
			if err != nil {
				return out
			}
			switch v := tok.(type) {
			case json.Delim:
				if v == '{' || v == '[' {
					depth++
				} else {
					depth--
				}
			case string:
				if depth == 1 && dec.More() {
					out = append(out, v)
					var skip json.RawMessage
					_ = dec.Decode(&skip)
				}
			}
		}
	}
	if got := keys([]byte(r.Body)); !reflect.DeepEqual(got, []string{"mode", "pinned", "approvals"}) {
		t.Fatal(got)
	}
	var top struct{ Approvals []json.RawMessage }
	if err := json.Unmarshal([]byte(r.Body), &top); err != nil || len(top.Approvals) != 2 {
		t.Fatal(err, r.Body)
	}
	want := []string{"gate", "hash", "approver", "at", "evaluatedTree", "channel", "rule"}
	for _, a := range top.Approvals {
		if got := keys(a); !reflect.DeepEqual(got, want) {
			t.Fatal(got, string(a))
		}
	}
	var design map[string]any
	_ = json.Unmarshal(top.Approvals[1], &design)
	if design["evaluatedTree"] != nil || design["rule"] != nil || design["channel"] != "web" {
		t.Fatal(design)
	}
	if at := design["at"].(string); !strings.HasSuffix(at, "Z") {
		t.Fatal(at)
	}

	got := parseCiApprovals(t, []byte(r.Body))
	if got.Mode != "web" || !got.Pinned || len(got.EvaluatedTree) != 0 {
		t.Fatalf("%+v", got)
	}
	brd, design2 := got.Approvals[gate.BRD], got.Approvals[gate.DESIGN]
	if !brd.IsAuto() || brd.Rule != "flow:low" || brd.Hash != f.hash("BRD") || brd.At.IsZero() {
		t.Fatalf("%+v", brd)
	}
	if design2.IsAuto() || design2.Approver != "cs.owner" || design2.Channel != "web" || design2.Rule != "" || design2.Hash != f.hash("DESIGN") {
		t.Fatalf("%+v", design2)
	}
}
