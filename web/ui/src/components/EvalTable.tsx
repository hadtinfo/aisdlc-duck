import { ago } from "../format";
import type { EvalReport } from "../types";
import { useT } from "../i18n";

function softTone(level: string): string {
  const l = level.toUpperCase();
  if (l === "OK") return "ok";
  if (l === "WARN" || l === "WARNING" || l === "OVER") return "warn";
  return "bad";
}

/** Kết quả eval giống bảng ở CLI: kiểm tra cứng (đạt/không) và kiểm tra mềm (mức). */
export function EvalTable({ report }: { report: EvalReport | null }) {
  const { t } = useT();
  if (!report) return <p className="muted">{t("eval.none")} <code>aisdlc eval</code>.</p>;
  return (
    <div className="eval-block">
      <p className={`eval-head ${report.passed ? "pass" : "fail"}`}>
        {report.passed ? t("eval.passed") : t("eval.failed")}
        {report.at && <span className="muted"> · {ago(report.at)}{report.mode ? ` · ${report.mode}` : ""}</span>}
      </p>
      <table className="grid">
        <thead><tr><th colSpan={3}>{t("eval.hard")}</th></tr></thead>
        <tbody>
          {report.hard.map((c) => (
            <tr key={c.id}>
              <td className="mono">{c.id}</td>
              <td><span className={`pill ${c.passed ? "ok" : "bad"}`}>{c.passed ? t("eval.pass") : t("eval.fail")}</span></td>
              <td>{c.detail}</td>
            </tr>
          ))}
        </tbody>
        <thead><tr><th colSpan={3}>{t("eval.soft")}</th></tr></thead>
        <tbody>
          {report.soft.map((c) => (
            <tr key={c.id}>
              <td className="mono">{c.id}</td>
              <td><span className={`pill ${softTone(c.level ?? "")}`}>{c.level}</span></td>
              <td>{c.detail}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
