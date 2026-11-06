/** Hàm thuần cho điều khiển (màn C) và lệnh bước/file (trang B). */
import type { ControlAction, FeatureEvent, StepData } from "./types";

export type ActionStatus = "PENDING" | "APPLIED" | "STALE" | "EXPIRED";
export const INSTRUCTION_MAX = 2000;

export function actionLabel(kind: "ACCEPT_STEP" | "ALLOW_SCOPE", status: ActionStatus, reason?: string | null): string {
  switch (status) {
    case "PENDING": return kind === "ACCEPT_STEP" ? "Đã accept, chờ áp dụng" : "Đã cho phép, chờ áp dụng";
    case "APPLIED": return "Đã áp dụng";
    case "STALE": return reason ? `Hết hiệu lực: ${reason} — bấm lại` : "Hết hiệu lực — bấm lại";
    case "EXPIRED": return "Hết hạn (7 ngày) — bấm lại";
  }
}

/**
 * Nhãn trạng thái lệnh Accept của bước: khi bước chờ dev (mọi trạng thái lệnh), và cả khi bước đã ACCEPTED nhờ chính
 * lệnh đó (APPLIED) — snapshot ACCEPTED thường tới cùng lúc với kết quả nên không thế thì "Đã áp dụng" không bao giờ hiện.
 */
export function acceptStatusShown(stepStatus: string, acc: ControlAction | null): boolean {
  if (!acc) return false;
  return stepStatus === "AWAITING_DEV" || (stepStatus === "ACCEPTED" && acc.status === "APPLIED");
}

/** record.check.outOfScope ∪ path của SCOPE_DENIED cùng bước; bỏ trùng, giữ thứ tự xuất hiện. */
export function outOfScopeFiles(step: StepData, events: FeatureEvent[]): string[] {
  const out = new Set<string>(step.record?.check?.outOfScope ?? []);
  for (const e of events) if (e.type === "SCOPE_DENIED" && e.step === step.id && e.path) out.add(e.path);
  return [...out];
}

/** Lệnh mới nhất (id lớn nhất) đúng (kind, step, path). */
export function latestAction(actions: ControlAction[], kind: string, step: string, path?: string): ControlAction | null {
  let best: ControlAction | null = null;
  for (const a of actions) {
    if (a.kind !== kind || a.step !== step || (a.path ?? undefined) !== (path ?? undefined)) continue;
    if (!best || a.id > best.id) best = a;
  }
  return best;
}

export function feedbackLabel(s: "RELATED_CHANGED" | "NO_CHANGE_SEEN" | "UNMATCHED"): string {
  switch (s) {
    case "RELATED_CHANGED": return "có thay đổi liên quan";
    case "NO_CHANGE_SEEN": return "chưa thấy thay đổi";
    case "UNMATCHED": return "không rõ mục nào";
  }
}

/** Event SSE nào cần tải lại /control: mọi loại trừ FILE_EDIT (nhiều và không đổi trạng thái điều khiển). */
export function eventReloadsControl(type: string): boolean {
  return type !== "FILE_EDIT";
}

/**
 * Bản sao luật `PendingActions.validScopePath` (core Java, server dùng cùng luật): đường dẫn file cụ thể, tương đối repo,
 * ≤ 500 ký tự, không glob `* ? [ ] { } !`, không `:`, `\\`, `/` hay `./` đầu, không đoạn rỗng/`.`/`..`, không `.aisdlc/`.
 * Sai luật thì nút "Cho phép" không dùng được (server trả 400): trang B chỉ lệnh terminal.
 */
export function validScopePath(p: string | null | undefined): boolean {
  if (!p || p.trim() === "" || p.length > 500 || p.startsWith("/") || p.startsWith("./") || p.includes("\\")) return false;
  for (const ch of p) if ("*?[]{}!:".includes(ch) || ch === "\u0000") return false;
  const segs = p.split("/");
  if (segs.some((s) => s === "" || s === "." || s === "..")) return false;
  return segs[0] !== ".aisdlc";
}
