// Package fee tính phí chuyển tiền.
package fee

// Fee là phí chuyển tiền 0,15% số tiền (VND), làm tròn nửa lên.
func Fee(amount int64) int64 {
	return (amount*15 + 5_000) / 10_000
}
