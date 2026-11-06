// Lời giải e2e chỉ build sau khi chép vào bản sao của e2e/fixture-service (module example.com/fixture).
// go.mod riêng để `go test ./...`, `go vet` và lint ở gốc repo bỏ qua thư mục này.
module example.com/fixture-solutions

go 1.25.0
