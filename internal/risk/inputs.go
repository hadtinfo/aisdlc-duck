package risk

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"aisdlc/internal/lang"
	"aisdlc/internal/model"
	"aisdlc/internal/review"
	"aisdlc/internal/spec"
)

// RiskInputs là dữ kiện đưa vào bộ phân loại. Tables: "bảng" hoặc "bảng.cột"; Endpoints: "METHOD /path".
// AgentLevel rỗng khi agent không đánh giá. SampleFile là tên file mẫu của ngôn ngữ (để so glob đường dẫn);
// rỗng thì Classify dùng tên trung tính.
type RiskInputs struct {
	Text       string
	Paths      []string
	Tables     []string
	Endpoints  []string
	Labels     []string
	AgentLevel string
	SampleFile string
}

var endpointRe = regexp.MustCompile("\\b(GET|POST|PUT|PATCH|DELETE)\\s+(/\\S*)")
var endpointTailRe = regexp.MustCompile("[`,;:.)|*\"'\\]]+$")

// CollectFull gom dữ kiện luồng đầy đủ từ thư mục tính năng; file thiếu coi như rỗng.
func CollectFull(p model.FeaturePaths, title string, a lang.Adapter) (RiskInputs, error) {
	brd, err := readOrEmpty(p.Brd())
	if err != nil {
		return RiskInputs{}, err
	}
	design, err := readOrEmpty(p.DesignMd())
	if err != nil {
		return RiskInputs{}, err
	}
	var ds *spec.DesignSpec
	if fileExists(p.DesignYaml()) {
		d, err := spec.ReadDesign(p.DesignYaml())
		if err != nil {
			return RiskInputs{}, err
		}
		ds = &d
	}
	paths := []string{}
	seen := map[string]bool{}
	var impact *spec.Impact
	if ds != nil {
		for _, s := range ds.Steps {
			for _, g := range s.Scope {
				if !seen[g] {
					seen[g] = true
					paths = append(paths, g)
				}
			}
		}
		impact = ds.Impact
	}
	tables, err := draftTables(p.DbDrafts(), a)
	if err != nil {
		return RiskInputs{}, err
	}
	in := RiskInputs{
		Text:       join(title, brd, design),
		Paths:      paths,
		Tables:     tables,
		Endpoints:  endpoints(design),
		SampleFile: a.SampleFileName(),
	}
	applyImpact(&in, impact)
	return in, nil
}

// CollectLite gom dữ kiện luồng nhẹ từ cases.yaml.
func CollectLite(p model.FeaturePaths, title string, a lang.Adapter) (RiskInputs, error) {
	in := RiskInputs{Text: title, SampleFile: a.SampleFileName()}
	if fileExists(p.Cases()) {
		cs, err := spec.ReadCases(p.Cases())
		if err != nil {
			return RiskInputs{}, err
		}
		parts := []string{title}
		for _, c := range cs.Cases {
			if c.Title != "" {
				parts = append(parts, c.Title)
			}
		}
		in.Text = strings.Join(parts, "\n")
		in.Paths = append([]string{}, cs.Scope...)
		applyImpact(&in, cs.Impact)
	}
	normalize(&in)
	return in, nil
}

// CollectBrdOnly: dữ kiện chỉ có văn bản (xem trước mức ở cổng BRD).
func CollectBrdOnly(title, brd string) RiskInputs {
	in := RiskInputs{Text: join(title, brd)}
	normalize(&in)
	return in
}

func applyImpact(in *RiskInputs, impact *spec.Impact) {
	if impact != nil {
		in.Labels = append([]string{}, impact.Labels...)
		in.AgentLevel = impact.Level
	}
	normalize(in)
}

func normalize(in *RiskInputs) {
	if in.Paths == nil {
		in.Paths = []string{}
	}
	if in.Tables == nil {
		in.Tables = []string{}
	}
	if in.Endpoints == nil {
		in.Endpoints = []string{}
	}
	if in.Labels == nil {
		in.Labels = []string{}
	}
}

func join(parts ...string) string {
	var out []string
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n")
}

func fileExists(f string) bool {
	_, err := os.Stat(f)
	return err == nil
}

func readOrEmpty(f string) (string, error) {
	b, err := os.ReadFile(f)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("Không đọc được %s: %v", f, err)
	}
	return string(b), nil
}

// draftTables: bảng và cột của SQL nháp, theo thứ tự migration của adapter; bảng không kèm schema, cột chữ thường.
func draftTables(dir string, a lang.Adapter) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("Không đọc được %s: %v", dir, err)
	}
	out := []string{}
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, f := range a.MigrationOrder(files) {
		sql, err := readOrEmpty(f)
		if err != nil {
			return nil, err
		}
		for _, st := range review.ParseSQL(sql) {
			if st.Table == "" {
				continue
			}
			t := st.Table[strings.LastIndexByte(st.Table, '.')+1:]
			add(t)
			for _, c := range st.Columns {
				add(t + "." + strings.ToLower(c.Name))
			}
		}
	}
	return out, nil
}

func endpoints(designMd string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, line := range review.ParseDesign(designMd).APILines {
		for _, m := range endpointRe.FindAllStringSubmatch(line, -1) {
			e := m[1] + " " + endpointTailRe.ReplaceAllString(m[2], "")
			if !seen[e] {
				seen[e] = true
				out = append(out, e)
			}
		}
	}
	return out
}
