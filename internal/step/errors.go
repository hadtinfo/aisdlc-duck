// Package step chứa bước Sx, so khớp phạm vi (scope), phạm vi bổ sung và các lệnh chờ từ web.
package step

// Error là StepException của Java: Msg là thông báo tiếng Việt dành cho dev.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func errf(msg string) error { return &Error{Msg: msg} }
