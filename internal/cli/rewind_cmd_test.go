package cli

import (
	"fmt"
	"strings"
	"testing"
)

// seqConsole trả lần lượt các dòng đã gõ.
type seqConsole struct{ lines []string }

func (c *seqConsole) ReadLine(string) (string, bool) {
	if len(c.lines) == 0 {
		return "", false
	}
	l := c.lines[0]
	c.lines = c.lines[1:]
	return l, true
}

func (r *testRepo) backupRefs() string {
	r.t.Helper()
	return strings.TrimSpace(gitOut(r.t, r.root, "for-each-ref", "refs/aisdlc/backup/"))
}

func rewindRepo(t *testing.T) *testRepo {
	r := newTestRepo(t)
	r.s1AwaitingDev()
	r.allStepsAccepted()
	return r
}

func TestRewindCmd_LocalToS2(t *testing.T) {
	r := rewindRepo(t)
	s2 := "internal/limit/service/limit_service.go"
	eq(t, r.runC(typed("PAY-1"), "rewind", "S2"), 0)
	contains(t, r.out, "Sẽ quay PAY-1 về đầu S2", "Đã quay PAY-1 về đầu S2. Tiếp: aisdlc step start S2")
	if r.exists(s2) {
		t.Fatal("file S2 phải bị xoá")
	}
	if !r.exists(s1File) {
		t.Fatal("file S1 phải còn")
	}
	if !strings.Contains(r.backupRefs(), "refs/aisdlc/backup/PAY-1/") {
		t.Fatal("thiếu ref dự phòng")
	}
	eq(t, r.status().Steps["S2"], "NOT_STARTED")
	eq(t, r.status().Steps["S1"], "ACCEPTED")
}

func TestRewindCmd_PickerNumber(t *testing.T) {
	r := rewindRepo(t)
	// mốc: brd, design, cases, S1, S2, review => 5 = S2
	eq(t, r.runC(&seqConsole{lines: []string{"5", "PAY-1"}}, "rewind"), 0)
	contains(t, r.out, "Quay về mốc nào (PAY-1)", "1. Đầu BRD", "Đã quay PAY-1 về đầu S2.")
	eq(t, r.status().Steps["S2"], "NOT_STARTED")
}

func TestRewindCmd_WrongKeyAborts(t *testing.T) {
	r := rewindRepo(t)
	s2 := "internal/limit/service/limit_service.go"
	eq(t, r.runC(typed("SAI-1"), "rewind", "S2"), 3)
	if !r.exists(s2) {
		t.Fatal("không file nào được đổi")
	}
	eq(t, r.backupRefs(), "")
	eq(t, r.status().Steps["S2"], "ACCEPTED")
}

func TestRewindCmd_NoConsole(t *testing.T) {
	r := rewindRepo(t)
	eq(t, r.run("rewind", "S2"), 3)
	contains(t, r.err, noConsoleMsg())
	eq(t, r.backupRefs(), "")
}

func TestRewindCmd_CorruptMarkerFails(t *testing.T) {
	r := rewindRepo(t)
	r.write("aisdlc/features/PAY-1/rewind-pending.json", `{"nope"`)
	eq(t, r.runC(typed("PAY-1"), "rewind", "S2"), 2)
	contains(t, r.err, "rewind-pending.json")
	eq(t, r.status().Steps["S2"], "ACCEPTED")
}

// oldBackup tạo ref dự phòng giả của một lần quay về trước (trỏ tới HEAD).
func (r *testRepo) oldBackup() string {
	r.t.Helper()
	const ref = "refs/aisdlc/backup/PAY-1/old"
	r.gitCmd("update-ref", ref, "HEAD")
	return ref
}

const s2Marker = `{"backupRef":"%s","by":"x",
"at":"2026-01-01T00:00:00Z","plan":{"Target":{"ID":"S2","Gate":"","Step":"S2"},"ClearGates":["REVIEW"],
"DropSteps":["S2"],"ScopeSteps":["S2"]}}`

func TestRewindCmd_ResumesPendingMarker(t *testing.T) {
	r := rewindRepo(t)
	old := r.oldBackup()
	r.write("aisdlc/features/PAY-1/rewind-pending.json", fmt.Sprintf(s2Marker, old))
	eq(t, r.runC(typed("PAY-1"), "rewind", "S2"), 0)
	contains(t, r.out, old, "Bản hiện tại cất ở refs/aisdlc/backup/PAY-1/", "Đã quay PAY-1 về đầu S2.")
	eq(t, strings.Count(r.backupRefs(), "refs/aisdlc/backup/"), 2) // luôn chụp bản dự phòng mới khi chạy tiếp
	if r.exists("aisdlc/features/PAY-1/rewind-pending.json") {
		t.Fatal("dấu dở phải được xoá")
	}
	eq(t, r.status().Steps["S2"], "NOT_STARTED")
}

// Dấu đang dở có ref dự phòng lạ (không thuộc refs/aisdlc/backup/<KEY>/ hoặc không trỏ tới commit): từ chối.
func TestRewindCmd_ResumeRefusesBadBackupRef(t *testing.T) {
	for _, ref := range []string{"refs/heads/main", "refs/aisdlc/backup/PAY-2/x", "refs/aisdlc/backup/PAY-1/missing", ""} {
		t.Run(ref, func(t *testing.T) {
			r := rewindRepo(t)
			r.write("aisdlc/features/PAY-1/rewind-pending.json", fmt.Sprintf(s2Marker, ref))
			eq(t, r.runC(typed("PAY-1"), "rewind", "S2"), 2)
			contains(t, r.err, "rewind-pending.json", "không hợp lệ")
			eq(t, r.backupRefs(), "")
			eq(t, r.status().Steps["S2"], "ACCEPTED")
			if !r.exists("internal/limit/service/limit_service.go") {
				t.Fatal("không file nào được đổi")
			}
		})
	}
}

// Dấu cũ: dev đã sửa tiếp sau khi lần quay về bị đứt; chạy lại phải lập lại phần code và cất bản hiện tại.
func TestRewindCmd_ResumeBacksUpLaterWork(t *testing.T) {
	r := rewindRepo(t)
	old := r.oldBackup()
	r.write("aisdlc/features/PAY-1/rewind-pending.json", fmt.Sprintf(s2Marker, old))
	const f = "internal/limit/service/limit_service.go"
	r.write(f, "// sửa sau khi đứt\n")
	eq(t, r.runC(typed("PAY-1"), "rewind", "S2"), 0)
	var fresh string
	for _, l := range strings.Split(r.backupRefs(), "\n") {
		if ref := l[strings.LastIndex(l, "\t")+1:]; ref != old {
			fresh = ref
		}
	}
	if fresh == "" {
		t.Fatalf("thiếu bản dự phòng mới: %q", r.backupRefs())
	}
	eq(t, gitOut(t, r.root, "show", fresh+":"+f), "// sửa sau khi đứt")
}

func TestRewindCmd_PendingForOtherCheckpointWarns(t *testing.T) {
	r := rewindRepo(t)
	r.write("aisdlc/features/PAY-1/rewind-pending.json", `{"backupRef":"refs/aisdlc/backup/PAY-1/old","by":"x",
"at":"2026-01-01T00:00:00Z","plan":{"Target":{"ID":"S1","Gate":"","Step":"S1"}}}`)
	eq(t, r.runC(typed("PAY-1"), "rewind", "S2"), 0)
	contains(t, r.out, "Cảnh báo: có lần quay về S1 đang dở", "S2")
	if r.backupRefs() == "" {
		t.Fatal("phải tạo bản dự phòng mới")
	}
}

func TestRewindCmd_PreviewListsForeignAndSkipped(t *testing.T) {
	r := rewindRepo(t)
	r.write("pkg/hand.go", "package pkg\n") // đổi ngoài các bước
	eq(t, r.runC(typed("PAY-1"), "rewind", "S2"), 0)
	contains(t, r.out, "Cảnh báo: 1 file đổi ngoài các bước của PAY-1", "pkg/hand.go")
}
