/** Chính sách rủi ro trên web: kiểu dữ liệu, gọi /api/policy/**, logic nháp (spec §6). Thang mức do dự án đặt. */
import { ApiError, getJson, postJson } from "./api";

export type RuleKind = "keyword" | "path" | "table" | "endpoint" | "label";
export interface PolicyRule { id: string; kind: RuleKind; match: string[]; level: string; note?: string | null; enabled: boolean }
/** Yêu cầu dạng phẳng như YAML: `{ id: coverage, min: 90 }`. */
export interface PolicyReq { id: string; [param: string]: unknown }
export interface Policy {
  schema: number; version: number; preset?: { name: string; version: string } | null;
  levels: string[]; default: string; rules: PolicyRule[];
  classification: { agent: "raise" | "off"; devLower: "reason" | "never" };
  requirements: Record<string, PolicyReq[]>;
  /** Quy trình theo mức: mức → cổng (brd|design|cases|step) → auto|manual. Vắng = mọi cổng manual. Review luôn duyệt tay. */
  flow?: Record<string, Partial<Record<FlowUnit, FlowMode>>>;
}
export type FlowUnit = "brd" | "design" | "cases" | "step";
export type FlowMode = "auto" | "manual";
export const FLOW_UNITS: { id: FlowUnit; name: string }[] = [
  { id: "brd", name: "BRD" }, { id: "design", name: "Thiết kế" }, { id: "cases", name: "Case" }, { id: "step", name: "Bước" },
];
export interface PolicyChange { kind: string; key: string; before: string | null; after: string | null; loosening: boolean }
export interface PresetUpdate {
  name: string; from: string; to: string; baseMissing?: boolean; changes: PolicyChange[];
  /** Bản mẫu mới (để áp từng thay đổi vào nháp). */
  policy?: Policy;
}
export interface PolicyView {
  version: number; policy: Policy; hash: string; yaml?: string; presetUpdate: PresetUpdate | null;
  featureCounts: Record<string, number>; presetRuleIds?: string[];
}
export interface PolicyVersion {
  version: number; author: string; reason: string; source: "import" | "web" | "preset-update" | string;
  createdAt: string; changes: PolicyChange[];
}
export interface RiskMatch { rule: string; level: string; on: string }
export interface ClassifyInputs {
  text: string; paths: string[]; tables: string[]; endpoints: string[]; labels: string[]; agentLevel: string | null;
}
export interface ClassifyResult {
  result: { level: string; ruleLevel: string; matched: RiskMatch[]; agentLevel: string | null };
  requirements: { id: string; name: string; params: Record<string, unknown> }[];
}
export interface Draft { baseVersion: number; policy: Policy; fromPreset?: boolean }

// ---- API ----

/** Chính sách đang hiệu lực; repo chưa có chính sách (404) → null. */
export async function loadPolicy(repo: string): Promise<PolicyView | null> {
  try {
    return await getJson<PolicyView>(`/api/policy?${new URLSearchParams({ repo })}`);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
}
export const versions = (repo: string) => getJson<PolicyVersion[]>(`/api/policy/versions?${new URLSearchParams({ repo })}`);
/** 400 → ApiError câu lỗi đầu; 409 (có bản mới hơn) → ApiError status 409 kèm câu của server. */
export const publish = (repo: string, baseVersion: number, policy: Policy, reason: string, fromPreset = false) =>
  postJson<{ version: number; loosening: PolicyChange[] }>("/api/policy/publish",
    { repo, baseVersion, policy, reason, fromPreset });
export const validate = (policy: Policy, repo?: string) =>
  postJson<{ errors: string[]; changes: PolicyChange[] }>("/api/policy/validate", repo ? { policy, repo } : { policy });
export const classify = (policy: Policy, inputs: ClassifyInputs) =>
  postJson<ClassifyResult>("/api/policy/classify", { policy, inputs });

// ---- Tên hiển thị ----

export const KINDS: Record<RuleKind, { name: string; where: string; ph: string }> = {
  keyword: { name: "Từ khóa", where: "story, BRD, thiết kế", ph: "regex, ví dụ: hoàn tiền|refund" },
  path: { name: "Đường dẫn code", where: "phạm vi các bước trong design.yaml", ph: "**/billing/**" },
  table: { name: "Bảng / cột DB", where: "migration và ER diff", ph: "payment_*" },
  endpoint: { name: "Endpoint API", where: "bảng API của thiết kế", ph: "POST /payments/**" },
  label: { name: "Nhãn tác động của agent", where: "đánh giá tác động ở bước thiết kế", ph: "money-movement" },
};
export const AGENT_LABEL: Record<string, string> = { raise: "Chỉ được nâng mức", off: "Tắt, chỉ dùng rule" };
export const DEV_LOWER_LABEL: Record<string, string> = { reason: "Chỉ cần ghi lý do", never: "Không cho hạ" };

export interface ReqInfo { id: string; name: string; desc: string; param?: string; unit?: string; def?: number }
/** Thư viện yêu cầu đi kèm AISDLC (trùng RequirementCatalog của core); `sql-zero` là yêu cầu tự viết. */
export const REQ_CATALOG: ReqInfo[] = [
  { id: "rule-pos-neg", name: "Case đúng và case sai cho mỗi rule BRD", desc: "Ma trận rule × case không được có ô trống" },
  { id: "boundary", name: "Case biên cho rule gắn thẻ [biên]", desc: "Rule có thẻ [biên] trong BRD: min, max, min−1, max+1" },
  { id: "idempotent", name: "Case gửi trùng", desc: "Cùng request gửi 2 lần, kết quả và DB như gửi 1 lần" },
  { id: "concurrent", name: "Case đồng thời", desc: "Số luồng chạy song song trên cùng bản ghi", param: "threads", unit: "luồng", def: 4 },
  { id: "invariant", name: "Bất biến DB sau mọi case", desc: "Khai trong design.yaml (luồng nhẹ: cases.yaml), harness kiểm sau case (từ bước step nếu khai)" },
  { id: "coverage", name: "Coverage dòng mới tối thiểu", desc: "JaCoCo trên dòng mới của tính năng", param: "min", unit: "%", def: 80 },
];
export const SQL_ZERO = "sql-zero";
const catalog = (id: string) => REQ_CATALOG.find((r) => r.id === id);

/** Khóa nhận diện yêu cầu trong một mức: `sql-zero` có thể nhiều cái, phân biệt theo tên. */
export const reqKey = (q: PolicyReq) => (q.id === SQL_ZERO ? `${SQL_ZERO}:${String(q.name ?? "")}` : q.id);
export function reqName(q: PolicyReq): string {
  if (q.id === SQL_ZERO) return `Tự viết: ${String(q.name ?? SQL_ZERO)}`;
  return catalog(q.id)?.name ?? q.id;
}
const reqOrder = (q: PolicyReq) => {
  const i = REQ_CATALOG.findIndex((r) => r.id === q.id);
  return i < 0 ? REQ_CATALOG.length : i;
};

/** Màu pill của mức: theo vị trí trong thang (đầu ok, cuối bad, giữa warn); không biết thang thì theo tên. */
export function levelTone(level: string | null | undefined, levels?: string[] | null): "ok" | "warn" | "bad" | "muted" {
  if (!level) return "muted";
  if (levels && levels.length > 0) {
    const i = levels.indexOf(level);
    if (i < 0) return "muted";
    return i === 0 ? "ok" : i === levels.length - 1 ? "bad" : "warn";
  }
  return level === "low" ? "ok" : level === "medium" ? "warn" : level === "high" ? "bad" : "muted";
}

// ---- Sửa nháp (hàm thuần, trả bản mới) ----

const clone = <T,>(x: T): T => JSON.parse(JSON.stringify(x)) as T;

/** Bật/tắt yêu cầu ở một mức. Bật yêu cầu có tham số: lấy giá trị lớn nhất ở mức khác, không có thì mặc định. */
export function setRequirement(p: Policy, level: string, key: string, on: boolean, req?: PolicyReq): Policy {
  const out = clone(p);
  const list = (out.requirements[level] ?? []).filter((q) => reqKey(q) !== key);
  if (on) {
    let q: PolicyReq | undefined = req ? clone(req) : undefined;
    if (!q) {
      const others = Object.values(out.requirements).flat().filter((x) => reqKey(x) === key);
      const info = catalog(key);
      q = { id: key };
      if (info?.param) {
        const vals = others.map((x) => Number(x[info.param!])).filter((n) => Number.isFinite(n));
        q[info.param] = vals.length ? Math.max(...vals) : info.def;
      } else if (others[0]) {
        q = clone(others[0]);
      }
    }
    list.push(q);
    list.sort((a, b) => reqOrder(a) - reqOrder(b));
  }
  if (list.length > 0 || out.requirements[level]) out.requirements[level] = list;
  return out;
}

export function setReqParam(p: Policy, level: string, key: string, param: string, value: unknown): Policy {
  const out = clone(p);
  const q = (out.requirements[level] ?? []).find((x) => reqKey(x) === key);
  if (q) q[param] = value;
  return out;
}

/** Đặt chế độ duyệt của một cổng ở một mức. */
export function setFlow(p: Policy, level: string, unit: FlowUnit, mode: FlowMode): Policy {
  const out = clone(p);
  out.flow = out.flow ?? {};
  out.flow[level] = { ...(out.flow[level] ?? {}), [unit]: mode };
  return out;
}
export const flowOf = (p: Policy, level: string, unit: FlowUnit): FlowMode => p.flow?.[level]?.[unit] === "auto" ? "auto" : "manual";

/** Thêm hoặc thay rule theo id (`oldId` khi sửa). */
export function putRule(p: Policy, rule: PolicyRule, oldId?: string): Policy {
  const out = clone(p);
  const i = out.rules.findIndex((r) => r.id === (oldId ?? rule.id));
  if (i >= 0) out.rules[i] = clone(rule);
  else out.rules.push(clone(rule));
  return out;
}

export function removeRule(p: Policy, id: string): Policy {
  const out = clone(p);
  out.rules = out.rules.filter((r) => r.id !== id);
  return out;
}

/** Id rule mới chưa dùng: R1, R2, … */
export function nextRuleId(rules: PolicyRule[]): string {
  const ids = new Set(rules.map((r) => r.id));
  for (let n = rules.length + 1; ; n++) if (!ids.has(`R${n}`)) return `R${n}`;
}

/**
 * Áp một thay đổi của mẫu mới (`latest`) vào nháp. Không đổi `preset`: bản mẫu chỉ lên khi mọi thay đổi đã được áp
 * hoặc bỏ qua (xem {@link presetVersionFor}), để mục chưa xem không bị ẩn mất sau khi xuất bản.
 */
export function applyPresetChange(draft: Policy, latest: Policy, c: PolicyChange): Policy {
  let out = clone(draft);
  const slash = c.key.indexOf("/");
  const level = slash >= 0 ? c.key.slice(0, slash) : "";
  const rest = slash >= 0 ? c.key.slice(slash + 1) : c.key;
  const dot = rest.indexOf(".");
  const reqId = dot >= 0 ? rest.slice(0, dot) : rest;
  const latestReq = () => (latest.requirements[level] ?? []).find((q) => q.id === reqId);
  switch (c.kind) {
    case "ADD_RULE":
    case "CHANGE_RULE": {
      const r = latest.rules.find((x) => x.id === c.key);
      if (r) out = putRule(out, r);
      break;
    }
    case "REMOVE_RULE":
      out = removeRule(out, c.key);
      break;
    case "ADD_REQ": {
      const q = latestReq();
      if (q) out = setRequirement(out, level, reqKey(q), true, q);
      break;
    }
    case "REMOVE_REQ":
      out.requirements[level] = (out.requirements[level] ?? []).filter((q) => q.id !== reqId);
      break;
    case "CHANGE_PARAM": {
      const q = latestReq();
      const param = rest.slice(dot + 1);
      const mine = (out.requirements[level] ?? []).find((x) => x.id === reqId);
      if (!mine) {
        if (q) out = setRequirement(out, level, reqKey(q), true, q);
      } else if (q && q[param] !== undefined) {
        mine[param] = q[param];
      } else {
        delete mine[param];
      }
      break;
    }
    case "CHANGE_CLASSIFICATION":
      if (c.key === "agent" && (c.after === "raise" || c.after === "off")) out.classification.agent = c.after;
      else if (c.key === "devLower" && (c.after === "reason" || c.after === "never")) out.classification.devLower = c.after;
      else if (c.key === "default" && c.after) out.default = c.after;
      break;
    case "CHANGE_FLOW": {
      const [lv, unit] = c.key.split("/") as [string, FlowUnit];
      const want = latest.flow?.[lv]?.[unit];
      if (want) {
        out.flow = out.flow ?? {};
        out.flow[lv] = { ...(out.flow[lv] ?? {}), [unit]: want };
      } else if (out.flow?.[lv]) {
        delete out.flow[lv][unit];
        if (Object.keys(out.flow[lv]).length === 0) delete out.flow[lv];
      }
      break;
    }
    default:
      throw new Error(`Loại thay đổi mẫu chưa hỗ trợ: ${c.kind}`);
  }
  return out;
}

export type PresetHandled = Record<string, "applied" | "skipped">;
/** Khóa của một thay đổi mẫu trong bảng đã áp/bỏ qua. */
export const changeId = (c: PolicyChange) => `${c.kind}:${c.key}`;

/** Mọi thay đổi của mẫu mới đã được áp hoặc bỏ qua trong phiên này. */
export const allPresetHandled = (u: PresetUpdate | null | undefined, handled: PresetHandled) =>
  !!u && u.changes.every((c) => !!handled[changeId(c)]);

/** Đánh dấu bỏ qua mọi thay đổi còn lại (chưa áp, chưa bỏ qua). */
export function skipAllRemaining(u: PresetUpdate, handled: PresetHandled): PresetHandled {
  const out = { ...handled };
  for (const c of u.changes) if (!out[changeId(c)]) out[changeId(c)] = "skipped";
  return out;
}

/**
 * `preset` của nháp: lên bản mẫu mới chỉ khi mọi thay đổi đã áp hoặc bỏ qua (spec §4.3: chọn từng mục, không tự
 * ghi đè); còn lại giữ bản mẫu của chính sách đang hiệu lực.
 */
export function presetVersionFor(draft: Policy, base: Policy, u: PresetUpdate | null | undefined, handled: PresetHandled): Policy {
  const out = clone(draft);
  if (u && allPresetHandled(u, handled)) out.preset = { name: u.name, version: u.to };
  else out.preset = base.preset ? clone(base.preset) : base.preset;
  return out;
}

// ---- Mô tả thay đổi của nháp ----

const s = (v: unknown) => (v == null ? "—" : String(v));
/** So rule theo nội dung (bỏ qua thứ tự khóa JSON, note null/vắng). */
export const sameRule = (a: PolicyRule, b: PolicyRule) =>
  a.kind === b.kind && a.level === b.level && a.enabled === b.enabled && (a.note ?? "") === (b.note ?? "")
  && a.match.join("\n") === b.match.join("\n");

/** Mỗi dòng là một thay đổi giữa bản đang hiệu lực và nháp, để hiện trên thanh nháp và hộp thoại xuất bản. */
export function draftChanges(base: Policy, draft: Policy): string[] {
  const out: string[] = [];
  // rule
  const baseRules = new Map(base.rules.map((r) => [r.id, r]));
  const draftIds = new Set(draft.rules.map((r) => r.id));
  for (const r of draft.rules) {
    const b = baseRules.get(r.id);
    if (!b) {
      out.push(`${r.id}: thêm (${KINDS[r.kind]?.name ?? r.kind} ${r.match.join(", ")} → ${r.level})`);
      continue;
    }
    if (sameRule(b, r)) continue;
    const parts: string[] = [];
    if (b.enabled !== r.enabled) parts.push(r.enabled ? "bật" : "tắt");
    if (b.kind !== r.kind) parts.push(`loại ${KINDS[b.kind]?.name ?? b.kind} → ${KINDS[r.kind]?.name ?? r.kind}`);
    if (b.match.join("\n") !== r.match.join("\n")) parts.push(`mẫu khớp ${b.match.join(", ")} → ${r.match.join(", ")}`);
    if (b.level !== r.level) parts.push(`mức ${b.level} → ${r.level}`);
    if ((b.note ?? "") !== (r.note ?? "")) parts.push("sửa ghi chú");
    out.push(`${r.id}: ${parts.join("; ")}`);
  }
  for (const b of base.rules) if (!draftIds.has(b.id)) out.push(`${b.id}: xóa`);

  // yêu cầu theo mức
  const levels = [...new Set([...draft.levels, ...base.levels, ...Object.keys(base.requirements), ...Object.keys(draft.requirements)])];
  for (const level of levels) {
    const a = new Map((base.requirements[level] ?? []).map((q) => [reqKey(q), q]));
    const b = new Map((draft.requirements[level] ?? []).map((q) => [reqKey(q), q]));
    for (const [k, q] of b) {
      const old = a.get(k);
      const name = reqName(q);
      if (!old) {
        out.push(`${name}: bật ở mức ${level}`);
        continue;
      }
      const params = new Set([...Object.keys(old), ...Object.keys(q)].filter((p) => p !== "id"));
      for (const p of params) {
        if (s(old[p]) === s(q[p])) continue;
        if (q.id === SQL_ZERO) out.push(`${name} mức ${level}: đổi ${p === "sql" ? "SQL" : p}`);
        else {
          const unit = catalog(q.id)?.param === p ? catalog(q.id)?.unit : undefined;
          out.push(unit ? `${name} mức ${level}: ${s(old[p])} → ${s(q[p])} ${unit}`
            : `${name} mức ${level}: ${p} ${s(old[p])} → ${s(q[p])}`);
        }
      }
    }
    for (const [k, q] of a) if (!b.has(k)) out.push(`${reqName(q)}: tắt ở mức ${level}`);
  }

  // quy trình theo mức
  for (const level of new Set([...draft.levels, ...base.levels])) {
    for (const u of FLOW_UNITS) {
      const a = flowOf(base, level, u.id), b = flowOf(draft, level, u.id);
      if (a !== b) out.push(`Quy trình mức ${level}, cổng ${u.id}: ${a} → ${b}`);
    }
  }

  // phân loại và thang
  if (base.classification.agent !== draft.classification.agent) {
    out.push(`Đánh giá tác động của agent: ${AGENT_LABEL[base.classification.agent] ?? base.classification.agent} → ${AGENT_LABEL[draft.classification.agent] ?? draft.classification.agent}`);
  }
  if (base.classification.devLower !== draft.classification.devLower) {
    out.push(`Dev hạ mức so với rule hoặc agent: ${DEV_LOWER_LABEL[base.classification.devLower] ?? base.classification.devLower} → ${DEV_LOWER_LABEL[draft.classification.devLower] ?? draft.classification.devLower}`);
  }
  if (base.default !== draft.default) out.push(`Mức mặc định: ${base.default} → ${draft.default}`);
  if (base.levels.join("\n") !== draft.levels.join("\n")) out.push(`Thang mức: ${base.levels.join(", ")} → ${draft.levels.join(", ")}`);
  if ((base.preset?.version ?? "") !== (draft.preset?.version ?? "") && draft.preset) {
    out.push(`Mẫu ${draft.preset.name}: bản ${s(base.preset?.version)} → ${draft.preset.version}`);
  }
  return out;
}

// ---- Nháp trong localStorage (mọi truy cập bọc try/catch: trang sandbox, chế độ riêng tư) ----

const draftKey = (repoId: string) => `aisdlc.risk.draft.${repoId}`;
function storage(): Storage | null {
  try {
    return typeof localStorage === "undefined" ? null : localStorage;
  } catch {
    return null;
  }
}

export function loadDraft(repoId: string): Draft | null {
  try {
    const raw = storage()?.getItem(draftKey(repoId));
    if (!raw) return null;
    const d = JSON.parse(raw) as Draft;
    const ok = d && typeof d.baseVersion === "number" && d.policy && Array.isArray(d.policy.rules)
      && Array.isArray(d.policy.levels) && typeof d.policy.requirements === "object";
    return ok ? d : null;
  } catch {
    return null;
  }
}

export function saveDraft(repoId: string, d: Draft): void {
  try {
    storage()?.setItem(draftKey(repoId), JSON.stringify(d));
  } catch { /* không lưu được thì nháp chỉ sống trong trang */ }
}

export function clearDraft(repoId: string): void {
  try {
    storage()?.removeItem(draftKey(repoId));
  } catch { /* như trên */ }
}

/** Tách danh sách người dùng nhập: theo dấu phẩy hoặc xuống dòng. */
export const splitList = (v: string) => v.split(/[,\n]/).map((x) => x.trim()).filter(Boolean);

/** Một dòng mô tả `PolicyChange` của server (diff mẫu, lịch sử phiên bản, phần bị nới). */
export function changeText(c: PolicyChange): string {
  const v = (x: string | null) => x ?? "—";
  switch (c.kind) {
    case "ADD_RULE": return `+ rule ${c.key}: ${v(c.after)}`;
    case "REMOVE_RULE": return `− rule ${c.key}: ${v(c.before)}`;
    case "CHANGE_RULE": return `rule ${c.key}: ${v(c.before)} ⟶ ${v(c.after)}`;
    // after/before của yêu cầu là "id k=v …": giữ tham số
    case "ADD_REQ": return `+ yêu cầu ${c.key.split("/")[0]}/${c.after ?? c.key.split("/")[1]}`;
    case "REMOVE_REQ": return `− yêu cầu ${c.key.split("/")[0]}/${c.before ?? c.key.split("/")[1]}`;
    case "CHANGE_CLASSIFICATION":
      return `${c.key === "default" ? "default" : `classification.${c.key}`}: ${v(c.before)} ⟶ ${v(c.after)}`;
    case "CHANGE_FLOW": return `flow ${c.key}: ${v(c.before)} ⟶ ${v(c.after)}`;
    default: return `${c.key}: ${v(c.before)} ⟶ ${v(c.after)}`;
  }
}
