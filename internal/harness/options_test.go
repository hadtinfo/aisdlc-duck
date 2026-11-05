package harness

import (
	"reflect"
	"testing"

	"aisdlc/internal/spec"
)

func TestParseModeDefaultsToCases(t *testing.T) {
	for in, want := range map[string]string{"": ModeCases, "  ": ModeCases, "cases": ModeCases,
		"migration-check": ModeMigrationCheck, "cases-with-drafts": ModeCasesWithDrafts} {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v", in, got, err)
		}
	}
}

func TestSelectFiltersByStep(t *testing.T) {
	cases := []spec.CaseSpec{{ID: "C1", Step: "S1"}, {ID: "C2", Step: "S2"}, {ID: "C3", Step: "S3"}}
	steps := SplitSteps(" S1 , S3,")
	var ids []string
	for _, c := range Select(cases, steps) {
		ids = append(ids, c.ID)
	}
	if !reflect.DeepEqual(ids, []string{"C1", "C3"}) {
		t.Errorf("ids = %v", ids)
	}
	if len(Select(cases, nil)) != 3 || len(SplitSteps("")) != 0 {
		t.Error("empty steps must select all")
	}
}

func TestDraftsOnlyInMigrationCheckAndCasesWithDrafts(t *testing.T) {
	if WithDrafts(ModeCases) || !WithDrafts(ModeMigrationCheck) || !WithDrafts(ModeCasesWithDrafts) {
		t.Error("WithDrafts wrong")
	}
}

func TestUnknownModeIsRejected(t *testing.T) {
	_, err := ParseMode("bogus")
	if err == nil || err.Error() != "mode không hợp lệ: bogus (cases, migration-check hoặc cases-with-drafts)" {
		t.Errorf("err = %v", err)
	}
}
