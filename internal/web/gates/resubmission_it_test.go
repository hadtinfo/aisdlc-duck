package gates_test

import (
	"strings"
	"testing"

	"aisdlc/internal/web/webtest"
)

// Port ResubmissionIT (2): GET /api/features/{repo}/{key}/resubmission?gate= (R3).

func (f *gfx) resub(t *testing.T, repo, gate string) webtest.Resp {
	t.Helper()
	r := f.w.GetSession("/api/features/"+repo+"/K-1/resubmission?gate="+gate, f.devS)
	if r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	return r
}

func TestDiffAndFeedbackAfterResubmission(t *testing.T) {
	fx := newGfx(t, "rs")
	f := fx.feat(t, "r/rs-1", "K-1", "FULL")
	f.openGates("BRD")
	f.text["DESIGN"] = "# D\n## API\nGET /x\n## Data\nt\n"
	f.submitOk("DESIGN")
	if r := f.action("reject", fx.ownerS, "DESIGN", str("Mục API thiếu 404")); r.Status != 200 {
		t.Fatal(r.Status, r.Body)
	}
	f.text["DESIGN"] = "# D\n## API\nGET /x\n404\n## Data\nt\n"
	f.submitOk("DESIGN")

	j := fx.resub(t, "r/rs-1", "DESIGN").JSON(t)
	files := j["files"].([]any)
	if j["n"] != 2.0 || j["previousAt"] == "" || len(files) != 1 {
		t.Fatal(j)
	}
	file := files[0].(map[string]any)
	if file["status"] != "CHANGED" || file["added"] != 1.0 || !contains(file["patch"], "+404") {
		t.Fatal(file)
	}
	fb := j["feedback"].([]any)[0].(map[string]any)
	if fb["kind"] != "REJECTED" || fb["by"] != "rs.owner" || fb["status"] != "RELATED_CHANGED" ||
		fb["matched"].([]any)[0] != gpath("DESIGN")+"#API" {
		t.Fatal(fb)
	}
}

func contains(v any, sub string) bool {
	s, ok := v.(string)
	return ok && strings.Contains(s, sub)
}

func TestSingleSubmissionAndLocalAndBadGate(t *testing.T) {
	fx := newGfx(t, "rs")
	f := fx.feat(t, "r/rs-2", "K-1", "FULL")
	f.openGates("BRD")
	f.submitOk("DESIGN")
	if b := fx.resub(t, "r/rs-2", "DESIGN").Body; b != `{"n":1}` {
		t.Fatal(b)
	}
	if b := fx.resub(t, "r/rs-2", "CASES").Body; b != `{"n":0}` {
		t.Fatal(b)
	}
	fx.exec(t, "UPDATE repo_settings SET approvals = 'local' WHERE repo_id = 'r/rs-2'")
	if b := fx.resub(t, "r/rs-2", "DESIGN").Body; b != `{"n":0}` {
		t.Fatal(b)
	}
	if r := fx.w.GetSession("/api/features/r/rs-2/K-1/resubmission?gate=XYZ", fx.devS); r.Status != 400 {
		t.Fatal(r.Status)
	}
	if r := fx.w.GetSession("/api/features/r/rs-2/NOPE/resubmission?gate=DESIGN", fx.devS); r.Status != 404 {
		t.Fatal(r.Status)
	}
}
