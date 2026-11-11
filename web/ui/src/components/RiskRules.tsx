import { useState } from "react";
import { DEV_LOWER_LABEL, KINDS, type Policy, type PolicyRule, nextRuleId, putRule, removeRule, sameRule } from "../risk";
import { LevelPill } from "./RiskPanel";
import { RiskDialog } from "./RiskDialog";

/** Luật schema 1 (chỉ đọc) hiện theo kiểu cũ: loại, mẫu khớp và ghi chú. */
function legacyText(r: PolicyRule): string {
  const kind = r.kind ? KINDS[r.kind]?.name ?? r.kind : "";
  return [r.note, [kind, ...(r.match ?? [])].filter(Boolean).join(": ")].filter(Boolean).join(" — ");
}

/**
 * Tab "Rule nhận diện": rule mô tả bằng lời (schema 2) do subagent trong Claude Code xếp loại; bảng rule, hộp thoại
 * thêm/sửa, bật/tắt. Chính sách schema 1 chỉ đọc (chuyển sang schema 2 bằng nút ở đầu trang).
 */
export function RiskRules({ policy, base, presetRuleIds, onChange }: {
  policy: Policy; base: Policy; presetRuleIds: string[]; onChange: (p: Policy) => void;
}) {
  const [editing, setEditing] = useState<PolicyRule | "new" | null>(null);
  const presetName = policy.preset?.name ?? base.preset?.name ?? "";
  const baseRules = new Map(base.rules.map((r) => [r.id, r]));
  const readOnly = policy.schema === 1;
  return (
    <div className="rk-stack">
      <div className="rk-note info"><div className="grow">
        Mức rủi ro của tính năng = <b>mức cao nhất</b> trong: các rule mà subagent xác định là khớp, mức dev đặt tay.
        Mỗi rule là một mô tả bằng lời; việc xếp loại làm trong phiên Claude Code, máy chủ không chạy LLM.
        Lõi AISDLC không biết nghiệp vụ; mọi rule ở đây là của dự án này.
      </div></div>
      {readOnly && <div className="rk-note warn"><div className="grow">
        Chính sách đang dùng <b>schema 1</b> (luật so khớp) nên chỉ xem được. Chuyển sang schema 2 để sửa và để phân loại hoạt động.
      </div></div>}
      <section className="box">
        <div className="rk-head"><h2>Rule nhận diện</h2>
          {!readOnly && <button type="button" onClick={() => setEditing("new")}>Thêm rule</button>}
        </div>
        {policy.rules.length === 0 ? <p className="muted">Chưa có rule nào; mọi tính năng nhận mức mặc định <b>{policy.default}</b>.</p> : (
          <table className="grid">
            <thead><tr><th>ID</th><th>Mô tả</th><th>Mức</th><th>Bật</th><th>Nguồn</th>{!readOnly && <th />}</tr></thead>
            <tbody>
              {policy.rules.map((r) => {
                const b = baseRules.get(r.id);
                return (
                  <tr key={r.id} className={`${r.enabled ? "" : "off"} ${b && sameRule(b, r) ? "" : "changed"}`}>
                    <td className="mono">{r.id}</td>
                    <td>{r.describe ?? legacyText(r)}</td>
                    <td><LevelPill level={r.level} levels={policy.levels} /></td>
                    <td>{r.enabled ? "Bật" : "Tắt"}</td>
                    <td>{presetRuleIds.includes(r.id)
                      ? <span className="tag preset">mẫu {presetName}</span>
                      : <span className="tag">dự án</span>}</td>
                    {!readOnly && (
                      <td className="act">
                        <button type="button" className="link" onClick={() => setEditing(r)}>Sửa</button>{" · "}
                        <button type="button" className="link" onClick={() => onChange(putRule(policy, { ...r, enabled: !r.enabled }))}>
                          {r.enabled ? "Tắt" : "Bật"}
                        </button>
                        {!b && <>{" · "}<button type="button" className="link" onClick={() => onChange(removeRule(policy, r.id))}>Xóa</button></>}
                      </td>
                    )}
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </section>
      <section className="box">
        <div className="rk-head"><h2>Nguồn phân loại khác</h2></div>
        <div className="rk-form">
          <label className="rk-field"><span>Dev hạ mức so với rule</span>
            <select value={policy.classification.devLower} disabled={readOnly}
              onChange={(e) => onChange({ ...policy, classification: { ...policy.classification, devLower: e.target.value as "reason" | "never" } })}>
              {Object.entries(DEV_LOWER_LABEL).map(([v, t]) => <option key={v} value={v}>{t}</option>)}
            </select>
          </label>
        </div>
      </section>
      {editing && !readOnly && (
        <RuleDialog policy={policy} rule={editing === "new" ? null : editing} onClose={() => setEditing(null)}
          onSave={(r, oldId) => { onChange(putRule(policy, r, oldId)); setEditing(null); }} />
      )}
    </div>
  );
}

function RuleDialog({ policy, rule, onClose, onSave }: {
  policy: Policy; rule: PolicyRule | null; onClose: () => void; onSave: (r: PolicyRule, oldId?: string) => void;
}) {
  const [id, setId] = useState(rule?.id ?? nextRuleId(policy.rules));
  const [level, setLevel] = useState(rule?.level ?? policy.levels[policy.levels.length - 1] ?? "");
  const [describe, setDescribe] = useState(rule?.describe ?? "");
  const [error, setError] = useState("");

  function save() {
    const newId = id.trim();
    if (!newId) return setError("Cần ID rule");
    if (newId !== rule?.id && policy.rules.some((r) => r.id === newId)) return setError(`ID ${newId} đã có`);
    if (!describe.trim()) return setError("Mô tả không được để trống");
    onSave({ id: newId, level, describe: describe.trim(), enabled: rule?.enabled ?? true }, rule?.id);
  }

  return (
    <RiskDialog title={rule ? `Sửa rule ${rule.id}` : "Thêm rule"} onClose={onClose}>
      <div className="rk-form">
        {!rule && <label className="rk-field"><span>ID</span><input type="text" value={id} onChange={(e) => setId(e.target.value)} /></label>}
        <label className="rk-field"><span>Mức khi khớp</span>
          <select value={level} onChange={(e) => setLevel(e.target.value)}>
            {policy.levels.map((l) => <option key={l} value={l}>{l}</option>)}
          </select>
        </label>
        <label className="rk-field wide"><span>Mô tả bằng lời: khi nào tính năng thuộc rule này</span>
          <textarea rows={3} value={describe} autoFocus placeholder="Ví dụ: có dòng tiền đi qua (chuyển tiền, hoàn tiền, trừ số dư)"
            onChange={(e) => setDescribe(e.target.value)} />
        </label>
        {error && <p className="error wide" role="alert">{error}</p>}
        <div className="wide rk-row end">
          <button type="button" className="secondary" onClick={onClose}>Hủy</button>
          <button type="button" onClick={save}>Lưu vào nháp</button>
        </div>
      </div>
    </RiskDialog>
  );
}
