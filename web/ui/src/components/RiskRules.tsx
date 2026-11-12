import { useState } from "react";
import { DEV_LOWER_LABEL, KINDS, type Policy, type PolicyRule, nextRuleId, putRule, removeRule, sameRule } from "../risk";
import { LevelPill } from "./RiskPanel";
import { RiskDialog } from "./RiskDialog";
import { useT } from "../i18n";

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
  const { t } = useT();
  const [editing, setEditing] = useState<PolicyRule | "new" | null>(null);
  const presetName = policy.preset?.name ?? base.preset?.name ?? "";
  const baseRules = new Map(base.rules.map((r) => [r.id, r]));
  const readOnly = policy.schema === 1;
  return (
    <div className="rk-stack">
      <div className="rk-note info"><div className="grow">
        {t("rr.intro1")} <b>{t("rr.intro2")}</b> {t("rr.intro3")}
      </div></div>
      {readOnly && <div className="rk-note warn"><div className="grow">
        {t("rr.schema1a")} <b>schema 1</b> {t("rr.schema1b")}
      </div></div>}
      <section className="box">
        <div className="rk-head"><h2>{t("rr.title")}</h2>
          {!readOnly && <button type="button" onClick={() => setEditing("new")}>{t("rr.add")}</button>}
        </div>
        {policy.rules.length === 0 ? <p className="muted">{t("rr.none")} <b>{policy.default}</b>.</p> : (
          <table className="grid">
            <thead><tr><th>ID</th><th>{t("rr.describe")}</th><th>{t("rr.level")}</th><th>{t("rr.enabled")}</th><th>{t("rr.source")}</th>{!readOnly && <th />}</tr></thead>
            <tbody>
              {policy.rules.map((r) => {
                const b = baseRules.get(r.id);
                return (
                  <tr key={r.id} className={`${r.enabled ? "" : "off"} ${b && sameRule(b, r) ? "" : "changed"}`}>
                    <td className="mono">{r.id}</td>
                    <td>{r.describe ?? legacyText(r)}</td>
                    <td><LevelPill level={r.level} levels={policy.levels} /></td>
                    <td>{r.enabled ? t("rr.on") : t("rr.off")}</td>
                    <td>{presetRuleIds.includes(r.id)
                      ? <span className="tag preset">{t("rr.preset", { name: presetName })}</span>
                      : <span className="tag">{t("rr.project")}</span>}</td>
                    {!readOnly && (
                      <td className="act">
                        <button type="button" className="link" onClick={() => setEditing(r)}>{t("rr.edit")}</button>{" · "}
                        <button type="button" className="link" onClick={() => onChange(putRule(policy, { ...r, enabled: !r.enabled }))}>
                          {r.enabled ? t("rr.turnOff") : t("rr.turnOn")}
                        </button>
                        {!b && <>{" · "}<button type="button" className="link" onClick={() => onChange(removeRule(policy, r.id))}>{t("rr.delete")}</button></>}
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
        <div className="rk-head"><h2>{t("rr.otherSources")}</h2></div>
        <div className="rk-form">
          <label className="rk-field"><span>{t("rr.devLower")}</span>
            <select value={policy.classification.devLower} disabled={readOnly}
              onChange={(e) => onChange({ ...policy, classification: { ...policy.classification, devLower: e.target.value as "reason" | "never" } })}>
              {Object.entries(DEV_LOWER_LABEL).map(([v, label]) => <option key={v} value={v}>{label}</option>)}
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
  const { t } = useT();
  const [id, setId] = useState(rule?.id ?? nextRuleId(policy.rules));
  const [level, setLevel] = useState(rule?.level ?? policy.levels[policy.levels.length - 1] ?? "");
  const [describe, setDescribe] = useState(rule?.describe ?? "");
  const [error, setError] = useState("");

  function save() {
    const newId = id.trim();
    if (!newId) return setError(t("rr.needId"));
    if (newId !== rule?.id && policy.rules.some((r) => r.id === newId)) return setError(t("rr.idExists", { id: newId }));
    if (!describe.trim()) return setError(t("rr.needDescribe"));
    onSave({ id: newId, level, describe: describe.trim(), enabled: rule?.enabled ?? true }, rule?.id);
  }

  return (
    <RiskDialog title={rule ? t("rr.editTitle", { id: rule.id }) : t("rr.add")} onClose={onClose}>
      <div className="rk-form">
        {!rule && <label className="rk-field"><span>ID</span><input type="text" value={id} onChange={(e) => setId(e.target.value)} /></label>}
        <label className="rk-field"><span>{t("rr.levelWhenMatched")}</span>
          <select value={level} onChange={(e) => setLevel(e.target.value)}>
            {policy.levels.map((l) => <option key={l} value={l}>{l}</option>)}
          </select>
        </label>
        <label className="rk-field wide"><span>{t("rr.describeLabel")}</span>
          <textarea rows={3} value={describe} autoFocus placeholder={t("rr.describePh")}
            onChange={(e) => setDescribe(e.target.value)} />
        </label>
        {error && <p className="error wide" role="alert">{error}</p>}
        <div className="wide rk-row end">
          <button type="button" className="secondary" onClick={onClose}>{t("common.cancel")}</button>
          <button type="button" onClick={save}>{t("rr.saveDraft")}</button>
        </div>
      </div>
    </RiskDialog>
  );
}
