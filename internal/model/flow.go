package model

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Flow là luồng của tính năng; YAML ghi chữ hoa như Jackson ghi enum.
type Flow string

const (
	FlowFull Flow = "FULL"
	FlowLite Flow = "LITE"
)

func parseFlow(s string) (Flow, error) {
	switch v := Flow(strings.ToUpper(strings.TrimSpace(s))); v {
	case FlowFull, FlowLite:
		return v, nil
	}
	return "", fmt.Errorf("Flow không hợp lệ: %q (chỉ nhận FULL, LITE)", s)
}

// UnmarshalYAML nhận FULL/LITE không phân biệt hoa thường.
func (f *Flow) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v, err := parseFlow(s)
	if err != nil {
		return fmt.Errorf("dòng %d: %v", n.Line, err)
	}
	*f = v
	return nil
}

// UnmarshalJSON nhận FULL/LITE không phân biệt hoa thường.
func (f *Flow) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := parseFlow(s)
	if err != nil {
		return err
	}
	*f = v
	return nil
}
