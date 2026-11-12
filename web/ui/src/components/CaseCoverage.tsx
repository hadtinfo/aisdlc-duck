import type { ReviewCardData } from "../types";
import { Key, useT } from "../i18n";

type CasesPart = NonNullable<ReviewCardData["cases"]>;
const KINDS: [string, Key][] = [["happy", "cases.kind.happy"], ["error", "cases.kind.error"], ["boundary", "cases.kind.boundary"]];

/** Độ phủ bước × loại case, và ma trận quy tắc × case khi BRD có ID quy tắc. */
export function CaseCoverage({ cases }: { cases: CasesPart }) {
  const { t } = useT();
  const coverage = cases.coverage ?? {};
  const steps = Object.keys(coverage).sort((a, b) => Number(a === "?") - Number(b === "?"));
  const rules = cases.ruleMatrix ? Object.entries(cases.ruleMatrix) : null;
  return (
    <>
      <h3>{t("coverage.byStep")}</h3>
      {steps.length === 0 ? <p className="muted">{t("cases.none")}</p> : (
        <div className="tbl">
          <table className="grid coverage">
            <thead><tr><th>{t("steps.step")}</th>{KINDS.map(([k, label]) => <th key={k}>{t(label)}</th>)}</tr></thead>
            <tbody>
              {steps.map((s) => (
                <tr key={s}>
                  <td>{s === "?" ? <span className="muted">{t("coverage.noStep")}</span> : <strong>{s}</strong>}</td>
                  {KINDS.map(([k]) => {
                    const n = coverage[s]?.[k] ?? 0;
                    return <td key={k} className={n === 0 ? "cell-warn" : ""}>{n}</td>;
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {rules && (
        <>
          <h3>{t("coverage.rules")}</h3>
          <div className="tbl">
            <table className="grid coverage">
              <thead><tr><th>{t("coverage.rule")}</th>{KINDS.map(([k, label]) => <th key={k}>{t(label)}</th>)}</tr></thead>
              <tbody>
                {rules.map(([r, kinds]) => (
                  <tr key={r}>
                    <td><strong>{r}</strong></td>
                    {KINDS.map(([k]) => {
                      const ids = kinds[k] ?? [];
                      return ids.length === 0
                        ? <td key={k} className="cell-bad">{t("coverage.missing")}</td>
                        : <td key={k}>{ids.join(", ")}</td>;
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </>
  );
}
