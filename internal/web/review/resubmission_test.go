package review

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"aisdlc/internal/i18n"
)

// Port ResubmissionServiceTest (10).

const (
	dPath   = "aisdlc/features/PAY-1/design.md"
	sqlPath = "db/V2__daily_limit.sql"
)

var (
	t1  = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2  = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	mid = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
)

const oldDesign = "# Design\nintro\n## API\nGET /x\n## Data\ntable\n### Index\nidx\n## DB\nold\n"

func art(path, text string) StoredArtifact { return StoredArtifact{Path: path, Content: []byte(text)} }

func prevSub(design, sql string) StoredSubmission {
	return StoredSubmission{ID: 1, At: t1, Artifacts: []StoredArtifact{art(dPath, design), art(sqlPath, sql)}}
}

func latestSub(design, sql string) StoredSubmission {
	return StoredSubmission{ID: 2, At: t2, Artifacts: []StoredArtifact{art(dPath, design), art(sqlPath, sql)}}
}

func fb(id int64, gate string, at time.Time, comment string) FeedbackRow {
	return FeedbackRow{ID: id, Gate: gate, Kind: "REJECTED", By: "an", At: at, Comment: comment}
}

func only(t *testing.T, r Result) FeedbackMatch {
	t.Helper()
	if len(r.Feedback) != 1 {
		t.Fatalf("%+v", r.Feedback)
	}
	return r.Feedback[0]
}

func run(comment, newDesign, newSQL string) Result {
	return Compare("DESIGN", 2, prevSub(oldDesign, "s1"), latestSub(newDesign, newSQL),
		[]FeedbackRow{fb(7, "DESIGN", mid, comment)}, i18n.VI)
}

func wantMatch(t *testing.T, m FeedbackMatch, status string, matched ...string) {
	t.Helper()
	if matched == nil {
		matched = []string{}
	}
	if m.Status != status || (len(matched) > 0 || m.Status == "UNMATCHED") && !reflect.DeepEqual(m.Matched, matched) {
		t.Fatalf("got %s %v, want %s %v", m.Status, m.Matched, status, matched)
	}
}

func TestCommentOnSectionThatChanged(t *testing.T) {
	r := run("Mục API thiếu 404", strings.Replace(oldDesign, "GET /x", "GET /x\n404", 1), "s1")
	wantMatch(t, only(t, r), "RELATED_CHANGED", dPath+"#API")
}

func TestCommentOnSectionButOtherSectionChanged(t *testing.T) {
	r := run("Mục API thiếu 404", strings.Replace(oldDesign, "table", "table2", 1), "s1")
	wantMatch(t, only(t, r), "NO_CHANGE_SEEN")
}

func TestCommentNamingChangedFile(t *testing.T) {
	wantMatch(t, only(t, run("xem lại V2__daily_limit.sql", oldDesign, "s2")), "RELATED_CHANGED", sqlPath)
}

func TestCommentNamingNothingIsUnmatched(t *testing.T) {
	wantMatch(t, only(t, run("làm lại đi", oldDesign, "s2")), "UNMATCHED")
}

func TestShortHeadingTitleIsIgnored(t *testing.T) {
	wantMatch(t, only(t, run("DB sai", strings.Replace(oldDesign, "old", "new", 1), "s1")), "UNMATCHED")
}

func TestSectionEndsAtSameOrHigherLevelHeading(t *testing.T) {
	// "Data" gồm cả "### Index"; đổi nội dung Index = đổi Data
	wantMatch(t, only(t, run("Data chưa đủ", strings.Replace(oldDesign, "idx", "idx2", 1), "s1")), "RELATED_CHANGED", dPath+"#Data")
}

func TestFeedbackOutsideRangeOrOtherGateIsDropped(t *testing.T) {
	r := Compare("DESIGN", 2, prevSub(oldDesign, "s1"), latestSub(oldDesign, "s2"), []FeedbackRow{
		fb(1, "DESIGN", t1.Add(-5*time.Second), "API"), fb(2, "BRD", mid, "API"),
		fb(3, "DESIGN", t2.Add(5*time.Second), "API"), fb(4, "DESIGN", mid, "API")}, i18n.VI)
	if len(r.Feedback) != 1 || r.Feedback[0].ID != 4 {
		t.Fatalf("%+v", r.Feedback)
	}
}

func TestFileStatusesAndPatches(t *testing.T) {
	p := StoredSubmission{ID: 1, At: t1, Artifacts: []StoredArtifact{art("a", "1\n"), art("b", "x\n"), art("c", "same\n")}}
	l := StoredSubmission{ID: 2, At: t2, Artifacts: []StoredArtifact{art("a", "2\n"), art("c", "same\n"), art("d", "n\n")}}
	r := Compare("DESIGN", 2, p, l, nil, i18n.VI)
	var got []string
	for _, f := range r.Files {
		got = append(got, f.Path+":"+f.Status)
	}
	if !reflect.DeepEqual(got, []string{"a:CHANGED", "c:SAME", "d:ADDED", "b:REMOVED"}) {
		t.Fatal(got)
	}
	if f := r.Files[0]; f.Added != 1 || f.Removed != 1 || f.Patch == nil || !strings.HasPrefix(*f.Patch, "--- a/a\n+++ b/a\n") {
		t.Fatalf("%+v", f)
	}
	if r.N != 2 || r.PreviousAt != "2026-01-01T00:00:00Z" || r.LatestAt != "2026-01-02T00:00:00Z" || r.Truncated {
		t.Fatalf("%+v", r)
	}
}

func TestBinaryFilesCompareByBytesWithoutPatch(t *testing.T) {
	p := StoredSubmission{ID: 1, At: t1, Artifacts: []StoredArtifact{{"i.png", []byte{0, 1, 2}}, {"j.bin", []byte{0xff, 0xfe}}}}
	l := StoredSubmission{ID: 2, At: t2, Artifacts: []StoredArtifact{{"i.png", []byte{0, 1, 3}}, {"j.bin", []byte{0xff, 0xfe}}}}
	r := Compare("DESIGN", 2, p, l, nil, i18n.VI)
	if len(r.Files) != 2 || r.Files[0].Status != "CHANGED" || r.Files[1].Status != "SAME" {
		t.Fatalf("%+v", r.Files)
	}
	for _, f := range r.Files {
		if f.Patch != nil {
			t.Fatalf("%+v", f)
		}
	}
}

func TestPatchesOver200KbAreTruncated(t *testing.T) {
	var big strings.Builder
	for i := 0; i < 3000; i++ {
		big.WriteString("line number " + strconv.Itoa(i) + " xxxxxxxxxxxxxxxxxxxx\n")
	}
	l := StoredSubmission{ID: 2, At: t2, Artifacts: []StoredArtifact{art("big1", big.String()), art("big2", big.String()), art("big3", big.String())}}
	r := Compare("DESIGN", 2, StoredSubmission{ID: 1, At: t1}, l, nil, i18n.VI)
	if !r.Truncated || r.Files[0].Patch == nil || r.Files[1].Patch != nil || r.Files[2].Added != 3000 {
		t.Fatalf("truncated=%v %v %v %d", r.Truncated, r.Files[0].Patch == nil, r.Files[1].Patch == nil, r.Files[2].Added)
	}
}
