import { useState } from "react";
import { Key, useT } from "../i18n";
import type { CaseData } from "../types";

const KIND: Record<string, Key> = { happy: "cases.kind.happy", error: "cases.kind.error", boundary: "cases.kind.boundary" };

/** Bảng case; bấm một dòng để mở YAML đầy đủ (raw). */
export function CasesTable({ cases }: { cases: CaseData[] }) {
  const { t } = useT();
  const [open, setOpen] = useState<string | null>(null);
  if (cases.length === 0) return <p className="muted">{t("cases.none")}</p>;
  return (
    <table className="grid cases">
      <thead><tr><th>Case</th><th>{t("steps.step")}</th><th>{t("token.kind")}</th><th>{t("common.title")}</th><th>Request</th><th>{t("cases.expect")}</th></tr></thead>
      <tbody>
        {cases.map((c) => (
          <CaseRows key={c.id} c={c} open={open === c.id} toggle={() => setOpen(open === c.id ? null : c.id)} />
        ))}
      </tbody>
    </table>
  );
}

function CaseRows({ c, open, toggle }: { c: CaseData; open: boolean; toggle: () => void }) {
  const { t } = useT();
  return (
    <>
      <tr className={`clickable${open ? " open" : ""}`} onClick={toggle} tabIndex={0} aria-expanded={open}
        onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); toggle(); } }}>
        <td><strong>{c.id}</strong></td>
        <td>{c.step}</td>
        <td><span className={`kind ${c.kind}`}>{KIND[c.kind] ? t(KIND[c.kind]) : c.kind}</span></td>
        <td>{c.title}</td>
        <td><code>{c.request}</code></td>
        <td><code>{c.expect}</code></td>
      </tr>
      {open && <tr className="raw-row"><td colSpan={6}><pre><code>{c.raw}</code></pre></td></tr>}
    </>
  );
}
