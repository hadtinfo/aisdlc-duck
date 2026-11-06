// Package buildinfo là phiên bản build của binary aisdlc và aisdlc-harness, để CLI kiểm hai binary cùng một bản build.
package buildinfo

import "runtime/debug"

// Dev là phiên bản khi không biết bản build (go test, build không có thông tin git): không kiểm khớp.
const Dev = "dev"

// version đặt lúc build bằng -ldflags "-X aisdlc/internal/buildinfo.version=..." (ưu tiên hơn thông tin git).
var version string

// Version là phiên bản build của binary đang chạy: version (ldflags), không có thì vcs.revision mà go build nhúng
// (thêm "+dirty" khi cây làm việc có thay đổi chưa commit), không có nữa thì Dev.
func Version() string {
	if version != "" {
		return version
	}
	bi, _ := debug.ReadBuildInfo()
	return FromBuildInfo(bi)
}

// FromBuildInfo lấy phiên bản từ thông tin build (nil hoặc không có vcs.revision là Dev).
func FromBuildInfo(bi *debug.BuildInfo) string {
	if bi == nil {
		return Dev
	}
	rev, dirty := "", false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return Dev
	}
	if dirty {
		rev += "+dirty"
	}
	return rev
}

// Compatible: hai binary dùng được với nhau khi cùng phiên bản hoặc một bên là bản Dev. Phiên bản rỗng (binary cũ
// chưa ghi phiên bản) là không khớp.
func Compatible(a, b string) bool {
	return a == Dev || b == Dev || (a != "" && a == b)
}
