package step

import (
	"encoding/json"

	"aisdlc/internal/fsx"
)

// writeJSON ghi JSON thụt 2 dấu cách, kiểu atomic.
func writeJSON(file string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(file, append(b, '\n'), 0o644)
}
