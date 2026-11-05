package gate

// GateStore là nơi lưu nộp/duyệt của cổng.
type GateStore interface {
	// Refresh đọc lại dữ liệu từ nguồn lưu; Service gọi đầu mỗi thao tác công khai.
	Refresh() error
	Submission(Gate) (Submission, bool, error)
	SaveSubmission(Submission) error
	Approval(Gate) (Approval, bool, error)
	SaveApproval(Approval) error
	// Clear xóa cả submission lẫn approval của cổng.
	Clear(Gate) error
}
