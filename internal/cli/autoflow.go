package cli

import (
	"slices"

	"aisdlc/internal/gate"
	"aisdlc/internal/risk"
)

// AutoFlow của Java: quy trình theo mức rủi ro ở CLI (spec risk-flow §5): tự duyệt cổng sau khi nộp sạch (chỉ chế độ
// local) và thay thế bản tự duyệt khi mức mới đặt cổng về duyệt tay. Cách hiển thị bản duyệt ở display.go.

// afterSubmit: sau khi nộp g sạch ở chế độ local, tự duyệt nếu flow của mức áp cho cổng là auto và review card không
// có cờ HIGH. Trả nhãn hiển thị khi đã tự duyệt ("" khi không); in lý do khi flow cho phép mà cờ HIGH chặn. Chế độ
// web: CLI không duyệt; trả nhãn khi bản nộp (đã vào cache) có bản tự duyệt của server.
func afterSubmit(ctx *Context, f *Feature, g gate.Gate) (string, error) {
	if g == gate.REVIEW {
		return "", nil
	}
	if f.Mode == gate.ModeWeb {
		hash, err := f.Gates.CurrentHash(g)
		if err != nil {
			return "", err
		}
		a, ok, err := f.Gates.Approval(g)
		if err != nil || !ok || !a.IsAuto() || a.Superseded || a.Hash != hash {
			return "", err
		}
		return autoLabel(a), nil
	}
	st, err := f.Gates.State(g)
	if err != nil || st != gate.AwaitingApproval {
		return "", err
	}
	card, err := safeCard(ctx, f, g)
	if err != nil {
		return "", nil // không dựng được thẻ: không biết cờ, để dev duyệt
	}
	high := risk.HasHighFlag(card.Flags)
	rf, hasRisk, err := risk.ReadRiskFile(f.Paths.Risk())
	if err != nil {
		return "", nil // risk.yaml hỏng: không biết mức, để dev duyệt
	}
	var level, reason string
	var flowAuto, manual bool
	if g == gate.BRD {
		l, ok := TryLoadPolicy(ctx, f.Root)
		if !ok || card.RiskPreview == nil {
			return "", nil
		}
		p := l.Policy
		fileLevel := ""
		if hasRisk {
			fileLevel = rf.Level
		}
		// đã có risk.yaml (vd. nâng mức ở submit design, risk set): mức chặt hơn của mức tạm và mức trong file, và flow
		// chụp trong file cũng phải cho phép — nộp lại BRD không hoàn tác được bản thay thế
		level = risk.LevelForGate(p, g, card.RiskPreview.Level, fileLevel)
		flowAuto = p.FlowFor(level, risk.FlowUnit(g)) == "auto" && (!hasRisk || risk.AutoFromRiskFile(&rf, g))
		if !flowAuto {
			reason, manual = "Mức "+level+": cổng "+risk.FlowUnit(g)+" đặt duyệt tay", true
		} else {
			reason, manual = risk.AutoGate(p, level, g, high)
		}
	} else {
		// Thiết kế trở đi: flow đã chụp trong risk.yaml (CI kiểm đúng nguồn này); không có risk.yaml thì duyệt tay
		if !hasRisk {
			return "", nil
		}
		level = rf.Level
		flowAuto = risk.AutoFromRiskFile(&rf, g)
		switch {
		case !flowAuto:
			reason, manual = "Mức "+level+": cổng "+risk.FlowUnit(g)+" đặt duyệt tay", true
		case high:
			reason, manual = "Review card có cờ đỏ mức HIGH", true
		}
	}
	if manual {
		if flowAuto {
			ctx.Println("Không tự duyệt cổng " + string(g) + ": " + reason + ".")
		}
		return "", nil
	}
	a, err := f.Gates.AutoApprove(g, "flow:"+level)
	if err != nil {
		return "", err
	}
	return autoLabel(a), nil
}

// reconcile: chế độ local, đầu mỗi submit và step finish, thay thế mọi bản tự duyệt còn hiệu lực mà flow chụp trong
// risk.yaml hiện tại đặt về duyệt tay (in thông điệp như khi nâng mức). Dựa vào trạng thái nên chạy lại không đổi gì:
// bù cho lệnh trước đã ghi risk.yaml mức mới rồi hỏng (vd. harness lỗi) trước khi kịp thay thế. Web: server thay thế
// khi nhận event rủi ro.
func reconcile(ctx *Context, f *Feature) ([]gate.Gate, error) {
	if f.Mode == gate.ModeWeb {
		return nil, nil
	}
	rf, ok, err := risk.ReadRiskFile(f.Paths.Risk())
	if err != nil || !ok {
		return nil, nil
	}
	var p *risk.RiskPolicy
	if l, ok := TryLoadPolicy(ctx, f.Root); ok {
		p = &l.Policy
	}
	return supersede(ctx, f, p, "", rf)
}

// supersede: mức rủi ro vừa đổi thành next, các cổng đang có bản tự duyệt mà flow của mức mới là duyệt tay bị thay
// thế (local ghi gates.json; web chỉ in, server tự làm) và in thông điệp. from: mức trước ("" khi chưa có risk.yaml:
// lấy mức trong bản tự duyệt). "Mức tăng" chỉ khi hạng mức thật sự tăng theo policy (nil = không biết); ngược lại là
// "Flow của mức … đổi". Trả các cổng bị thay thế.
func supersede(ctx *Context, f *Feature, policy *risk.RiskPolicy, from string, next risk.RiskFile) ([]gate.Gate, error) {
	seq := gate.Sequence(f.Flow())
	var manual []gate.Gate
	for _, g := range seq {
		if g != gate.REVIEW && !risk.AutoFromRiskFile(&next, g) {
			manual = append(manual, g)
		}
	}
	activeAuto := func(g gate.Gate) (gate.Approval, bool, error) {
		a, ok, err := f.Gates.Approval(g)
		if err != nil || !ok || !a.IsAuto() || a.Superseded {
			return gate.Approval{}, false, err
		}
		return a, true, nil
	}
	before := from
	var hit []gate.Gate
	if f.Mode == gate.ModeWeb {
		for _, g := range seq {
			if !slices.Contains(manual, g) {
				continue
			}
			a, ok, err := activeAuto(g)
			if err != nil {
				return nil, err
			}
			if ok {
				hit = append(hit, g)
				if before == "" {
					before = a.RuleLevel()
				}
			}
		}
	} else {
		if before == "" {
			for _, g := range manual {
				a, ok, err := activeAuto(g)
				if err != nil {
					return nil, err
				}
				if ok {
					before = a.RuleLevel()
					break
				}
			}
		}
		var err error
		if hit, err = f.Gates.SupersedeAuto(manual...); err != nil {
			return nil, err
		}
	}
	if len(hit) > 0 {
		ctx.Println(risk.SupersedeMessage(policy, before, next.Level, hit))
	}
	return hit, nil
}
