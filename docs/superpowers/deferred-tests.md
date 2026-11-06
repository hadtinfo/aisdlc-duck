# Test Java không port sang Go

Mọi test Java của `aisdlc-cli`, `aisdlc-core` và `aisdlc-junit` đã có bản Go (giai đoạn 1–5 xong, 2026-11-06), trừ các test ở bảng dưới. Bảng ghép từng test Java với test Go của giai đoạn 3 và 4 nằm trong lịch sử git của file này (trước commit dọn cuối giai đoạn 5).

Cách đối chiếu: mỗi phương thức `@Test` Java được so với tên `TestXxx` của Go (PascalCase, bỏ `_`, thêm `Test`); test không khớp tên được tra tay.

## Không port vì thiết kế Go khác

| Test Java | Lý do |
|---|---|
| `GateServiceTest.suiteLookupSkipsExcludedDirsAndIsSorted` | Bản Go không đưa file suite case vào artifact của cổng CASES. `TestCasesDoNotDependOnSuiteFiles` kiểm điều này. |
| `SurefireReportsTest.staleReportsFromAnEarlierBuildAreIgnored` | `go test -json` chạy mới mỗi lần nên không có báo cáo cũ để bỏ qua. |
| `SurefireReportsTest.casesSuiteClassMatchesSimpleOrQualifiedName` | Bản Go không có lớp suite case chạy qua JUnit; harness tự chạy case nên không cần khớp tên lớp. |
