import { middleEllipsis, type MapNode, type StepMap } from "../reviewModel";

const curve = (x1: number, y1: number, x2: number, y2: number) => {
  const mx = (x1 + x2) / 2;
  return `M${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`;
};
const nodeClass = (t: MapNode["tone"]) => `n-${t}`;
const subClass = (t: MapNode["tone"]) => (t === "bad" || t === "warn" || t === "ok" ? `t-${t}` : "t-m");
const edgeClass = (t: string) => (t === "bad" ? "e-bad" : t === "warn" ? "e-warn" : "e");
/** Số ký tự vừa chiều rộng ô (ước lượng theo cỡ chữ trong review.css). */
const fit = (w: number, px: number) => Math.max(4, Math.floor((w - 28) / px));

/** Bản đồ bước → vùng code, vẽ nguyên văn `stepMap`; cạnh tỏa đều theo chiều cao của nút hai đầu. */
export function StepMapView({ map }: { map: StepMap }) {
  const byId = new Map(map.nodes.map((n) => [n.id, n]));
  const outs = new Map<string, number>(), ins = new Map<string, number>();
  for (const e of map.edges) {
    outs.set(e.from, (outs.get(e.from) ?? 0) + 1);
    ins.set(e.to, (ins.get(e.to) ?? 0) + 1);
  }
  const outSeen = new Map<string, number>(), inSeen = new Map<string, number>();
  const at = (n: MapNode, seen: Map<string, number>, total: number) => {
    const k = seen.get(n.id) ?? 0;
    seen.set(n.id, k + 1);
    return n.y + (n.h * (k + 1)) / (total + 1);
  };
  // Tính hình học trước, vẽ nhãn sau mọi cạnh để nhãn (có viền nền) không bị cạnh khác đè lên.
  const labelled = new Set<string>();
  const edges = map.edges.flatMap((e) => {
    const a = byId.get(e.from), b = byId.get(e.to);
    if (!a || !b) return [];
    const y1 = at(a, outSeen, outs.get(a.id)!), y2 = at(b, inSeen, ins.get(b.id)!);
    const first = e.label !== undefined && !labelled.has(e.from);
    if (first) labelled.add(e.from);
    return [{ tone: e.tone, d: curve(a.x + a.w, y1, b.x, y2), title: e.label !== undefined ? `${e.from}: ${e.label}` : "",
      label: first ? middleEllipsis(`${e.from}: ${e.label}`, 30) : "", x: a.x + a.w + 8, y: y1 - 5 }];
  });
  const aria = map.nodes.filter((n) => n.tone === "step").map((n) => n.full).join("; ");
  return (
    <div className="diagram">
      <svg viewBox={`0 0 ${map.width} ${map.height}`} role="img" aria-label={`Bản đồ bước → vùng code: ${aria}`}>
        {edges.map((e, i) => (
          <path key={i} className={edgeClass(e.tone)} d={e.d}>{e.title && <title>{e.title}</title>}</path>
        ))}
        {edges.filter((e) => e.label).map((e, i) => (
          <text key={i} className="t-cm e-label" x={e.x} y={e.y}>{e.label}</text>
        ))}
        {map.nodes.map((n) => (
          <g key={n.id}>
            <title>{n.full}</title>
            <rect className={nodeClass(n.tone)} x={n.x} y={n.y} width={n.w} height={n.h} rx={6} />
            <text className={n.tone === "step" ? "t-b" : "t-c"} x={n.x + 14} y={n.y + (n.tone === "step" ? 26 : 23)}>{n.title}</text>
            <text className={subClass(n.tone)} x={n.x + 14} y={n.y + (n.tone === "step" ? 46 : 42)}>
              {middleEllipsis(n.sub, fit(n.w, 6.4))}
            </text>
          </g>
        ))}
      </svg>
    </div>
  );
}
