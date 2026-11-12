package harnessapi

import (
	"testing"

	"aisdlc/internal/i18n"
)

// Hằng tiếng Việt cũ phải bằng đúng bản vi của hàm theo locale, nếu không internal/cli lệch với harness.
func TestCompatConstsMatchKeys(t *testing.T) {
	i18n.SetDefault(i18n.VI)
	for _, c := range []struct{ old, now string }{
		{NotRun, NotRunText()}, {NoResults, NoResultsText()}, {NoRow, NoRowText()},
		{CanceledMessage, Canceled()}, {PanicPrefix, PanicPrefixText()},
	} {
		if c.old != c.now {
			t.Errorf("hằng %q khác bản vi %q", c.old, c.now)
		}
	}
}
