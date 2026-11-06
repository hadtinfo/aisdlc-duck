import { codeSpans } from "../markdown";
import type { ApiRow } from "../types";

const spans = (s: string) => codeSpans(s).map((x, j) => (x.code ? <code key={j}>{x.text}</code> : x.text));

/** Bảng kết quả API (mã · khi nào · body), dưới là các dòng endpoint (đoạn `code` vẽ thành <code>). */
export function ApiTable({ rows, lines }: { rows: ApiRow[]; lines: string[] }) {
  return (
    <>
      {rows.length === 0 ? <p className="muted">Không đọc được bảng kết quả trong design.md.</p> : (
        <div className="tbl">
          <table className="grid">
            <thead><tr><th>Kết quả</th><th>Khi nào</th><th>Body</th></tr></thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={i}><td><code>{r.status}</code></td><td>{spans(r.when)}</td><td>{spans(r.body)}</td></tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {lines.map((l, i) => (
        <p key={i} className="note">{spans(l)}</p>
      ))}
    </>
  );
}
