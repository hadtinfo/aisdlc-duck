package harnessapi

// Hằng tiếng Việt cũ, chỉ giữ để internal/cli (ngoài đợt 3) còn biên dịch trong lúc các đợt làm song song. Giá trị
// bằng đúng khóa catalog của hàm cùng nghĩa (TestCompatConstsMatchKeys giữ khớp); code mới dùng hàm theo locale:
// NotRunText, NoResultsText, NoRowText, Canceled, PanicPrefixText.
const (
	NotRun          = "case không được chạy (ứng dụng không khởi động được hoặc suite bị dừng)"
	NoResults       = "harness không ghi kết quả"
	NoRow           = "không có kết quả từ harness"
	CanceledMessage = "Harness bị hủy (Ctrl-C), đã dừng"
	PanicPrefix     = "harness lỗi: "
)
