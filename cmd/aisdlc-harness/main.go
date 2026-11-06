// Lệnh aisdlc-harness: harness black-box (testcontainers) tách khỏi binary hook aisdlc; CLI aisdlc gọi nó như tiến
// trình con. Xem internal/harness/harnesscmd.
package main

import (
	"os"

	"aisdlc/internal/harness/harnesscmd"
)

func main() { os.Exit(harnesscmd.Main(os.Args[1:], os.Stderr, harnesscmd.OSSignals())) }
