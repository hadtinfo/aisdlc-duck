package i18n

// en là catalog tiếng Anh. Quy tắc: khóa là chuỗi tiếng Việt nguyên văn trong code
// (đối số đầu của T/Tf), và bản dịch phải có đúng dãy động từ % như khóa.
// TestCatalogComplete và TestCatalogNoOrphans giữ hai chiều này khớp.
var en = map[string]string{
	"Giá trị locale không hợp lệ: %q (chỉ nhận vi hoặc en)": "Invalid locale value: %q (only vi or en are accepted)",
}
