// Package fee tính phí chuyển tiền.
package fee

// Fee là phí chuyển tiền 0,15% số tiền (VND).
func Fee(amount int64) int64 {
	return amount * 15 / 10000
}
