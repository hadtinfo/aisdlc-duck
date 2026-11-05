// Package harness là lõi chạy case black-box: đọc cases.yaml, gọi dịch vụ qua HTTP, so kết quả, ghi results.json.
package harness

import (
	"fmt"
	"strings"

	"aisdlc/internal/spec"
)

// Ba chế độ của harness.
const (
	ModeCases           = "cases"
	ModeCasesWithDrafts = "cases-with-drafts"
	ModeMigrationCheck  = "migration-check"

	// NotRun là thông điệp của case không được chạy.
	NotRun = "case không được chạy (ứng dụng không khởi động được hoặc suite bị dừng)"
	// NoResults khi không có results.json (hoặc hỏng).
	NoResults = "harness không ghi kết quả"
	// NoRow khi results.json thiếu dòng của case được chọn.
	NoRow = "không có kết quả từ harness"
)

// ParseMode chuẩn hóa mode: rỗng là cases, giá trị lạ là lỗi.
func ParseMode(s string) (string, error) {
	m := strings.TrimSpace(s)
	switch m {
	case "":
		return ModeCases, nil
	case ModeCases, ModeMigrationCheck, ModeCasesWithDrafts:
		return m, nil
	}
	return "", fmt.Errorf("mode không hợp lệ: %s (cases, migration-check hoặc cases-with-drafts)", m)
}

// WithDrafts cho biết mode có chạy thêm migration nháp không.
func WithDrafts(mode string) bool {
	return mode == ModeMigrationCheck || mode == ModeCasesWithDrafts
}

// SplitSteps tách danh sách bước phân cách bằng dấu phẩy, bỏ khoảng trắng và phần tử rỗng.
func SplitSteps(csv string) []string {
	var out []string
	for _, s := range strings.Split(csv, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Select giữ case thuộc steps; steps rỗng là tất cả.
func Select(cases []spec.CaseSpec, steps []string) []spec.CaseSpec {
	out := []spec.CaseSpec{}
	for _, c := range cases {
		if len(steps) == 0 || contains(steps, c.Step) {
			out = append(out, c)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
