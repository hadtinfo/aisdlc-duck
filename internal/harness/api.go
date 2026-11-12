package harness

import "aisdlc/internal/harness/harnessapi"

// Tên của harnessapi dùng lại trong harness (phần nặng, có testcontainers); CLI và eval chỉ import harnessapi.
type (
	Options      = harnessapi.Options
	RunResult    = harnessapi.RunResult
	VerifyResult = harnessapi.VerifyResult
)

const (
	ModeCases           = harnessapi.ModeCases
	ModeCasesWithDrafts = harnessapi.ModeCasesWithDrafts
	ModeMigrationCheck  = harnessapi.ModeMigrationCheck
	ExitTimeout         = harnessapi.ExitTimeout
	// Hằng tiếng Việt cũ (xem harnessapi/compat.go); code mới dùng NotRunText, NoResultsText, NoRowText, Canceled.
	NotRun          = harnessapi.NotRun
	NoResults       = harnessapi.NoResults
	NoRow           = harnessapi.NoRow
	CanceledMessage = harnessapi.CanceledMessage
)

var (
	ParseMode      = harnessapi.ParseMode
	WithDrafts     = harnessapi.WithDrafts
	SplitSteps     = harnessapi.SplitSteps
	Select         = harnessapi.Select
	WriteResults   = harnessapi.WriteResults
	ReadResults    = harnessapi.ReadResults
	TimeoutMessage = harnessapi.TimeoutMessage
	NotRunText     = harnessapi.NotRunText
	NoResultsText  = harnessapi.NoResultsText
	NoRowText      = harnessapi.NoRowText
	Canceled       = harnessapi.Canceled
)
