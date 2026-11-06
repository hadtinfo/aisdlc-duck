---
description: Chạy eval tầng 1 (build, unit test, Docker và bộ case đầy đủ)
argument-hint: ""
---
1. Chạy `aisdlc eval`. Lệnh chạy `go build`, `go test` (unit test), khởi động Postgres (và Kafka nếu cấu hình) qua Docker rồi chạy toàn bộ bộ case của tính năng. Cần Docker chạy được; không có thì harness báo lỗi khởi động (ví dụ "không khởi động được Postgres: ...") và các case đều ERROR. Chép nguyên văn lỗi đó cho dev, không tự sửa môi trường.
2. Tóm tắt ngắn kết quả: ĐẠT hay KHÔNG ĐẠT.
3. Kết quả nằm ở `aisdlc/features/<KEY>/eval/report.json`, `report.md`, `diff-by-step.md`; log harness ở `.aisdlc/run/<KEY>/harness.log`. Đọc chúng khi cần chi tiết.
4. Nếu không đạt, liệt kê từng hard check fail (tên check và nguyên nhân chính). Không tự sửa code ngoài bước đang mở.
