/** Mô hình vẽ review card (hàm thuần): ô tóm tắt, bản đồ bước → vùng code, ER diff, "đã xem" giả định. */
import type { Tone } from "./components/GateList";
import { Key, t as tr } from "./i18n";
import type { FlagLevel, GateName, ReviewCardData, ReviewFlag, Snapshot, TablePart } from "./types";

export type { Tone };

export function cardFor(snap: Snapshot, gate: GateName): ReviewCardData | null {
  return snap.reviewCards?.[gate] ?? null;
}

const TONE: Record<FlagLevel, Tone> = { HIGH: "bad", WARN: "warn", INFO: "ok", NA: "idle" };
export function flagTone(level: FlagLevel): Tone {
  return TONE[level] ?? "idle";
}

const TITLES: Record<string, Key> = {
  SCOPE_OVERLAP: "flag.SCOPE_OVERLAP",
  SCOPE_TOO_WIDE: "flag.SCOPE_TOO_WIDE",
  SCOPE_MATCHES_OLD_MIGRATION: "flag.SCOPE_MATCHES_OLD_MIGRATION",
  SCOPE_BAD_GLOB: "flag.SCOPE_BAD_GLOB",
  SCOPE_NO_SCOPE: "flag.SCOPE_NO_SCOPE",
  BUDGET_HIGH: "flag.BUDGET_HIGH",
  DB_TOUCHES_EXISTING: "flag.DB_TOUCHES_EXISTING",
  DB_CREATE_ONLY: "flag.DB_CREATE_ONLY",
  DB_UNRECOGNIZED_SQL: "flag.DB_UNRECOGNIZED_SQL",
  DB_COLUMN_UNPARSED: "flag.DB_COLUMN_UNPARSED",
  CASE_VALUE_NOT_IN_BRD: "flag.CASE_VALUE_NOT_IN_BRD",
  CASES_RULE_UNCOVERED: "flag.CASES_RULE_UNCOVERED",
  CASES_RULE_UNKNOWN: "flag.CASES_RULE_UNKNOWN",
  BRD_RULE_NO_ID: "flag.BRD_RULE_NO_ID",
  DEPENDENCY_ADDED: "flag.DEPENDENCY_ADDED",
  ASSUMPTIONS_EMPTY: "flag.ASSUMPTIONS_EMPTY",
};
export function flagTitle(f: ReviewFlag): string {
  if (f.code.endsWith("_FAILED") || f.code === "DOC_UNREADABLE") return tr("flag.failed");
  return TITLES[f.code] ? tr(TITLES[f.code]) : f.code;
}

const RANK: Record<FlagLevel, number> = { HIGH: 0, WARN: 1, INFO: 2, NA: 3 };
export function sortFlags(flags: ReviewFlag[]): ReviewFlag[] {
  return flags.map((f, i) => ({ f, i }))
    .sort((a, b) => (RANK[a.f.level] ?? 4) - (RANK[b.f.level] ?? 4) || a.i - b.i)
    .map((x) => x.f);
}

/** Cắt giữa, giữ đầu và đuôi (đuôi dài hơn khi lẻ để tên file còn nguyên). */
export function middleEllipsis(s: string, max: number): string {
  if (s.length <= max) return s;
  if (max < 2) return s.slice(0, max);
  const keep = max - 1, head = Math.floor(keep / 2), tail = keep - head;
  return s.slice(0, head) + "…" + s.slice(s.length - tail);
}

// ---------- Ô tóm tắt ----------

export interface Tile { label: string; value: string; sub: string; tone?: Tone }
const NONE = "—";

function flagTiles(flags: ReviewFlag[]): Tile[] {
  const tile = (label: string, level: FlagLevel, tone: Tone): Tile => {
    const hit = flags.filter((f) => f.level === level);
    return { label, value: String(hit.length), sub: hit.length ? flagTitle(hit[0]) : tr("tile.none"),
      ...(hit.length ? { tone } : {}) };
  };
  return [tile(tr("flags.title"), "HIGH", "bad"), tile(tr("tile.warnings"), "WARN", "warn")];
}

export function summaryTiles(card: ReviewCardData): Tile[] {
  const [high, warn] = flagTiles(card.flags ?? []);
  const empty = (label: string): Tile => ({ label, value: NONE, sub: "" });
  switch (card.gate) {
    case "DESIGN": {
      const d = card.design;
      if (!d) return [empty(tr("steps.step")), high, warn, empty("DB"), empty("API")];
      const tables = d.tables ?? [], steps = d.steps ?? [];
      const added = tables.filter((t) => t.status === "NEW").length;
      const altered = tables.filter((t) => t.status === "ALTERED").length;
      const dropped = tables.filter((t) => t.status === "DROPPED").length;
      const touched = new Set((d.touchesExisting ?? []).map((t) => t.table)).size;
      return [
        { label: tr("steps.step"), value: String(steps.length), sub: steps.map((s) => s.id).join(", ") },
        high, warn,
        { label: "DB", value: tr("tile.dbAdded", { n: added }) + (altered ? ` · ${tr("tile.dbAltered", { n: altered })}` : "") + (dropped ? ` · ${tr("tile.dbDropped", { n: dropped })}` : ""),
          sub: touched ? tr("tile.dbTouched", { n: touched }) : tr("tile.dbUntouched"),
          ...(dropped ? { tone: "bad" as Tone } : altered ? { tone: "warn" as Tone } : {}) },
        { label: "API", value: `${(d.apiLines ?? []).length} endpoint`, sub: (d.statusCodes ?? []).join(" · ") },
      ];
    }
    case "CASES": {
      const c = card.cases;
      if (!c) return [empty("Case"), high, warn, empty(tr("tile.stepsNoCase")), empty(tr("tile.rulesNoCase"))];
      const bare = Object.entries(c.coverage ?? {})
        .filter(([step, kinds]) => step !== "?" && Object.values(kinds).reduce((a, b) => a + b, 0) === 0)
        .map(([step]) => step);
      const rules = c.ruleMatrix ? Object.entries(c.ruleMatrix)
        .filter(([, kinds]) => Object.values(kinds).every((ids) => ids.length === 0)).map(([r]) => r) : null;
      return [
        { label: "Case", value: String((c.rows ?? []).length), sub: "" },
        high, warn,
        { label: tr("tile.stepsNoCase"), value: String(bare.length), sub: bare.join(", "),
          ...(bare.length ? { tone: "warn" as Tone } : {}) },
        rules ? { label: tr("tile.rulesNoCase"), value: String(rules.length), sub: rules.join(", "),
          ...(rules.length ? { tone: "warn" as Tone } : {}) } : empty(tr("tile.rulesNoCase")),
      ];
    }
    case "BRD": {
      const b = card.brd;
      const n = card.assumptions?.length;
      return [
        b ? { label: tr("coverage.rule"), value: String(b.rules), sub: tr("tile.withId", { n: b.rulesWithId }) } : empty(tr("coverage.rule")),
        b ? { label: tr("tile.criteria"), value: String(b.acceptanceCriteria), sub: "" } : empty(tr("tile.criteria")),
        n === undefined ? empty(tr("tile.assumptions")) : { label: tr("tile.assumptions"), value: n ? String(n) : tr("tile.notRecorded"), sub: "" },
        b ? { label: tr("tile.outOfScope"), value: String((b.outOfScope ?? []).length), sub: "" } : empty(tr("tile.outOfScope")),
      ];
    }
    default:
      return [high, warn];
  }
}

// ---------- Bản đồ bước → vùng code ----------

export interface MapNode { id: string; x: number; y: number; w: number; h: number; tone: Tone | "step" | "plain";
  title: string; sub: string; full: string }
export interface MapEdge { from: string; to: string; tone: Tone | "plain"; label?: string }
export interface StepMap { nodes: MapNode[]; edges: MapEdge[]; width: number; height: number; hidden: number }

const MAX_TARGETS = 12;
const STEP = { x: 10, w: 200, h: 64, gap: 24 };
const TARGET = { x: 420, w: 280, h: 56, gap: 16 };

interface Target { full: string; tone: MapNode["tone"]; sub: string; from: { step: string; tone: MapEdge["tone"] }[] }

/**
 * Glob theo đoạn đường dẫn, cùng nghĩa với `ScopeMatcher` (Java glob + "**&#47;" khớp cả 0 thư mục):
 * đoạn `**` khớp 0 hay nhiều đoạn (ở cuối glob thì ít nhất 1), `*` khớp mọi ký tự trong một đoạn, `?` một ký tự
 * không phải `/`. Không có ký tự đại diện thì chỉ khớp đúng đường dẫn đó.
 */
function globMatches(glob: string, path: string): boolean {
  const g = glob.split("/"), p = path.split("/");
  const seg = g.map((x) => x === "**" ? null
    // nosemgrep: javascript.lang.security.audit.detect-non-literal-regexp.detect-non-literal-regexp -- mọi ký tự đặc biệt của regex trong glob đã được escape
    : new RegExp("^" + x.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*+/g, "[^/]*").replace(/\?/g, "[^/]") + "$"));
  const memo = new Map<string, boolean>();
  const go = (i: number, j: number): boolean => {
    const k = `${i}:${j}`;
    const hit = memo.get(k);
    if (hit !== undefined) return hit;
    let r: boolean;
    if (i === g.length) r = j === p.length;
    else if (seg[i] === null) {
      const min = i === g.length - 1 ? 1 : 0;
      r = false;
      for (let n = j + min; n <= p.length && !r; n++) r = go(i + 1, n);
    } else r = j < p.length && seg[i]!.test(p[j]) && go(i + 1, j + 1);
    memo.set(k, r);
    return r;
  };
  return go(0, 0);
}

export function stepMap(card: ReviewCardData): StepMap | null {
  const steps = card.design?.steps;
  if (!steps || steps.length === 0) return null;
  const ids = new Set(steps.map((s) => s.id));
  const targets: Target[] = [];
  const byFull = new Map<string, Target>();
  const add = (full: string, tone: MapNode["tone"], sub: string, step: string, edge: MapEdge["tone"]) => {
    let t = byFull.get(full);
    if (!t) {
      t = { full, tone, sub, from: [] };
      byFull.set(full, t);
      targets.push(t);
    }
    if (!t.from.some((e) => e.step === step)) t.from.push({ step, tone: edge });
  };

  const regions: string[] = [];
  const flags = card.flags ?? [];
  for (const f of flags.filter((x) => x.code === "SCOPE_OVERLAP")) {
    const refs = f.refs ?? [];
    const who = refs.filter((r) => ids.has(r));
    const region = refs.find((r) => !ids.has(r));
    if (region === undefined) continue;
    regions.push(region);
    for (const s of who) add(region, "bad", tr("map.stepsTouch", { n: who.length }), s, "bad");
  }
  for (const f of flags.filter((x) => x.code === "SCOPE_MATCHES_OLD_MIGRATION")) {
    const [step, ...files] = f.refs ?? [];
    if (!step || !ids.has(step)) continue;
    for (const file of files) add(file, "warn", tr("map.existingMatches", { step }), step, "warn");
  }
  for (const s of steps) for (const p of s.plannedFiles ?? []) add(p, "ok", tr("map.newMatchesSql"), s.id, "plain");
  for (const s of steps) for (const g of s.emptyGlobs ?? []) add(g, "idle", tr("map.emptyGlob"), s.id, "plain");

  const covered = (file: string) => byFull.has(file) || regions.some((r) => globMatches(r, file));
  for (const s of steps) {
    const dirs = new Map<string, number>();
    for (const file of s.matchedFiles ?? []) {
      if (covered(file)) continue;
      const i = file.lastIndexOf("/");
      const dir = (i < 0 ? "" : file.slice(0, i)) + "/";
      dirs.set(dir, (dirs.get(dir) ?? 0) + 1);
    }
    for (const [dir, k] of dirs) add(dir, "plain", tr("map.existingFiles", { n: k }), s.id, "plain");
  }

  const shown = targets.slice(0, MAX_TARGETS).map((t) => ({ ...t }));
  // phần matchedFiles bị cắt (tối đa 20) ghi một lần vào đích thư mục cuối còn hiện của bước; không có thì bỏ
  for (const s of steps) {
    const more = (s.matchedCount ?? 0) - (s.matchedFiles ?? []).length;
    if (more <= 0) continue;
    const last = [...shown].reverse().find((t) => t.tone === "plain" && t.from.some((e) => e.step === s.id));
    if (last) last.sub += ` · ${tr("map.moreFiles", { n: more })}`;
  }

  const stepsH = steps.length * STEP.h + (steps.length - 1) * STEP.gap;
  const targetsH = shown.length ? shown.length * TARGET.h + (shown.length - 1) * TARGET.gap : 0;
  const inner = Math.max(stepsH, targetsH);
  const stepTop = 10 + (inner - stepsH) / 2, targetTop = 10 + (inner - targetsH) / 2;

  const nodes: MapNode[] = steps.map((s, i) => ({
    id: s.id, x: STEP.x, y: stepTop + i * (STEP.h + STEP.gap), w: STEP.w, h: STEP.h, tone: "step",
    title: s.id, sub: s.title, full: `${s.id} · ${s.title}`,
  }));
  const edges: MapEdge[] = [];
  const label = new Map(steps.map((s) => [s.id, (s.scope ?? []).length === 1 ? s.scope[0] : undefined]));
  shown.forEach((t, i) => {
    const id = `t${i}`;
    nodes.push({ id, x: TARGET.x, y: targetTop + i * (TARGET.h + TARGET.gap), w: TARGET.w, h: TARGET.h,
      tone: t.tone, title: middleEllipsis(t.full, 34), sub: t.sub, full: t.full });
    for (const e of t.from) {
      const l = label.get(e.step);
      edges.push({ from: e.step, to: id, tone: e.tone, ...(l !== undefined ? { label: l } : {}) });
    }
  });
  return { nodes, edges, width: 710, height: inner + 20, hidden: targets.length - shown.length };
}

// ---------- ER diff ----------

export interface ErBox { name: string; x: number; y: number; w: number; h: number; tone: Tone | "plain"; badge: string;
  rows: { name: string; type: string; added: boolean }[] }
export interface ErLayout { boxes: ErBox[]; edges: { from: string; fromRow: number; to: string }[]; width: number; height: number }

const BADGE: Record<TablePart["status"], { badge: Key; tone: Tone | "plain" }> = {
  NEW: { badge: "er.new", tone: "ok" },
  EXISTING: { badge: "er.existing", tone: "plain" },
  ALTERED: { badge: "er.altered", tone: "warn" },
  DROPPED: { badge: "er.dropped", tone: "bad" },
};
const ER = { left: 20, right: 420, w: 260, head: 40, row: 26, gap: 20, top: 20 };

export function erLayout(tables: TablePart[]): ErLayout | null {
  if (tables.length === 0) return null;
  const byName = (a: TablePart, b: TablePart) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0);
  const left = tables.filter((t) => t.status !== "NEW").sort(byName);
  const right = tables.filter((t) => t.status === "NEW").sort(byName);
  const names = new Map(tables.map((t) => [t.name.toLowerCase(), t.name]));
  const boxes: ErBox[] = [];
  const edges: ErLayout["edges"] = [];
  let bottom = 0;
  for (const [column, x] of [[left, ER.left], [right, ER.right]] as const) {
    let y = ER.top;
    for (const t of column) {
      const cols = [...t.columns.map((c) => ({ c, added: false })), ...(t.addedColumns ?? []).map((c) => ({ c, added: true }))];
      const rows = cols.map(({ c, added }) => ({
        name: c.name, type: [c.type ?? "", ...c.constraints].filter((s) => s !== "").join(" "), added }));
      const h = ER.head + ER.row * rows.length;
      boxes.push({ name: t.name, x, y, w: ER.w, h, tone: BADGE[t.status].tone, badge: tr(BADGE[t.status].badge), rows });
      cols.forEach(({ c }, i) => {
        const ref = c.references?.split("(")[0].trim().replace(/[`"[\]]/g, "");
        const to = ref ? names.get(ref.slice(ref.lastIndexOf(".") + 1).toLowerCase()) : undefined;
        if (to) edges.push({ from: t.name, fromRow: i, to });
      });
      y += h + ER.gap;
    }
    bottom = Math.max(bottom, y - ER.gap);
  }
  return { boxes, edges, width: ER.right + ER.w + ER.left, height: bottom + ER.top };
}

// ---------- "Đã xem" giả định (localStorage, chỉ tiện ích trình duyệt) ----------

export function seenKey(repoId: string, feature: string, gate: GateName, hash: string): string {
  return `aisdlc.seen:${repoId}:${feature}:${gate}:${hash}`;
}

export function loadSeen(key: string): number[] {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return [];
    const v: unknown = JSON.parse(raw);
    return Array.isArray(v) && v.every((n) => typeof n === "number") ? v : [];
  } catch {
    return [];
  }
}

export function saveSeen(key: string, seen: number[]): void {
  try {
    localStorage.setItem(key, JSON.stringify(seen));
  } catch {
    // trình duyệt chặn hoặc đầy: bỏ qua
  }
}
