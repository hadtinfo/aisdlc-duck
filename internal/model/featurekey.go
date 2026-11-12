// Package model chứa các kiểu dữ liệu cơ bản của AISDLC.
package model

import (
	"encoding/json"
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"

	"aisdlc/internal/i18n"
)

var keyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]+-\d+$`)

// FeatureKey là mã tính năng, ví dụ PAY-123.
type FeatureKey string

// ParseFeatureKey kiểm tra và trả về FeatureKey.
func ParseFeatureKey(s string) (FeatureKey, error) {
	if !keyPattern.MatchString(s) {
		return "", fmt.Errorf(i18n.T("Feature key không hợp lệ: %s (ví dụ: PAY-123)"), s)
	}
	return FeatureKey(s), nil
}

func (k FeatureKey) String() string { return string(k) }

// UnmarshalYAML kiểm tra như ParseFeatureKey.
func (k *FeatureKey) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	v, err := ParseFeatureKey(s)
	if err != nil {
		return err
	}
	*k = v
	return nil
}

// UnmarshalJSON kiểm tra như ParseFeatureKey.
func (k *FeatureKey) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseFeatureKey(s)
	if err != nil {
		return err
	}
	*k = v
	return nil
}
