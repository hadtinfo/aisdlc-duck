// Package gate chứa các cổng duyệt, store local, cache web đã ký và GateService.
package gate

// Error là GateException của Java: Msg là thông báo tiếng Việt dành cho dev.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func errf(msg string) error { return &Error{Msg: msg} }
