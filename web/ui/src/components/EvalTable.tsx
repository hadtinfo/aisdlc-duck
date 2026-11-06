import { ago } from "../format";
import type { EvalReport } from "../types";

function softTone(level: string): string {
  const l = level.toUpperCase();
  if (l === "OK") return "ok";
  if (l === "WARN" || l === "WARNING" || l === "OVER") return "warn";
  return "bad";
}

/** Kết quả eval giống bảng ở CLI: kiểm tra cứng (đạt/không) và kiểm tra mềm (mức). */
export function EvalTable({ report }: { report: EvalReport | null }) {
  if (!report) return <p className="muted">Chưa có kết quả eval. Chạy <code>aisdlc eval</code>.</p>;
  return (
    <div className="eval-block">
      <p className={`eval-head ${report.passed ? "pass" : "fail"}`}>
        {report.passed ? "Eval ĐẠT" : "Eval KHÔNG ĐẠT"}
        {report.at && <span className="muted"> · {ago(report.at)}{report.mode ? ` · ${report.mode}` : ""}</span>}
      </p>
      <table className="grid">
        <thead><tr><th colSpan={3}>Kiểm tra cứng</th></tr></thead>
        <tbody>
          {report.hard.map((c) => (
            <tr key={c.id}>
              <td className="mono">{c.id}</td>
              <td><span className={`pill ${c.passed ? "ok" : "bad"}`}>{c.passed ? "ĐẠT" : "KHÔNG ĐẠT"}</span></td>
              <td>{c.detail}</td>
            </tr>
          ))}
        </tbody>
        <thead><tr><th colSpan={3}>Kiểm tra mềm</th></tr></thead>
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
