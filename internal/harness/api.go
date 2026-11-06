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
	NotRun              = harnessapi.NotRun
	NoResults           = harnessapi.NoResults
	NoRow               = harnessapi.NoRow
	ExitTimeout         = harnessapi.ExitTimeout
	CanceledMessage     = harnessapi.CanceledMessage
)

var (
	ParseMode      = harnessapi.ParseMode
	WithDrafts     = harnessapi.WithDrafts
	SplitSteps     = harnessapi.SplitSteps
	Select         = harnessapi.Select
	WriteResults   = harnessapi.WriteResults
	ReadResults    = harnessapi.ReadResults
	TimeoutMessage = harnessapi.TimeoutMessage
)
