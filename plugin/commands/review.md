---
description: Eval, review độc lập bằng subagent và nộp cổng review
argument-hint: ""
---
Thực hiện theo thứ tự, `<KEY>` là Jira key của tính năng đang làm (xem `aisdlc status`).

1. Chạy `aisdlc eval`. Nếu không đạt, báo dev và dừng.
2. Dùng `aisdlc/features/<KEY>/eval/diff-by-step.md` do `aisdlc eval` vừa sinh (đã kèm patch), không tạo lại.
3. Gọi subagent `aisdlc-reviewer` (agent `aisdlc:aisdlc-reviewer`) với **chỉ** các đường dẫn: `brd.md` (nếu có), `design.md`, `design.yaml`, `cases.yaml` trong `aisdlc/features/<KEY>/` và `eval/diff-by-step.md`. Không tóm tắt quá trình code, không kể lý do vì sao code như vậy.
4. Ghi JSON subagent trả về, nguyên văn (bỏ rào ``` nếu subagent bọc JSON trong khối code, chỉ giữ JSON thuần), vào `aisdlc/features/<KEY>/review-agent.json` (cùng thư mục với brd.md, không phải eval/).
5. Chạy `aisdlc submit review`. Nếu báo JSON không hợp lệ, gọi lại subagent và ghi lại.
6. **Dừng.** Nói với dev: "Trước khi duyệt, `aisdlc review review` in review card (cờ đỏ, giả định, tóm tắt). Dev duyệt bằng `aisdlc approve review` ở terminal riêng."
   Nếu `aisdlc status` báo "Duyệt trong Claude Code: bật", nhắc thêm: "hoặc gõ `/aisdlc:approve review` để duyệt ngay trong Claude Code (hiện hộp xác nhận), hoặc duyệt trên web". Không tự duyệt khi dev chưa yêu cầu trong cuộc trò chuyện này.
