/** Kế hoạch nút duyệt trên web (R3, hàm thuần): cờ HIGH/WARN kèm theo, giả định đã/chưa đánh dấu, quyền thao tác. */
import { flagTitle, sortFlags } from "./reviewModel";
import type { GateData, ReviewCardData } from "./types";

export interface ApprovePlan {
  hashShort: string;
  flags: { code: string; level: string; title: string }[];
  unseenAssumptions: number;
  seenTexts: string[];
}

export function approvePlan(card: ReviewCardData | null, gate: GateData, seen: number[]): ApprovePlan {
  const flags = sortFlags(card?.flags ?? [])
    .filter((f) => f.level === "HIGH" || f.level === "WARN")
    .map((f) => ({ code: f.code, level: f.level, title: flagTitle(f) }));
  const items = card?.assumptions ?? [];
  const idx = [...new Set(seen)].filter((i) => Number.isInteger(i) && i >= 0 && i < items.length).sort((a, b) => a - b);
  return {
    hashShort: (gate.currentHash ?? "").slice(0, 8),
    flags,
    unseenAssumptions: items.length - idx.length,
    seenTexts: idx.map((i) => items[i].text),
  };
}

/** Chủ tính năng hoặc lead (server kiểm lại). */
export function canAct(me: { login: string; role: string }, owner: string | null): boolean {
  return me.role === "lead" || (owner != null && me.login === owner);
}

/** Nút nào hiện theo trạng thái cổng. */
export function actionsFor(state: string): { approve: boolean; reject: boolean; reopen: boolean } {
  return {
    approve: state === "AWAITING_APPROVAL",
    reject: state === "AWAITING_APPROVAL" || state === "SUBMITTED_WITH_FINDINGS",
    reopen: state === "APPROVED" || state === "STALE",
  };
}

/** Huy hiệu bản duyệt của cổng: tự duyệt theo flow (`flow:<mức>`) hoặc đã bị thay thế khi nâng mức; không có thì null. */
export function approvalBadge(gate: Pick<GateData, "approval" | "superseded">): string | null {
  const a = gate.approval;
  if (a) {
    if (a.rule?.startsWith("flow:")) return `tự duyệt (mức ${a.rule.slice("flow:".length)})`;
    return a.label?.startsWith("tự duyệt") ? a.label : null;
  }
  return gate.superseded ? "đã thay thế" : null;
}
