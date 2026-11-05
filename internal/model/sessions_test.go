package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteThenRead(t *testing.T) {
	root := t.TempDir()
	if err := WriteSession(root, "abc", "PAY-1"); err != nil {
		t.Fatal(err)
	}
	k, ok, err := ReadSession(root, "abc")
	if err != nil || !ok || k != "PAY-1" {
		t.Fatalf("k=%q ok=%v err=%v", k, ok, err)
	}
}

func TestTwoSessionsIndependent(t *testing.T) {
	root := t.TempDir()
	WriteSession(root, "s1", "PAY-1")
	WriteSession(root, "s2", "PAY-2")
	k1, _, _ := ReadSession(root, "s1")
	k2, _, _ := ReadSession(root, "s2")
	if k1 != "PAY-1" || k2 != "PAY-2" {
		t.Fatal(k1, k2)
	}
	all, err := AllSessions(root)
	if err != nil || len(all) != 2 || all["s2"] != "PAY-2" {
		t.Fatalf("%v %v", all, err)
	}
}

func TestUnsafeIdStaysInside(t *testing.T) {
	root := t.TempDir()
	WriteSession(root, "../../x/y", "PAY-1")
	WriteSession(root, strings.Repeat("a", 300), "PAY-2")
	entries, err := os.ReadDir(filepath.Join(root, ".aisdlc", "sessions"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("%v %v", entries, err)
	}
	if SessionFileName("../../x/y") != "______x_y" {
		t.Fatal(SessionFileName("../../x/y"))
	}
	if len(SessionFileName(strings.Repeat("a", 300))) != 100 {
		t.Fatal("phải cắt 100")
	}
	if k, ok, _ := ReadSession(root, "../../x/y"); !ok || k != "PAY-1" {
		t.Fatal(k, ok)
	}
	if _, err := os.Stat(filepath.Join(root, "x")); err == nil {
		t.Fatal("ghi ra ngoài")
	}
}

func TestMissingIsEmpty(t *testing.T) {
	root := t.TempDir()
	if _, ok, err := ReadSession(root, "none"); ok || err != nil {
		t.Fatal(ok, err)
	}
	all, err := AllSessions(root)
	if err != nil || len(all) != 0 {
		t.Fatal(all, err)
	}
}

func TestWriteActiveAtomic(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := WriteActive(root, FeatureKey(fmt.Sprintf("PAY-%d", i+1))); err != nil {
				t.Error(err)
			}
		}(i)
	}
	for i := 0; i < 200; i++ {
		if k, ok, err := ReadActive(root); err != nil || (ok && !strings.HasPrefix(string(k), "PAY-")) {
			t.Fatalf("k=%q ok=%v err=%v", k, ok, err)
		}
	}
	wg.Wait()
	if k, ok, err := ReadActive(root); err != nil || !ok || !strings.HasPrefix(string(k), "PAY-") {
		t.Fatalf("k=%q ok=%v err=%v", k, ok, err)
	}
	es, _ := os.ReadDir(filepath.Join(root, ".aisdlc"))
	if len(es) != 1 {
		t.Fatalf("còn file tạm: %v", es)
	}
}
