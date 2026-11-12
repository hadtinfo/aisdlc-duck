package cli

import (
	"encoding/json"

	"aisdlc/internal/i18n"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/webclient"
)

// isNotWeb: lỗi 409 của server là "repo không ở chế độ duyệt trên web". Server mới gửi mã NOT_WEB_MODE trong JSON
// lỗi; server cũ chỉ có câu (tiếng Việt, hoặc bản tiếng Anh trong catalog khi dịch theo Accept-Language), nên câu là
// đường lùi. msg là câu đã qua webError.
func isNotWeb(r webclient.Response, msg string) bool {
	var e struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(r.Body, &e) == nil && e.Code == snapshot.ErrCodeNotWebMode {
		return true
	}
	notWeb := i18n.VI.T("Repo không ở chế độ duyệt trên web")
	return msg == notWeb || msg == i18n.EN.T(notWeb)
}
