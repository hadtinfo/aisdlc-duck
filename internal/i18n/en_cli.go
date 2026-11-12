package i18n

// enCLI là bản tiếng Anh của khu vực CLI và hook (đợt 2): internal/cli, policy, gate, step, spec, model, snapshot,
// gitx, yamlx, fsx và cmd/aisdlc. Quy tắc như en.go: khóa là chuỗi tiếng Việt nguyên văn, cùng dãy động từ %.
var enCLI = map[string]string{
	// fsx, yamlx, gitx, model, snapshot
	"Không ghi được %s: %w":                           "Cannot write %s: %w",
	"Không ghi được %s: %v":                           "Cannot write %s: %v",
	"Không đọc được %s: %v":                           "Cannot read %s: %v",
	"Không đọc được %s: %s":                           "Cannot read %s: %s",
	"YAML không hợp lệ: %s":                           "Invalid YAML: %s",
	"YAML không hợp lệ: %v":                           "Invalid YAML: %v",
	"dòng $1":                                         "line $1",
	"dòng %d: %v":                                     "line %d: %v",
	"dòng %d: khóa của mapping phải là giá trị đơn":   "line %d: mapping keys must be scalar values",
	"Đầu ra git diff --numstat không hợp lệ: %s":      "Invalid git diff --numstat output: %s",
	"Đầu ra git diff --name-status không hợp lệ: %s":  "Invalid git diff --name-status output: %s",
	"Không tạo được file index tạm: %s":               "Cannot create the temporary index file: %s",
	"git %s thất bại (%d): %s":                        "git %s failed (%d): %s",
	"Không chạy được git: %s":                         "Cannot run git: %s",
	"Giá trị %s không hợp lệ: %v":                     "Invalid value for %s: %v",
	"Giá trị %s không hợp lệ: %v (phải lớn hơn 0)":    "Invalid value for %s: %v (must be greater than 0)",
	"Feature key không hợp lệ: %s (ví dụ: PAY-123)":   "Invalid feature key: %s (example: PAY-123)",
	"Flow không hợp lệ: %q (chỉ nhận FULL, LITE)":     "Invalid flow: %q (only FULL, LITE are accepted)",
	"không đọc được aisdlc/config.yaml ở %s":          "cannot read aisdlc/config.yaml at %s",
	"chưa bật ở %s":                                   "not enabled at %s",
	"Không serialize được trạng thái cổng: %v":        "Cannot serialize the gate state: %v",
	"Trạng thái cổng hỏng: %v":                        "Corrupt gate state: %v",
	"Trạng thái cổng hỏng: rỗng":                      "Corrupt gate state: empty",
	"Không serialize được trạng thái điều khiển: %v":  "Cannot serialize the control state: %v",
	"Trạng thái điều khiển hỏng: %v":                  "Corrupt control state: %v",
	"Trạng thái điều khiển rỗng":                      "Control state is empty",
	"Khóa công khai không hợp lệ: %v":                 "Invalid public key: %v",
	"Khóa công khai không hợp lệ: không phải Ed25519": "Invalid public key: not Ed25519",
	"theo %s": "per %s",
}

func init() { register(enCLI) }
