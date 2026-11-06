import { useState } from "react";
import type { CaseData } from "../types";

const KIND: Record<string, string> = { happy: "Thuận", error: "Lỗi", boundary: "Biên" };

/** Bảng case; bấm một dòng để mở YAML đầy đủ (raw). */
export function CasesTable({ cases }: { cases: CaseData[] }) {
  const [open, setOpen] = useState<string | null>(null);
  if (cases.length === 0) return <p className="muted">Chưa có case.</p>;
  return (
    <table className="grid cases">
      <thead><tr><th>Case</th><th>Bước</th><th>Loại</th><th>Tiêu đề</th><th>Request</th><th>Kỳ vọng</th></tr></thead>
      <tbody>
        {cases.map((c) => (
          <CaseRows key={c.id} c={c} open={open === c.id} toggle={() => setOpen(open === c.id ? null : c.id)} />
        ))}
      </tbody>
    </table>
  );
}

function CaseRows({ c, open, toggle }: { c: CaseData; open: boolean; toggle: () => void }) {
  return (
    <>
      <tr className={`clickable${open ? " open" : ""}`} onClick={toggle} tabIndex={0} aria-expanded={open}
        onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); toggle(); } }}>
        <td><strong>{c.id}</strong></td>
        <td>{c.step}</td>
        <td><span className={`kind ${c.kind}`}>{KIND[c.kind] ?? c.kind}</span></td>
        <td>{c.title}</td>
        <td><code>{c.request}</code></td>
        <td><code>{c.expect}</code></td>
      </tr>
      {open && <tr className="raw-row"><td colSpan={6}><pre><code>{c.raw}</code></pre></td></tr>}
    </>
  );
}
