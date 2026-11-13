package snapshot

// Mã lỗi máy đọc trong JSON lỗi của server web ({"error": "<câu theo locale>", "code": "<mã>"}): phần hợp đồng CLI ↔
// web, như các phong bì ký trong gói này. Câu đổi theo Accept-Language, mã thì không.
const (
	// ErrCodeNotWebMode: 409 khi repo không ở chế độ duyệt trên web.
	ErrCodeNotWebMode = "NOT_WEB_MODE"
	// ErrCodeStaleAttempt: snapshot của lần làm cũ (tính năng đã rewind); ingest trả 200 accepted=false kèm mã này.
	ErrCodeStaleAttempt = "STALE_ATTEMPT"
)
