// Package yamlx bọc yaml.v3: đọc khoan dung với khóa lạ, lỗi tiếng Việt kèm số dòng.
package yamlx

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"aisdlc/internal/fsx"
)

var (
	linePrefix = regexp.MustCompile(`line (\d+)`)
	yamlPrefix = regexp.MustCompile(`^yaml: `)
)

// detail chuyển lỗi của yaml.v3 thành chi tiết có "dòng N".
func detail(err error) string {
	s := yamlPrefix.ReplaceAllString(err.Error(), "")
	s = strings.ReplaceAll(s, "unmarshal errors:\n  ", "")
	return linePrefix.ReplaceAllString(s, "dòng $1")
}

// Parse giải mã YAML vào out; khóa lạ bị bỏ qua.
func Parse(data []byte, out any) error {
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("YAML không hợp lệ: %s", detail(err))
	}
	return nil
}

// Read đọc file YAML vào out.
func Read(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Không đọc được %s: %v", path, err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("Không đọc được %s: %s", path, detail(err))
	}
	return nil
}

// Dump mã hóa v thành YAML thụt 2 khoảng trắng, không có dấu "---".
func Dump(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("YAML không hợp lệ: %v", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("YAML không hợp lệ: %v", err)
	}
	return buf.Bytes(), nil
}

// Write ghi v ra path kiểu atomic, tạo thư mục cha nếu cần.
func Write(path string, v any) error {
	data, err := Dump(v)
	if err != nil {
		return fmt.Errorf("Không ghi được %s: %v", path, err)
	}
	// fsx.WriteAtomic đã tạo thư mục cha và báo "Không ghi được <path>: ...".
	return fsx.WriteAtomic(path, data, 0o644)
}
