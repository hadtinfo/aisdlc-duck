package harnesstest

import (
	"os"
	"testing"
)

// Thời hạn Ryuk: đặt mặc định khi chưa có; giá trị dev/CI đặt sẵn (có hay không tiền tố) được giữ.
func TestSetRyukDefaults(t *testing.T) {
	t.Setenv("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT", "")
	t.Setenv("RYUK_CONNECTION_TIMEOUT", "")
	t.Setenv("TESTCONTAINERS_RYUK_RECONNECTION_TIMEOUT", "")
	t.Setenv("RYUK_RECONNECTION_TIMEOUT", "30s")
	setRyukDefaults()
	if got := os.Getenv("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT"); got != ryukConnectionTimeout {
		t.Errorf("connection timeout = %q", got)
	}
	if got := os.Getenv("TESTCONTAINERS_RYUK_RECONNECTION_TIMEOUT"); got != "" {
		t.Errorf("reconnection timeout bị ghi đè dù RYUK_RECONNECTION_TIMEOUT đã đặt: %q", got)
	}
	t.Setenv("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT", "90s")
	setRyukDefaults()
	if got := os.Getenv("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT"); got != "90s" {
		t.Errorf("giá trị đặt sẵn bị ghi đè: %q", got)
	}
}
