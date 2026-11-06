---
description: Chạy eval tầng 1 (build, unit test, Docker và bộ case đầy đủ)
argument-hint: ""
---
1. Chạy `aisdlc eval`. Lệnh chạy `go build`, `go test` (unit test), khởi động Postgres (và Kafka nếu cấu hình) qua Docker rồi chạy toàn bộ bộ case của tính năng. Cần Docker chạy được; thiếu Docker thì lệnh báo "cần Docker cho harness" và dừng, báo dev chứ không tự sửa.
2. Tóm tắt ngắn kết quả: ĐẠT hay KHÔNG ĐẠT.
3. Nếu không đạt, liệt kê từng hard check fail (tên check và nguyên nhân chính). Không tự sửa code ngoài bước đang mở.
