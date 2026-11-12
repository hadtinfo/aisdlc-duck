package i18n

// en là catalog tiếng Anh. Quy tắc: khóa là chuỗi tiếng Việt nguyên văn trong code
// (đối số đầu của T/Tf), và bản dịch phải có đúng dãy động từ % như khóa.
// TestCatalogComplete và TestCatalogNoOrphans giữ hai chiều này khớp.
var en = map[string]string{
	"Giá trị locale không hợp lệ: %q (chỉ nhận vi hoặc en)": "Invalid locale value: %q (only vi or en are accepted)",
	"BRD thiếu mục bắt buộc: \"%s\".":                       "BRD is missing the required section: \"%s\".",
	"Mục \"%s\" của BRD đang trống.":                        "BRD section \"%s\" is empty.",
	"Câu hỏi mở chưa được trả lời: %s":                      "Open question not answered: %s",
	"# Duyệt cổng trên web (docs/user-guide.md mục 6.3); bỏ dòng này để duyệt local (gates.json).\napprovals: web\n": "# Gate approval on the web (docs/user-guide.md section 6.3); remove this line to approve locally (gates.json).\napprovals: web\n",
	"Cảnh báo: aisdlc/config.yaml giữ locale %s hiện có; file mới tạo theo --locale %s.":                             "Warning: aisdlc/config.yaml keeps its existing locale %s; newly created files follow --locale %s.",
}

// register gộp một catalog phụ vào en. Mỗi khu vực (cli, engine, web…) giữ bản dịch trong file en_<khu>.go riêng và
// gọi register trong init(), để các nhánh làm song song không cùng sửa một map. Cùng khóa mà khác bản dịch là lỗi
// lập trình: panic ngay khi nạp gói.
func register(m map[string]string) {
	for k, v := range m {
		if old, ok := en[k]; ok && old != v {
			panic("i18n: conflicting English translations for key " + k)
		}
		en[k] = v
	}
}
