import { useState } from "react";
import { flowUnits, type FlowMode, type Policy, type PolicyReq, REQ_CATALOG, flowOf, setFlow, SQL_ZERO, reqKey, setReqParam, setRequirement } from "../risk";
import { LevelPill } from "./RiskPanel";
import { RiskDialog } from "./RiskDialog";
import { useT } from "../i18n";

interface Row { key: string; name: string; desc: string; custom: boolean; param?: string; unit?: string; req?: PolicyReq }

/** Chữ ký nội dung một yêu cầu (bỏ qua thứ tự khóa). */
const sig = (q: PolicyReq | undefined) =>
  q ? JSON.stringify(Object.entries(q).map(([k, v]) => [k, String(v)]).sort()) : "";

/** Tab "Yêu cầu theo mức": ma trận yêu cầu × mức, ô số cho tham số, form yêu cầu tự viết `sql-zero`. */
export function RiskRequirements({ policy, base, onChange }: { policy: Policy; base: Policy; onChange: (p: Policy) => void }) {
  const { t } = useT();
  const [added, setAdded] = useState<PolicyReq[]>([]);
  const [form, setForm] = useState(false);

  const all = (p: Policy) => Object.values(p.requirements).flat();
  const customs = new Map<string, PolicyReq>();
  for (const q of [...all(policy), ...added]) if (q.id === SQL_ZERO && !customs.has(reqKey(q))) customs.set(reqKey(q), q);
  const rows: Row[] = [
    ...REQ_CATALOG.map((c) => ({ key: c.id, name: c.name, desc: c.desc, custom: false, param: c.param, unit: c.unit })),
    ...[...customs.values()].map((q) => ({
      key: reqKey(q), name: t("rk.custom", { name: String(q.name ?? "") }), desc: t("rq.sqlZeroDesc", { sql: String(q.sql ?? "") }), custom: true, req: q,
    })),
  ];
  const find = (p: Policy, level: string, key: string) => (p.requirements[level] ?? []).find((q) => reqKey(q) === key);
  const changed = (key: string) => policy.levels.some((l) => sig(find(policy, l, key)) !== sig(find(base, l, key)));

  return (
    <div className="rk-stack">
      <div className="rk-note info"><div className="grow">
        {t("rq.intro1")} <b>Case</b> {t("rq.intro2")} <b>Review</b> {t("rq.intro3")}
      </div></div>
      <section className="box">
        <div className="rk-head">
          <h2>{t("rq.title")}</h2>
          <button type="button" className="secondary" onClick={() => setForm(true)}>{t("rq.custom")}</button>
        </div>
        <table className="grid rk-matrix">
          <thead><tr><th>{t("rq.requirement")}</th>{policy.levels.map((l) => <th key={l} className="lv"><LevelPill level={l} levels={policy.levels} /></th>)}</tr></thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.key} className={changed(r.key) ? "changed" : ""}>
                <td>
                  <div>{r.name} {r.custom && <span className="tag">{t("rq.customTag")}</span>}</div>
                  <div className={`rk-desc ${r.custom ? "mono" : ""}`}>{r.desc}</div>
                </td>
                {policy.levels.map((l) => {
                  const q = find(policy, l, r.key);
                  return (
                    <td key={l} className="cell">
                      <input type="checkbox" aria-label={`${r.name} – ${l}`} checked={!!q}
                        onChange={(e) => onChange(setRequirement(policy, l, r.key, e.target.checked, r.req))} />
                      {r.param && q && (
                        <>
                          <input type="number" min={1} aria-label={`${r.name} – ${l} – ${r.unit}`} value={String(q[r.param] ?? "")}
                            onChange={(e) => {
                              const v = e.target.value;
                              onChange(setReqParam(policy, l, r.key, r.param!, v === "" || Number.isNaN(Number(v)) ? v : Number(v)));
                            }} />
                          {" "}<span className="muted">{r.unit}</span>
                        </>
                      )}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </section>
      <section className="box">
        <div className="rk-head"><h2>{t("rq.flowTitle")}</h2></div>
        <p className="muted" style={{ fontSize: 12.5, marginTop: 0 }}>
          <code>auto</code>: {t("rq.flowAuto")} <code>manual</code>: {t("rq.flowManual")}
          {t("rq.flowDefault")} <code>manual</code>. <b>{t("rq.reviewManual")}</b>.
        </p>
        <table className="grid rk-matrix">
          <thead><tr><th>{t("rq.gate")}</th>{policy.levels.map((l) => <th key={l} className="lv"><LevelPill level={l} levels={policy.levels} /></th>)}</tr></thead>
          <tbody>
            {flowUnits(policy).map((u) => (
              <tr key={u.id} className={policy.levels.some((l) => flowOf(policy, l, u.id) !== flowOf(base, l, u.id)) ? "changed" : ""}>
                <td>{u.name} <span className="muted mono">{u.id}</span></td>
                {policy.levels.map((l) => (
                  <td key={l} className="cell">
                    <select aria-label={t("rq.flowAria", { unit: u.name, level: l })} value={flowOf(policy, l, u.id)}
                      onChange={(e) => onChange(setFlow(policy, l, u.id, e.target.value as FlowMode))}>
                      <option value="manual">manual</option>
                      <option value="auto">auto</option>
                    </select>
                  </td>
                ))}
              </tr>
            ))}
            <tr><td>Review</td><td colSpan={policy.levels.length} className="muted">{t("rq.reviewManual")}</td></tr>
          </tbody>
        </table>
      </section>
      <p className="muted" style={{ fontSize: 12.5, margin: 0 }}>
        {t("rq.lib1")} <code>aisdlc-junit</code> {t("rq.lib2")}
      </p>
      {form && (
        <SqlZeroDialog policy={policy} names={[...customs.values()].map((q) => String(q.name ?? ""))} onClose={() => setForm(false)}
          onSave={(q, level) => { setAdded((a) => [...a, q]); onChange(setRequirement(policy, level, reqKey(q), true, q)); setForm(false); }} />
      )}
    </div>
  );
}

function SqlZeroDialog({ policy, names, onClose, onSave }: {
  policy: Policy; names: string[]; onClose: () => void; onSave: (q: PolicyReq, level: string) => void;
}) {
  const { t } = useT();
  const [name, setName] = useState("");
  const [sql, setSql] = useState("");
  const [level, setLevel] = useState(policy.levels[policy.levels.length - 1] ?? "");
  const [error, setError] = useState("");
  function save() {
    const n = name.trim();
    const q = sql.trim();
    if (!n) return setError(t("rq.needName"));
    if (names.includes(n)) return setError(t("rq.nameExists", { name: n }));
    if (!/^(select|with)\b/i.test(q)) return setError(t("rq.sqlOnly"));
    onSave({ id: SQL_ZERO, name: n, sql: q }, level);
  }
  return (
    <RiskDialog title={t("rq.custom")} onClose={onClose}>
      <div className="rk-form">
        <label className="rk-field wide"><span>{t("rq.nameLabel")}</span>
          <input type="text" value={name} autoFocus placeholder={t("rq.namePh")} onChange={(e) => setName(e.target.value)} />
        </label>
        <label className="rk-field wide"><span>{t("rq.sqlLabel")}</span>
          <textarea className="mono" rows={4} value={sql} placeholder="SELECT id FROM orders WHERE status = 'PENDING' AND created_at < now() - interval '1 day'"
            onChange={(e) => setSql(e.target.value)} />
        </label>
        <label className="rk-field"><span>{t("rq.enableAt")}</span>
          <select value={level} onChange={(e) => setLevel(e.target.value)}>
            {policy.levels.map((l) => <option key={l} value={l}>{l}</option>)}
          </select>
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
