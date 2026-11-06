import { afterEach, describe, expect, it, vi } from "vitest";
import { resetSession } from "./api";
import {
  type Policy, type PolicyChange, type PresetUpdate, applyPresetChange, changeText, presetVersionFor, skipAllRemaining, clearDraft, draftChanges, levelTone, loadDraft, loadPolicy,
  publish, saveDraft, setFlow, setRequirement,
} from "./risk";

/** Mẫu generic như GET /api/policy trả (JSON của RiskPolicy). */
function generic(): Policy {
  return {
    schema: 1, version: 1, preset: { name: "generic", version: "1" },
    levels: ["low", "medium", "high"], default: "low",
    rules: [
      { id: "G1", kind: "label", match: ["money-movement"], level: "high", note: "Có dòng tiền", enabled: true },
      { id: "G2", kind: "label", match: ["schema-change"], level: "medium", note: "Đổi cấu trúc dữ liệu", enabled: true },
    ],
    classification: { agent: "raise", devLower: "reason" },
    requirements: {
      medium: [{ id: "rule-pos-neg" }, { id: "coverage", min: 75 }],
      high: [{ id: "rule-pos-neg" }, { id: "concurrent", threads: 8 }, { id: "coverage", min: 90 }],
    },
  };
}
const clone = <T,>(x: T): T => JSON.parse(JSON.stringify(x)) as T;

describe("draftChanges", () => {
  it("draftChanges_describesRuleEditAndParam", () => {
    const base = generic();
    const draft = clone(base);
    draft.rules[1].level = "high";
    draft.requirements.high[2].min = 85;
    expect(draftChanges(base, draft)).toEqual([
      "G2: mức medium → high",
      "Coverage dòng mới tối thiểu mức high: 90 → 85 %",
    ]);
  });

  it("thêm, tắt rule; bật/tắt yêu cầu; nguồn phân loại", () => {
    const base = generic();
    const draft = clone(base);
    draft.rules[0].enabled = false;
    draft.rules.push({ id: "R3", kind: "path", match: ["**/accounting/**"], level: "high", enabled: true });
    draft.requirements.medium = draft.requirements.medium.filter((q) => q.id !== "rule-pos-neg");
    draft.requirements.low = [{ id: "sql-zero", name: "Tổng nợ bằng tổng có", sql: "SELECT 1" }];
    draft.classification.agent = "off";
    expect(draftChanges(base, draft)).toEqual([
      "G1: tắt",
      "R3: thêm (Đường dẫn code **/accounting/** → high)",
      "Tự viết: Tổng nợ bằng tổng có: bật ở mức low",
      "Case đúng và case sai cho mỗi rule BRD: tắt ở mức medium",
      "Đánh giá tác động của agent: Chỉ được nâng mức → Tắt, chỉ dùng rule",
    ]);
  });

  it("không đổi gì thì rỗng, kể cả khi thứ tự yêu cầu khác", () => {
    const base = generic();
    const draft = clone(base);
    draft.requirements.high.reverse();
    expect(draftChanges(base, draft)).toEqual([]);
  });
});

describe("nháp trong localStorage", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("draftStorage_survivesThrowingLocalStorage", () => {
    const boom = () => { throw new Error("SecurityError"); };
    vi.stubGlobal("localStorage", { getItem: boom, setItem: boom, removeItem: boom });
    expect(() => saveDraft("a/b", { baseVersion: 1, policy: generic() })).not.toThrow();
    expect(loadDraft("a/b")).toBeNull();
    expect(() => clearDraft("a/b")).not.toThrow();
    // truy cập chính biến localStorage cũng ném (trang trong sandbox)
    vi.unstubAllGlobals();
    Object.defineProperty(globalThis, "localStorage", { configurable: true, get: boom });
    try {
      expect(() => saveDraft("a/b", { baseVersion: 1, policy: generic() })).not.toThrow();
      expect(loadDraft("a/b")).toBeNull();
    } finally {
      delete (globalThis as { localStorage?: unknown }).localStorage;
    }
  });

  it("lưu theo key aisdlc.risk.draft.<repoId>; JSON hỏng thì null", () => {
    const store = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
    });
    saveDraft("gitlab/pay", { baseVersion: 3, policy: generic() });
    expect([...store.keys()]).toEqual(["aisdlc.risk.draft.gitlab/pay"]);
    expect(loadDraft("gitlab/pay")?.baseVersion).toBe(3);
    store.set("aisdlc.risk.draft.gitlab/pay", "{hỏng");
    expect(loadDraft("gitlab/pay")).toBeNull();
    clearDraft("gitlab/pay");
    expect(store.size).toBe(0);
  });
});

describe("levelTone", () => {
  it("theo vị trí trong thang: đầu ok, cuối bad, giữa warn", () => {
    const lv = ["a", "b", "c", "d"];
    expect(lv.map((l) => levelTone(l, lv))).toEqual(["ok", "warn", "warn", "bad"]);
    expect(levelTone("x", ["x", "y"])).toBe("ok");
    expect(levelTone("y", ["x", "y"])).toBe("bad");
  });
  it("không biết thang: theo tên low/medium/high, còn lại trung tính", () => {
    expect(levelTone("low")).toBe("ok");
    expect(levelTone("medium")).toBe("warn");
    expect(levelTone("high")).toBe("bad");
    expect(levelTone("cao")).toBe("muted");
    expect(levelTone("zzz", ["a", "b"])).toBe("muted");
  });
});

describe("setRequirement", () => {
  it("bật yêu cầu có tham số lấy giá trị ở mức khác, tắt thì bỏ khỏi mức", () => {
    const p = setRequirement(generic(), "medium", "concurrent", true);
    expect(p.requirements.medium).toContainEqual({ id: "concurrent", threads: 8 });
    const q = setRequirement(p, "high", "coverage", false);
    expect(q.requirements.high.map((r) => r.id)).toEqual(["rule-pos-neg", "concurrent"]);
    expect(generic().requirements.medium).toHaveLength(2); // không sửa bản gốc
  });
});

describe("applyPresetChange", () => {
  const latest = (): Policy => {
    const p = generic();
    p.preset = { name: "generic", version: "2" };
    p.rules.push({ id: "G9", kind: "keyword", match: ["lãi suất"], level: "high", enabled: true });
    p.requirements.high[2].min = 85;
    return p;
  };
  const ch = (kind: string, key: string, after: string | null = null): PolicyChange =>
    ({ kind, key, before: null, after, loosening: false });

  it("áp rule mới, đổi tham số; không tự đổi bản mẫu", () => {
    let d = applyPresetChange(generic(), latest(), ch("ADD_RULE", "G9"));
    d = applyPresetChange(d, latest(), ch("CHANGE_PARAM", "high/coverage.min", "85"));
    expect(d.rules.map((r) => r.id)).toEqual(["G1", "G2", "G9"]);
    expect(d.requirements.high.find((r) => r.id === "coverage")).toEqual({ id: "coverage", min: 85 });
    expect(d.preset).toEqual({ name: "generic", version: "1" });
  });

  it("xóa rule, bỏ yêu cầu, đổi classification", () => {
    let d = applyPresetChange(generic(), latest(), ch("REMOVE_RULE", "G2"));
    d = applyPresetChange(d, latest(), ch("REMOVE_REQ", "medium/coverage"));
    d = applyPresetChange(d, latest(), ch("CHANGE_CLASSIFICATION", "agent", "off"));
    expect(d.rules.map((r) => r.id)).toEqual(["G1"]);
    expect(d.requirements.medium.map((r) => r.id)).toEqual(["rule-pos-neg"]);
    expect(d.classification.agent).toBe("off");
  });
});

describe("applyPresetChange flow", () => {
  const latest = (): Policy => {
    const p = generic();
    p.preset = { name: "generic", version: "2" };
    p.flow = { low: { brd: "auto", step: "auto" }, medium: { step: "auto" } };
    return p;
  };
  const ch = (key: string): PolicyChange => ({ kind: "CHANGE_FLOW", key, before: "manual", after: "auto", loosening: true });

  it("áp CHANGE_FLOW từ mẫu mới: v1→v2 có flow.low.brd auto", () => {
    let d = generic();
    for (const k of ["low/brd", "low/step", "medium/step"]) d = applyPresetChange(d, latest(), ch(k));
    expect(d.flow?.low?.brd).toBe("auto");
    expect(d.flow?.medium?.step).toBe("auto");
  });

  it("mẫu mới không còn khóa: xóa khỏi nháp, bỏ mức rỗng", () => {
    const base = generic();
    base.flow = { high: { brd: "auto" } };
    const d = applyPresetChange(base, latest(), ch("high/brd"));
    expect(d.flow?.high).toBeUndefined();
  });

  it("loại thay đổi lạ thì báo lỗi", () => {
    expect(() => applyPresetChange(generic(), latest(), { kind: "WAT", key: "x", before: null, after: null, loosening: false }))
      .toThrow();
  });
});

describe("API", () => {
  afterEach(() => { vi.unstubAllGlobals(); resetSession(); });
  const json = (status: number, body: unknown) =>
    new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

  it("loadPolicy: 404 thì null, repo được mã hóa trong query", async () => {
    const f = vi.fn(async (_p: string) => json(404, { error: "Repo chưa có chính sách" }));
    vi.stubGlobal("fetch", f);
    expect(await loadPolicy("a/b c")).toBeNull();
    expect(f.mock.calls[0][0]).toBe("/api/policy?repo=a%2Fb+c");
  });

  it("publish gửi CSRF và đúng body", async () => {
    const f = vi.fn(async (path: string, _i?: RequestInit) => path === "/api/me"
      ? json(200, { login: "dev", displayName: "Dev", role: "dev", csrf: "c-9" })
      : json(201, { version: 4, loosening: [] }));
    vi.stubGlobal("fetch", f);
    const r = await publish("a/b", 3, generic(), "lý do", true);
    expect(r.version).toBe(4);
    const [path, init] = f.mock.calls[1];
    expect(path).toBe("/api/policy/publish");
    expect(new Headers(init?.headers).get("X-CSRF-Token")).toBe("c-9");
    expect(JSON.parse(String(init?.body))).toEqual({ repo: "a/b", baseVersion: 3, policy: generic(), reason: "lý do", fromPreset: true });
  });
});

describe("changeText", () => {
  const c = (kind: string, key: string, before: string | null, after: string | null): PolicyChange =>
    ({ kind, key, before, after, loosening: false });
  it("một dòng cho mỗi loại thay đổi của server", () => {
    expect(changeText(c("ADD_RULE", "G9", null, "keyword lãi suất → high"))).toBe("+ rule G9: keyword lãi suất → high");
    expect(changeText(c("REMOVE_RULE", "G2", "label schema-change → medium", null))).toBe("− rule G2: label schema-change → medium");
    expect(changeText(c("CHANGE_RULE", "G2", "a → low", "a → high"))).toBe("rule G2: a → low ⟶ a → high");
    expect(changeText(c("ADD_REQ", "high/idempotent", null, "idempotent"))).toBe("+ yêu cầu high/idempotent");
    expect(changeText(c("REMOVE_REQ", "high/idempotent", "idempotent", null))).toBe("− yêu cầu high/idempotent");
    expect(changeText(c("ADD_REQ", "high/coverage", null, "coverage min=90"))).toBe("+ yêu cầu high/coverage min=90");
    expect(changeText(c("CHANGE_PARAM", "high/coverage.min", "90", "85"))).toBe("high/coverage.min: 90 ⟶ 85");
    expect(changeText(c("CHANGE_CLASSIFICATION", "agent", "raise", "off"))).toBe("classification.agent: raise ⟶ off");
  });
});

describe("bản mẫu trong nháp", () => {
  const u: PresetUpdate = {
    name: "generic", from: "1", to: "2",
    changes: [
      { kind: "ADD_RULE", key: "G9", before: null, after: "keyword lãi suất → high", loosening: false },
      { kind: "CHANGE_PARAM", key: "high/coverage.min", before: "90", after: "85", loosening: true },
    ],
  };

  it("presetVersionBumpsOnlyWhenAllHandled", () => {
    const base = generic();
    const one = presetVersionFor(base, base, u, { "ADD_RULE:G9": "applied" });
    expect(one.preset).toEqual({ name: "generic", version: "1" });
    const all = presetVersionFor(base, base, u, { "ADD_RULE:G9": "applied", "CHANGE_PARAM:high/coverage.min": "skipped" });
    expect(all.preset).toEqual({ name: "generic", version: "2" });
    // bỏ đánh dấu (vd. bỏ nháp) thì về bản mẫu của chính sách đang hiệu lực
    expect(presetVersionFor(all, base, u, {}).preset).toEqual({ name: "generic", version: "1" });
    expect(presetVersionFor(base, base, null, {}).preset).toEqual({ name: "generic", version: "1" });
  });

  it("skipAllRemainingBumps", () => {
    const base = generic();
    const h = skipAllRemaining(u, { "ADD_RULE:G9": "applied" });
    expect(h).toEqual({ "ADD_RULE:G9": "applied", "CHANGE_PARAM:high/coverage.min": "skipped" });
    const d = presetVersionFor(base, base, u, h);
    expect(d.preset).toEqual({ name: "generic", version: "2" });
    // nháp chỉ có bước nâng bản mẫu vẫn là một thay đổi để xuất bản
    expect(draftChanges(base, d)).toEqual(["Mẫu generic: bản 1 → 2"]);
  });
});

describe("flow", () => {
  it("setFlow_updatesDraft", () => {
    const base = generic();
    const out = setFlow(base, "low", "brd", "auto");
    expect(out.flow?.low?.brd).toBe("auto");
    expect(base.flow).toBeUndefined(); // hàm thuần
    expect(setFlow(out, "low", "brd", "manual").flow?.low?.brd).toBe("manual");
  });

  it("draftChanges_flowManualToAutoListed", () => {
    const base = generic();
    const draft = setFlow(base, "low", "step", "auto");
    expect(draftChanges(base, draft)).toEqual(["Quy trình mức low, cổng step: manual → auto"]);
    expect(draftChanges(draft, base)).toEqual(["Quy trình mức low, cổng step: auto → manual"]);
    expect(changeText({ kind: "CHANGE_FLOW", key: "low/step", before: "manual", after: "auto", loosening: true }))
      .toBe("flow low/step: manual ⟶ auto");
  });
});
