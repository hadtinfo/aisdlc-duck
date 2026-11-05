package model

// Flow là luồng của tính năng; YAML ghi chữ hoa như Jackson ghi enum.
type Flow string

const (
	FlowFull Flow = "FULL"
	FlowLite Flow = "LITE"
)
