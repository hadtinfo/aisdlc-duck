package spec

import (
	"fmt"
	"sort"
	"strings"
)

// Tập con khóa WireMock mà stub server của AISDLC hỗ trợ.
var (
	stubRequestKeys  = []string{"method", "url", "urlPath", "urlPathPattern", "queryParameters", "headers", "bodyPatterns"}
	stubResponseKeys = []string{"status", "headers", "body", "jsonBody", "fixedDelayMilliseconds"}
	stubMatcherKeys  = []string{"equalTo", "matches"}
	stubBodyKeys     = []string{"equalToJson", "contains"}
)

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func unsupported(id, path string, allowed []string) Finding {
	return Finding{"CASES_STUB_UNSUPPORTED", fmt.Sprintf("Stub %s: khóa \"%s\" không được hỗ trợ (chỉ hỗ trợ %s).",
		id, path, strings.Join(allowed, ", "))}
}

// checkExactlyOne kiểm một matcher: map có đúng một khóa thuộc allowed.
func checkExactlyOne(id, path string, v any, allowed []string) []Finding {
	m, ok := v.(map[string]any)
	if !ok {
		return []Finding{unsupported(id, path, allowed)}
	}
	var out []Finding
	good := 0
	for _, k := range sortedKeys(m) {
		if containsStr(allowed, k) {
			good++
			if good > 1 {
				out = append(out, unsupported(id, path+"."+k, allowed))
			}
		} else {
			out = append(out, unsupported(id, path+"."+k, allowed))
		}
	}
	if len(m) == 0 {
		out = append(out, unsupported(id, path, allowed))
	}
	return out
}

func validateStub(st StubSpec) []Finding {
	var out []Finding
	for _, k := range sortedKeys(st.Request) {
		path := "request." + k
		if !containsStr(stubRequestKeys, k) {
			out = append(out, unsupported(st.ID, path, stubRequestKeys))
			continue
		}
		switch k {
		case "queryParameters", "headers":
			m, ok := st.Request[k].(map[string]any)
			if !ok {
				out = append(out, unsupported(st.ID, path, stubMatcherKeys))
				continue
			}
			for _, name := range sortedKeys(m) {
				out = append(out, checkExactlyOne(st.ID, path+"."+name, m[name], stubMatcherKeys)...)
			}
		case "bodyPatterns":
			list, ok := st.Request[k].([]any)
			if !ok {
				out = append(out, unsupported(st.ID, path, stubBodyKeys))
				continue
			}
			for i, el := range list {
				out = append(out, checkExactlyOne(st.ID, fmt.Sprintf("%s[%d]", path, i), el, stubBodyKeys)...)
			}
		}
	}
	for _, k := range sortedKeys(st.Response) {
		if !containsStr(stubResponseKeys, k) {
			out = append(out, unsupported(st.ID, "response."+k, stubResponseKeys))
		}
	}
	return out
}
