package cli

import "aisdlc/internal/i18n"

// webNotWeb là câu 409 của server khi repo không ở chế độ duyệt trên web (MsgNotWeb của internal/web/gates). Đây là
// dữ liệu để so khớp, không phải chữ CLI tự in: server trả câu tiếng Việt này, hoặc bản tiếng Anh của nó trong catalog
// khi server dịch theo Accept-Language.
const webNotWeb = "Repo không ở chế độ duyệt trên web"

// isNotWeb: câu lỗi của server là "repo không ở chế độ duyệt trên web" (ở bất kỳ locale nào).
func isNotWeb(e string) bool { return e == webNotWeb || e == i18n.EN.T(webNotWeb) }
