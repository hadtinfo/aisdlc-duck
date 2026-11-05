package fsx

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteAtomicCreatesParents(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b", "f.json")
	if err := WriteAtomic(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "x" {
		t.Fatalf("%q", b)
	}
}

func TestWriteAtomicNoPartialFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	const n = 20
	contents := make([][]byte, n)
	for i := range contents {
		contents[i] = bytes.Repeat([]byte{byte('a' + i)}, 64*1024)
	}
	if err := WriteAtomic(p, contents[0], 0o644); err != nil {
		t.Fatal(err)
	}
	valid := func(b []byte) bool {
		for _, c := range contents {
			if bytes.Equal(b, c) {
				return true
			}
		}
		return false
	}
	stop := make(chan struct{})
	bad := make(chan string, 1)
	var rd sync.WaitGroup
	rd.Add(1)
	go func() {
		defer rd.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, err := os.ReadFile(p)
			if err != nil || !valid(b) {
				select {
				case bad <- "đọc giữa chừng không ra nội dung đầy đủ":
				default:
				}
				return
			}
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for r := 0; r < 5; r++ {
				if err := WriteAtomic(p, contents[i], 0o644); err != nil {
					t.Error(err)
				}
			}
		}(i)
	}
	wg.Wait()
	close(stop)
	rd.Wait()
	select {
	case m := <-bad:
		t.Fatal(m)
	default:
	}
	b, _ := os.ReadFile(p)
	if !valid(b) {
		t.Fatal("nội dung cuối không hợp lệ")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "state.json" {
			t.Errorf("còn file tạm %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("entries = %d", len(entries))
	}
}

func TestWriteAtomicErrorLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	// đích là thư mục: rename thất bại
	p := filepath.Join(dir, "d")
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(p, []byte("x"), 0o644); err == nil {
		t.Fatal("cần lỗi")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("còn file tạm %s", e.Name())
		}
	}
}
