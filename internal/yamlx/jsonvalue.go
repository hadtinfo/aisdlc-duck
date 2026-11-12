package yamlx

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"aisdlc/internal/i18n"
)

// JSONValue chuyển node YAML thành giá trị marshal được ra JSON, giống cách Jackson
// giữ nguyên chuỗi: !!timestamp giữ nguyên văn, khóa map luôn là chuỗi, alias được giải.
func JSONValue(n *yaml.Node) (any, error) {
	if n == nil {
		return nil, nil
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return JSONValue(n.Content[0])
	case yaml.AliasNode:
		return JSONValue(n.Alias)
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := JSONValue(c)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.MappingNode:
		out := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			for k.Kind == yaml.AliasNode && k.Alias != nil {
				k = k.Alias
			}
			if k.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf(i18n.T("dòng %d: khóa của mapping phải là giá trị đơn"), k.Line)
			}
			v, err := JSONValue(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			out[k.Value] = v
		}
		return out, nil
	case yaml.ScalarNode:
		if n.Tag == "!!timestamp" {
			return n.Value, nil
		}
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, fmt.Errorf(i18n.T("dòng %d: %v"), n.Line, err)
		}
		return v, nil
	}
	return nil, nil
}
