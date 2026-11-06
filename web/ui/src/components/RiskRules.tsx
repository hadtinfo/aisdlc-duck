import { useState } from "react";
import {
  AGENT_LABEL, DEV_LOWER_LABEL, KINDS, type Policy, type PolicyRule, type RuleKind, nextRuleId, putRule, removeRule, sameRule,
} from "../risk";
import { LevelPill } from "./RiskPanel";
import { RiskDialog } from "./RiskDialog";

/** Tab "Rule nhận diện": bảng rule, hộp thoại thêm/sửa, bật/tắt, khối nguồn phân loại khác. */
export function RiskRules({ policy, base, presetRuleIds, onChange }: {
  policy: Policy; base: Policy; presetRuleIds: string[]; onChange: (p: Policy) => void;
}) {
  const [editing, setEditing] = useState<PolicyRule | "new" | null>(null);
  const presetName = policy.preset?.name ?? base.preset?.name ?? "";
  const baseRules = new Map(base.rules.map((r) => [r.id, r]));
  return (
    <div className="rk-stack">
      <div className="rk-note info"><div className="grow">
        Mức rủi ro của tính năng = <b>mức cao nhất</b> trong: các rule khớp, đánh giá tác động của agent, mức dev đặt tay.
        Lõi AISDLC không biết nghiệp vụ; mọi rule ở đây là của dự án này.
      </div></div>
      <section className="box">
        <div className="rk-head"><h2>Rule nhận diện</h2><button type="button" onClick={() => setEditing("new")}>Thêm rule</button></div>
        {policy.rules.length === 0 ? <p className="muted">Chưa có rule nào; mọi tính năng nhận mức mặc định <b>{policy.default}</b>.</p> : (
          <table className="grid">
            <thead><tr><th>ID</th><th>Loại</th><th>Mẫu khớp</th><th>Mức</th><th>Nguồn</th><th /></tr></thead>
            <tbody>
              {policy.rules.map((r) => {
                const b = baseRules.get(r.id);
                return (
                  <tr key={r.id} className={`${r.enabled ? "" : "off"} ${b && sameRule(b, r) ? "" : "changed"}`}>
                    <td className="mono">{r.id}</td>
                    <td className="kind">{KINDS[r.kind]?.name ?? r.kind}</td>
                    <td>
                      {r.match.map((m, i) => <span key={i}>{i > 0 && " "}<code>{m}</code></span>)}
                      {r.note && <div className="rk-desc">{r.note}</div>}
                    </td>
                    <td><LevelPill level={r.level} levels={policy.levels} /></td>
                    <td>{presetRuleIds.includes(r.id)
                      ? <span className="tag preset">mẫu {presetName}</span>
                      : <span className="tag">dự án</span>}</td>
                    <td className="act">
                      <button type="button" className="link" onClick={() => setEditing(r)}>Sửa</button>{" · "}
                      <button type="button" className="link" onClick={() => onChange(putRule(policy, { ...r, enabled: !r.enabled }))}>
                        {r.enabled ? "Tắt" : "Bật"}
                      </button>
                      {!b && <>{" · "}<button type="button" className="link" onClick={() => onChange(removeRule(policy, r.id))}>Xóa</button></>}
                    </td>
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
          <label className="rk-field"><span>Đánh giá tác động của agent</span>
            <select value={policy.classification.agent}
              onChange={(e) => onChange({ ...policy, classification: { ...policy.classification, agent: e.target.value as "raise" | "off" } })}>
              {Object.entries(AGENT_LABEL).map(([v, t]) => <option key={v} value={v}>{t}</option>)}
            </select>
          </label>
          <label className="rk-field"><span>Dev hạ mức so với rule hoặc agent</span>
            <select value={policy.classification.devLower}
              onChange={(e) => onChange({ ...policy, classification: { ...policy.classification, devLower: e.target.value as "reason" | "never" } })}>
              {Object.entries(DEV_LOWER_LABEL).map(([v, t]) => <option key={v} value={v}>{t}</option>)}
            </select>
          </label>
          <p className="muted wide" style={{ fontSize: 12.5, margin: 0 }}>
            Agent đánh giá ở bước thiết kế: ghi mức đề xuất, lý do và nhãn tác động (ví dụ <code>money-movement</code>).
            Nhãn được dùng cho rule loại "Nhãn tác động".
          </p>
        </div>
      </section>
      {editing && (
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
  const [kind, setKind] = useState<RuleKind>(rule?.kind ?? "keyword");
  const [level, setLevel] = useState(rule?.level ?? policy.levels[policy.levels.length - 1] ?? "");
  const [match, setMatch] = useState((rule?.match ?? []).join("\n"));
  const [note, setNote] = useState(rule?.note ?? "");
  const [error, setError] = useState("");

  function save() {
    const items = match.split("\n").map((x) => x.trim()).filter(Boolean);
    const newId = id.trim();
    if (!newId) return setError("Cần ID rule");
    if (newId !== rule?.id && policy.rules.some((r) => r.id === newId)) return setError(`ID ${newId} đã có`);
    if (items.length === 0) return setError("Mẫu khớp không được để trống");
    onSave({ id: newId, kind, level, match: items, note: note.trim() || null, enabled: rule?.enabled ?? true }, rule?.id);
  }

  return (
    <RiskDialog title={rule ? `Sửa rule ${rule.id}` : "Thêm rule"} onClose={onClose}>
      <div className="rk-form">
        {!rule && <label className="rk-field"><span>ID</span><input type="text" value={id} onChange={(e) => setId(e.target.value)} /></label>}
        <label className="rk-field"><span>Loại</span>
          <select value={kind} onChange={(e) => setKind(e.target.value as RuleKind)}>
            {Object.entries(KINDS).map(([k, v]) => <option key={k} value={k}>{v.name}</option>)}
          </select>
        </label>
        <label className="rk-field"><span>Mức khi khớp</span>
          <select value={level} onChange={(e) => setLevel(e.target.value)}>
            {policy.levels.map((l) => <option key={l} value={l}>{l}</option>)}
          </select>
        </label>
        <label className="rk-field wide"><span>Mẫu khớp (mỗi dòng một) · so với {KINDS[kind].where}</span>
          <textarea className="mono" rows={3} value={match} placeholder={KINDS[kind].ph} autoFocus
            onChange={(e) => setMatch(e.target.value)} />
        </label>
        <label className="rk-field wide"><span>Ghi chú cho người đọc</span>
          <input type="text" value={note} onChange={(e) => setNote(e.target.value)} />
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
