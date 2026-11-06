package harness

import (
	"os"

	"aisdlc/internal/model"
	"aisdlc/internal/risk"
	"aisdlc/internal/spec"
)

// Checks gom bất biến chạy sau mỗi case: invariants của design.yaml (luồng FULL; mỗi bất biến có step thì chỉ từ bước
// đó trở đi) hoặc của cases.yaml (luồng lite: sau mọi case, bỏ step), rồi các yêu cầu sql-zero của risk.yaml.
// sql-zero là thuộc tính của cả tính năng nên gắn step = bước cuối trong design.yaml (không có bước: mọi case).
// stepOrder là thứ tự bước của design.yaml; luồng lite thì rỗng.
func Checks(p model.FeaturePaths) ([]spec.Invariant, []string, error) {
	var checks []spec.Invariant
	var order []string
	if exists(p.DesignYaml()) {
		d, err := spec.ReadDesign(p.DesignYaml())
		if err != nil {
			return nil, nil, err
		}
		checks = append(checks, d.Invariants...)
		order = d.StepOrder()
	} else if exists(p.Cases()) {
		c, err := spec.ReadCases(p.Cases())
		if err != nil {
			return nil, nil, err
		}
		for _, i := range c.Invariants {
			checks = append(checks, spec.Invariant{Name: i.Name, SQL: i.SQL})
		}
	}
	lastStep := ""
	if len(order) > 0 {
		lastStep = order[len(order)-1]
	}
	rf, ok, err := risk.ReadRiskFile(p.Risk())
	if err != nil {
		return nil, nil, err
	}
	if ok {
		for _, q := range rf.Requirements {
			if q.ID != risk.ReqSQLZero {
				continue
			}
			name, has := q.StrParam("name")
			if !has {
				name = risk.ReqSQLZero
			}
			sql, _ := q.StrParam("sql")
			checks = append(checks, spec.Invariant{Name: name, SQL: sql, Step: lastStep})
		}
	}
	return checks, order, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
