package gates

import (
	"encoding/json"
	"testing"
	"time"

	"aisdlc/internal/snapshot"
	"aisdlc/internal/spec"
)

// Port GateOverlayTest (7).

var (
	ovT1 = time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	ovT2 = time.Date(2026, 11, 3, 11, 0, 0, 0, time.UTC)
)

// ovSnap: snapshot FULL, mọi cổng NOT_SUBMITTED (hoặc LOCKED) theo CLI, currentHash hB/hD/hC/hR.
func ovSnap(t *testing.T) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(`{"feature":{"key":"K-1","flow":"FULL"},"eval":null,"steps":[],
	 "gates":[{"gate":"BRD","state":"NOT_SUBMITTED","currentHash":"hB","submission":null,"approval":null},
	          {"gate":"DESIGN","state":"LOCKED_BY_PREVIOUS","currentHash":"hD"},
	          {"gate":"CASES","state":"LOCKED_BY_PREVIOUS","currentHash":"hC"},
	          {"gate":"REVIEW","state":"LOCKED_BY_PREVIOUS","currentHash":"hR",
	           "lastFeedback":{"kind":"REJECTED","by":"cũ","at":"2026-01-01T00:00:00Z","comment":"cũ"}}]}`), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func ovState(subs []snapshot.Sub, apprs []snapshot.Appr, fbs []snapshot.Fb) snapshot.FeatureGateState {
	return snapshot.FeatureGateState{Repo: "r", Feature: "K-1", Mode: "web", IssuedAt: ovT2, Submissions: subs, Approvals: apprs, Feedback: fbs}
}

func ovAppr(gate, hash string) snapshot.Appr {
	return snapshot.Appr{Gate: gate, Hash: hash, Approver: "lead.l", At: ovT1, Channel: "web"}
}

func ovSub(gate, hash string, f ...spec.Finding) snapshot.Sub {
	return snapshot.Sub{Gate: gate, Hash: hash, At: ovT1, Findings: f}
}

func ovGate(t *testing.T, out any, g string) map[string]any {
	t.Helper()
	for _, n := range out.(map[string]any)["gates"].([]any) {
		if m := n.(map[string]any); m["gate"] == g {
			return m
		}
	}
	t.Fatal(g)
	return nil
}

func ovSt(t *testing.T, out any, g string) string {
	t.Helper()
	return ovGate(t, out, g)["state"].(string)
}

func TestApprovedWhenActiveApprovalMatchesCurrentHash(t *testing.T) {
	in := ovSnap(t)
	out := Apply(in, ovState([]snapshot.Sub{ovSub("BRD", "hB")}, []snapshot.Appr{ovAppr("BRD", "hB")}, nil))
	if ovSt(t, out, "BRD") != "APPROVED" {
		t.Fatal(out)
	}
	a := ovGate(t, out, "BRD")["approval"].(map[string]any)
	if a["hash"] != "hB" || a["approver"] != "lead.l" || a["at"] != "2026-11-03T10:00:00Z" {
		t.Fatal(a)
	}
	// cổng kế tiếp mở khóa: không có gì → NOT_SUBMITTED
	if ovSt(t, out, "DESIGN") != "NOT_SUBMITTED" || ovSt(t, out, "CASES") != "LOCKED_BY_PREVIOUS" {
		t.Fatal(out)
	}
	// không sửa snapshot gốc
	if ovSt(t, in, "BRD") != "NOT_SUBMITTED" || out.(map[string]any)["feature"].(map[string]any)["key"] != "K-1" {
		t.Fatal(in)
	}
}

func TestStaleWhenApprovalHashDiffers(t *testing.T) {
	out := Apply(ovSnap(t), ovState([]snapshot.Sub{ovSub("BRD", "cu")}, []snapshot.Appr{ovAppr("BRD", "cu")}, nil))
	if ovSt(t, out, "BRD") != "STALE" || ovGate(t, out, "BRD")["approval"].(map[string]any)["hash"] != "cu" ||
		ovSt(t, out, "DESIGN") != "LOCKED_BY_PREVIOUS" {
		t.Fatal(out)
	}
}

func TestAwaitingApprovalWhenLatestSubmissionMatchesAndNoFindings(t *testing.T) {
	out := Apply(ovSnap(t), ovState([]snapshot.Sub{ovSub("BRD", "x"), ovSub("BRD", "hB")}, nil, nil))
	g := ovGate(t, out, "BRD")
	if g["state"] != "AWAITING_APPROVAL" || g["submission"].(map[string]any)["hash"] != "hB" || g["approval"] != nil {
		t.Fatal(g)
	}
	if v, ok := g["approval"]; !ok || v != nil {
		t.Fatal("approval phải là null")
	}
	if ovSt(t, out, "DESIGN") != "LOCKED_BY_PREVIOUS" {
		t.Fatal(out)
	}
}

func TestSubmittedWithFindings(t *testing.T) {
	out := Apply(ovSnap(t), ovState([]snapshot.Sub{ovSub("BRD", "hB", spec.Finding{Code: "BRD_X", Message: "thiếu mục"})}, nil, nil))
	g := ovGate(t, out, "BRD")
	fs := g["submission"].(map[string]any)["findings"].([]any)
	if g["state"] != "SUBMITTED_WITH_FINDINGS" || fs[0].(map[string]any)["code"] != "BRD_X" {
		t.Fatal(g)
	}
}

func TestNotSubmittedWhenNothingOrSubmissionOutdated(t *testing.T) {
	if s := ovSt(t, Apply(ovSnap(t), ovState(nil, nil, nil)), "BRD"); s != "NOT_SUBMITTED" {
		t.Fatal(s)
	}
	if s := ovSt(t, Apply(ovSnap(t), ovState([]snapshot.Sub{ovSub("BRD", "cu")}, nil, nil)), "BRD"); s != "NOT_SUBMITTED" {
		t.Fatal(s)
	}
}

func TestLockedByPreviousFollowsGateOrderEvenWithLaterApproval(t *testing.T) {
	// DESIGN có approval khớp nhưng BRD chỉ đang chờ duyệt → DESIGN và sau đó khóa
	out := Apply(ovSnap(t), ovState([]snapshot.Sub{ovSub("BRD", "hB"), ovSub("DESIGN", "hD")},
		[]snapshot.Appr{ovAppr("DESIGN", "hD"), ovAppr("CASES", "hC")}, nil))
	for g, want := range map[string]string{"BRD": "AWAITING_APPROVAL", "DESIGN": "LOCKED_BY_PREVIOUS", "CASES": "LOCKED_BY_PREVIOUS",
		"REVIEW": "LOCKED_BY_PREVIOUS"} {
		if s := ovSt(t, out, g); s != want {
			t.Fatal(g, s)
		}
	}
	// chuỗi duyệt đủ → mở tới REVIEW
	out = Apply(ovSnap(t), ovState([]snapshot.Sub{ovSub("REVIEW", "hR")},
		[]snapshot.Appr{ovAppr("BRD", "hB"), ovAppr("DESIGN", "hD"), ovAppr("CASES", "hC")}, nil))
	if ovSt(t, out, "CASES") != "APPROVED" || ovSt(t, out, "REVIEW") != "AWAITING_APPROVAL" {
		t.Fatal(out)
	}
}

func TestLastFeedbackIsLatestServerFeedbackOfGate(t *testing.T) {
	out := Apply(ovSnap(t), ovState(nil, nil, []snapshot.Fb{
		{ID: 1, Gate: "BRD", Kind: "REJECTED", By: "lead.l", At: ovT1, Comment: "thiếu NFR"},
		{ID: 2, Gate: "DESIGN", Kind: "REOPENED", By: "lead.l", At: ovT1, Comment: ""},
		{ID: 3, Gate: "BRD", Kind: "REOPENED", By: "dev.d", At: ovT2, Comment: "sửa lại"}}))
	fb := ovGate(t, out, "BRD")["lastFeedback"].(map[string]any)
	if fb["kind"] != "REOPENED" || fb["by"] != "dev.d" || fb["at"] != "2026-11-03T11:00:00Z" || fb["comment"] != "sửa lại" {
		t.Fatal(fb)
	}
	if ovGate(t, out, "DESIGN")["lastFeedback"].(map[string]any)["kind"] != "REOPENED" {
		t.Fatal(out)
	}
	// cổng không có feedback trên server: bỏ feedback cục bộ cũ
	if v, ok := ovGate(t, out, "REVIEW")["lastFeedback"]; !ok || v != nil {
		t.Fatal(v)
	}
}

// Thêm: nhãn bản tự duyệt / Claude Code và cờ superseded.
func TestApprovalLabelsAndSuperseded(t *testing.T) {
	auto := snapshot.Appr{Gate: "BRD", Hash: "hB", Approver: "auto", At: ovT1, Channel: "auto", Rule: "flow:low"}
	cc := snapshot.Appr{Gate: "DESIGN", Hash: "hD", Approver: "dev.d", At: ovT1, Channel: "claude-code"}
	out := Apply(ovSnap(t), ovState(nil, []snapshot.Appr{auto, cc}, nil))
	a := ovGate(t, out, "BRD")["approval"].(map[string]any)
	if a["rule"] != "flow:low" || a["label"] != "tự duyệt (mức low)" {
		t.Fatal(a)
	}
	if l := ovGate(t, out, "DESIGN")["approval"].(map[string]any)["label"]; l != "dev.d (trong Claude Code)" {
		t.Fatal(l)
	}
	MarkSuperseded(out, map[string]bool{"BRD": true, "CASES": true})
	if _, has := ovGate(t, out, "BRD")["superseded"]; has {
		t.Fatal("BRD còn approval: không đánh dấu")
	}
	if ovGate(t, out, "CASES")["superseded"] != true {
		t.Fatal(out)
	}
}
