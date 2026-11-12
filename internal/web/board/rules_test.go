package board

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/i18n"
)

// Port BoardRulesTest (16).

func js(t *testing.T, s string) any {
	t.Helper()
	n, err := ParseTree([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

var runnable = regexp.MustCompile(`^aisdlc [a-z]+( [a-z]+)?( [A-Za-z0-9._/@+=:,-]+| '[^']*')?$`)

// commands: mỗi command là một lệnh chạy được; trả danh sách lệnh dạng chuỗi.
func commands(t *testing.T, snap any) string {
	t.Helper()
	var cmds []string
	for _, i := range Inbox(snap, i18n.VI) {
		if !runnable.MatchString(i.Command) {
			t.Fatalf("command không chạy được: %q", i.Command)
		}
		cmds = append(cmds, i.Command)
	}
	return fmt.Sprint(cmds)
}

func has(list []string, s string) bool { return slices.Contains(list, s) }

func TestFullBrdAwaitingApproval(t *testing.T) {
	s := js(t, `{"feature":{"flow":"FULL"},"gates":[{"gate":"BRD","state":"AWAITING_APPROVAL"}]}`)
	if Column(s) != "BRD" || !has(Labels(s), "AWAITING_APPROVAL:BRD") || !strings.Contains(commands(t, s), "aisdlc approve brd") {
		t.Fatal(Column(s), Labels(s), Inbox(s, i18n.VI))
	}
}

func TestFullDesignStale(t *testing.T) {
	s := js(t, `{"feature":{"flow":"FULL"},"gates":[{"gate":"BRD","state":"APPROVED"},{"gate":"DESIGN","state":"STALE"}]}`)
	if Column(s) != "DESIGN" || !has(Labels(s), "STALE:DESIGN") || !strings.Contains(commands(t, s), "aisdlc submit design") {
		t.Fatal(Column(s), Labels(s), Inbox(s, i18n.VI))
	}
	if !slices.ContainsFunc(Inbox(s, i18n.VI), func(i InboxItem) bool { return strings.Contains(i.Text, "`aisdlc reopen design`") }) {
		t.Fatal(Inbox(s, i18n.VI))
	}
}

func TestLiteStartsAtCases(t *testing.T) {
	s := js(t, `{"feature":{"flow":"LITE"},"gates":[{"gate":"CASES","state":"NOT_SUBMITTED"}]}`)
	if Column(s) != "CASES" {
		t.Fatal(Column(s))
	}
}

func TestCodeWhileAnyStepNotAccepted(t *testing.T) {
	s := js(t, `{"feature":{"flow":"LITE"},"gates":[{"gate":"CASES","state":"APPROVED"},{"gate":"REVIEW","state":"NOT_SUBMITTED"}],
	 "steps":[{"id":"S1","status":"ACCEPTED"},{"id":"S2","status":"NOT_STARTED"}]}`)
	if Column(s) != "CODE" {
		t.Fatal(Column(s))
	}
}

func TestReviewWhenAllStepsAccepted(t *testing.T) {
	s := js(t, `{"feature":{"flow":"LITE"},"gates":[{"gate":"CASES","state":"APPROVED"},{"gate":"REVIEW","state":"NOT_SUBMITTED"}],
	 "steps":[{"id":"S1","status":"ACCEPTED"},{"id":"S2","status":"ACCEPTED"}]}`)
	if Column(s) != "REVIEW" {
		t.Fatal(Column(s))
	}
}

func TestDoneWhenReviewApproved(t *testing.T) {
	s := js(t, `{"feature":{"flow":"LITE"},"gates":[{"gate":"CASES","state":"APPROVED"},{"gate":"REVIEW","state":"APPROVED"}],
	 "steps":[{"id":"S1","status":"ACCEPTED"}]}`)
	if Column(s) != "DONE" {
		t.Fatal(Column(s))
	}
}

func TestAwaitingDevStepWithOutOfScope(t *testing.T) {
	s := js(t, `{"feature":{"flow":"LITE"},"gates":[{"gate":"CASES","state":"APPROVED"}],
	 "steps":[{"id":"S2","status":"AWAITING_DEV","record":{"check":{"casesNotPassed":[],"outOfScope":["README2.txt"]}}}]}`)
	if !has(Labels(s), "AWAITING_ACCEPT:S2") {
		t.Fatal(Labels(s))
	}
	commands(t, s)
	items := Inbox(s, i18n.VI)
	var cmds []string
	for _, i := range items {
		cmds = append(cmds, i.Command)
		if strings.HasPrefix(i.Command, "aisdlc step finish") {
			t.Fatal("step finish cần IN_PROGRESS:", i.Command)
		}
	}
	if !has(cmds, "aisdlc step accept S2") || !has(cmds, "aisdlc scope allow README2.txt") {
		t.Fatal(cmds)
	}
	if !slices.ContainsFunc(items, func(i InboxItem) bool {
		return i.Kind == "OUT_OF_SCOPE" && strings.Contains(i.Text, "README2.txt") && strings.Contains(i.Text, "`aisdlc step start S2`")
	}) {
		t.Fatal(items)
	}
}

func TestOutOfScopeOneAllowCommandPerFileQuotedWhenNeeded(t *testing.T) {
	s := js(t, `{"feature":{"flow":"LITE"},"gates":[{"gate":"CASES","state":"APPROVED"}],
	 "steps":[{"id":"S2","status":"AWAITING_DEV","record":{"check":{"casesNotPassed":[],
	   "outOfScope":["docs/a b.txt","src/x/Y.java"]}}}]}`)
	commands(t, s)
	var got []string
	for _, i := range Inbox(s, i18n.VI) {
		if i.Kind == "OUT_OF_SCOPE" {
			got = append(got, i.Command)
		}
	}
	if !slices.Equal(got, []string{"aisdlc scope allow 'docs/a b.txt'", "aisdlc scope allow src/x/Y.java"}) {
		t.Fatal(got)
	}
	if g := shellArg("it's.txt"); g != `'it'\''s.txt'` {
		t.Fatal(g)
	}
}

func TestDoneAtIsReviewApproval(t *testing.T) {
	at, ok := DoneAt(js(t, `{"gates":[{"gate":"CASES","state":"APPROVED","approval":{"at":"2026-01-01T00:00:00Z"}},
	 {"gate":"REVIEW","state":"APPROVED","approval":{"at":"2026-02-01T00:00:00Z"}}]}`))
	if !ok || !at.Equal(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal(at, ok)
	}
	if _, ok := DoneAt(js(t, `{"gates":[{"gate":"REVIEW","state":"APPROVED","approval":null}]}`)); ok {
		t.Fatal("approval null")
	}
	if _, ok := DoneAt(js(t, `{"gates":[{"gate":"REVIEW","approval":{"at":"hôm qua"}}]}`)); ok {
		t.Fatal("at sai")
	}
}

func TestCasesNotPassedOnAwaitingStepSuggestsRestart(t *testing.T) {
	s := js(t, `{"feature":{"flow":"LITE"},"gates":[{"gate":"CASES","state":"APPROVED"}],
	 "steps":[{"id":"S1","status":"AWAITING_DEV","record":{"check":{"casesNotPassed":["C2"],"outOfScope":[]}}}]}`)
	commands(t, s)
	var hit []InboxItem
	for _, i := range Inbox(s, i18n.VI) {
		if i.Kind == "CASES_NOT_PASSED" {
			hit = append(hit, i)
		}
	}
	if len(hit) != 1 || hit[0].Command != "aisdlc step start S1" || !strings.Contains(hit[0].Text, "C2") ||
		!strings.Contains(hit[0].Text, "`aisdlc step finish S1`") {
		t.Fatal(hit)
	}
}

func TestCasesNotPassedAndCodingLabel(t *testing.T) {
	s := js(t, `{"feature":{"flow":"LITE"},"gates":[],
	 "steps":[{"id":"S1","status":"IN_PROGRESS","record":{"check":{"casesNotPassed":["C2"],"outOfScope":[]}}}]}`)
	if !has(Labels(s), "CODING:S1") {
		t.Fatal(Labels(s))
	}
	if !slices.ContainsFunc(Inbox(s, i18n.VI), func(i InboxItem) bool { return i.Kind == "CASES_NOT_PASSED" && strings.Contains(i.Text, "C2") }) {
		t.Fatal(Inbox(s, i18n.VI))
	}
}

func TestRejectedLabelOnNotSubmittedGate(t *testing.T) {
	s := js(t, `{"feature":{"flow":"FULL"},"gates":[{"gate":"BRD","state":"APPROVED"},
	 {"gate":"DESIGN","state":"NOT_SUBMITTED","lastFeedback":{"kind":"REJECTED","by":"x"}},
	 {"gate":"CASES","state":"NOT_SUBMITTED","lastFeedback":{"kind":"REOPENED","by":"x"}}]}`)
	if l := Labels(s); !has(l, "REJECTED:DESIGN") || has(l, "REJECTED:CASES") {
		t.Fatal(l)
	}
}

func TestFindingsAndOpenQuestions(t *testing.T) {
	s := js(t, `{"feature":{"flow":"FULL"},"gates":[{"gate":"BRD","state":"SUBMITTED_WITH_FINDINGS"}],
	 "openQuestions":[{"gate":"BRD","text":"Hạn mức?"}]}`)
	if !has(Labels(s), "FINDINGS:BRD") || !strings.Contains(commands(t, s), "aisdlc submit brd") {
		t.Fatal(Labels(s), Inbox(s, i18n.VI))
	}
}

func TestEvalFailAndSummary(t *testing.T) {
	s := js(t, `{"feature":{"flow":"LITE"},"gates":[],"eval":{"passed":false,"hard":[
	 {"id":"CASES_ALL_PASS","passed":false,"detail":"5/6 case pass"}]}}`)
	if !has(Labels(s), "EVAL_FAIL") || !strings.Contains(commands(t, s), "aisdlc eval") {
		t.Fatal(Labels(s), Inbox(s, i18n.VI))
	}
	e := EvalSummaryOf(s)
	if e == nil || e.Passed || e.CasesPassed == nil || *e.CasesPassed != 5 || e.CasesTotal == nil || *e.CasesTotal != 6 {
		t.Fatalf("%+v", e)
	}
}

func TestEvalSummaryWithoutMatchOrWithoutEval(t *testing.T) {
	if e := EvalSummaryOf(js(t, `{"eval":null}`)); e != nil {
		t.Fatalf("%+v", e)
	}
	e := EvalSummaryOf(js(t, `{"eval":{"passed":true,"hard":[{"id":"CASES_ALL_PASS","detail":"ok"}]}}`))
	if e == nil || !e.Passed || e.CasesPassed != nil || e.CasesTotal != nil {
		t.Fatalf("%+v", e)
	}
}

func TestOwnerFallbacks(t *testing.T) {
	m := map[string]string{"an@corp": "an.nguyen"}
	str := func(s string) *string { return &s }
	cases := []struct {
		snap  string
		first *string
		want  string
	}{
		{`{"feature":{"owner":null}}`, str("an"), "an"},
		{`{"feature":{"owner":"an@corp"}}`, str("bob"), "an.nguyen"},
		{`{"feature":{"owner":"x@corp"}}`, str("bob"), "bob"},
		{`{"feature":{"owner":"x@corp"}}`, nil, "x@corp"},
	}
	for _, c := range cases {
		got := Owner(js(t, c.snap), m, c.first)
		if got == nil || *got != c.want {
			t.Fatalf("%s: %v", c.snap, got)
		}
	}
	if got := Owner(js(t, `{"feature":{}}`), m, nil); got != nil {
		t.Fatal(*got)
	}
}
