import { middleEllipsis, type ErBox, type ErLayout } from "../reviewModel";
import { useT } from "../i18n";

const ROW_TOP = 40, ROW_H = 26, TYPE_X = 118;
const rowY = (b: ErBox, i: number) => b.y + ROW_TOP + ROW_H * i + ROW_H / 2;
const badgeClass = (t: ErBox["tone"]) => (t === "bad" || t === "warn" || t === "ok" ? `t-${t}` : "t-m");

/** Đường khóa ngoại: từ dòng cột của bảng nguồn tới đầu bảng đích; cùng cột thì vòng ra bên phải. */
function fkPath(a: ErBox, row: number, b: ErBox): string {
  const y1 = rowY(a, row), y2 = b.y + 20;
  if (a.x > b.x) {
    const x1 = a.x, x2 = b.x + b.w, mx = (x1 + x2) / 2;
    return `M${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`;
  }
  if (a.x < b.x) {
    const x1 = a.x + a.w, x2 = b.x, mx = (x1 + x2) / 2;
    return `M${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`;
  }
  const x = a.x + a.w, out = x + 40;
  return `M${x} ${y1} C ${out} ${y1}, ${out} ${y2}, ${x} ${y2}`;
}

/** Sơ đồ ER của SQL nháp, vẽ nguyên văn `erLayout`: bảng có sẵn bên trái, bảng mới bên phải. */
export function ErDiagram({ layout }: { layout: ErLayout }) {
  const { t } = useT();
  const byName = new Map(layout.boxes.map((b) => [b.name, b]));
  const aria = layout.boxes.map((b) => `${b.name} (${b.badge})`).join(", ");
  return (
    <div className="diagram">
      <svg viewBox={`0 0 ${layout.width} ${layout.height}`} role="img" aria-label={t("er.aria", { tables: aria })}>
        {layout.boxes.map((b) => (
          <g key={b.name}>
            <title>{`${b.name} · ${b.badge}`}</title>
            <rect className={`n-${b.tone}`} x={b.x} y={b.y} width={b.w} height={b.h} rx={6} />
            <text className="t-b" x={b.x + 14} y={b.y + 25}>{middleEllipsis(b.name, 24)}</text>
            <text className={badgeClass(b.tone)} x={b.x + b.w - 14} y={b.y + 25} textAnchor="end">{b.badge}</text>
            <line className="sep" x1={b.x} y1={b.y + ROW_TOP} x2={b.x + b.w} y2={b.y + ROW_TOP} />
            {b.rows.map((r, i) => (
              <g key={i}>
                <title>{`${r.added ? "+ " : ""}${r.name} ${r.type}`.trim()}</title>
                <text className={r.added ? "t-c t-added" : "t-c"} x={b.x + 14} y={rowY(b, i) + 4}>
                  {middleEllipsis((r.added ? "+ " : "") + r.name, 14)}
                </text>
                <text className="t-cm" x={b.x + TYPE_X} y={rowY(b, i) + 4}>{middleEllipsis(r.type, 18)}</text>
              </g>
            ))}
          </g>
        ))}
        {layout.edges.map((e, i) => {
          const a = byName.get(e.from), b = byName.get(e.to);
          return a && b ? <path key={i} className="e" d={fkPath(a, e.fromRow, b)}><title>{`${e.from} → ${e.to}`}</title></path> : null;
        })}
      </svg>
    </div>
  );
}
