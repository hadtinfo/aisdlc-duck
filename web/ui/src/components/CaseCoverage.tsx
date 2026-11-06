import type { ReviewCardData } from "../types";

type CasesPart = NonNullable<ReviewCardData["cases"]>;
const KINDS: [string, string][] = [["happy", "Thuận"], ["error", "Lỗi"], ["boundary", "Biên"]];

/** Độ phủ bước × loại case, và ma trận quy tắc × case khi BRD có ID quy tắc. */
export function CaseCoverage({ cases }: { cases: CasesPart }) {
  const coverage = cases.coverage ?? {};
  const steps = Object.keys(coverage).sort((a, b) => Number(a === "?") - Number(b === "?"));
  const rules = cases.ruleMatrix ? Object.entries(cases.ruleMatrix) : null;
  return (
    <>
      <h3>Độ phủ theo bước</h3>
      {steps.length === 0 ? <p className="muted">Chưa có case.</p> : (
        <div className="tbl">
          <table className="grid coverage">
            <thead><tr><th>Bước</th>{KINDS.map(([k, t]) => <th key={k}>{t}</th>)}</tr></thead>
            <tbody>
              {steps.map((s) => (
                <tr key={s}>
                  <td>{s === "?" ? <span className="muted">không có bước</span> : <strong>{s}</strong>}</td>
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
          <h3>Quy tắc × case</h3>
          <div className="tbl">
            <table className="grid coverage">
              <thead><tr><th>Quy tắc</th>{KINDS.map(([k, t]) => <th key={k}>{t}</th>)}</tr></thead>
              <tbody>
                {rules.map(([r, kinds]) => (
                  <tr key={r}>
                    <td><strong>{r}</strong></td>
                    {KINDS.map(([k]) => {
                      const ids = kinds[k] ?? [];
                      return ids.length === 0
                        ? <td key={k} className="cell-bad">thiếu</td>
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
