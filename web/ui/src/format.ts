import { t } from "./i18n";

/** Thời gian tương đối theo locale đang dùng. */
export function ago(iso: string, now: Date = new Date()): string {
  const sec = Math.max(0, Math.floor((now.getTime() - new Date(iso).getTime()) / 1000));
  if (sec < 60) return t("ago.justNow");
  const min = Math.floor(sec / 60);
  if (min < 60) return t("ago.minutes", { n: min });
  const hour = Math.floor(min / 60);
  if (hour < 24) return t("ago.hours", { n: hour });
  return t("ago.days", { n: Math.floor(hour / 24) });
}

export type BudgetLevel = "ok" | "over" | "far";

/** ok khi ≤1×, over khi ≤1,5×, far khi >1,5×; budget 0 coi như không giới hạn. */
export function budgetLevel(actual: number, budget: number): BudgetLevel {
  if (!(budget > 0) || actual <= budget) return "ok";
  return actual <= budget * 1.5 ? "over" : "far";
}

/** Dịch mã nhãn của backend theo locale đang dùng. */
export function labelText(code: string): string {
  const i = code.indexOf(":");
  const kind = i < 0 ? code : code.slice(0, i);
  const arg = i < 0 ? "" : code.slice(i + 1);
  switch (kind) {
    case "AWAITING_APPROVAL": return t("label.awaitingApproval", { arg });
    case "REJECTED": return t("label.rejected", { arg });
    case "STALE": return t("label.stale", { arg });
    case "FINDINGS": return t("label.findings", { arg });
    case "CODING": return t("label.coding", { arg });
    case "AWAITING_ACCEPT": return t("label.awaitingAccept", { arg });
    case "EVAL_FAIL": return t("label.evalFail");
    default: return code;
  }
}

/** Loại nhãn để tô màu: warn (cần người), bad (lỗi), info (đang chạy). */
export function labelTone(code: string): "warn" | "bad" | "info" {
  const kind = code.split(":")[0];
  if (kind === "REJECTED" || kind === "EVAL_FAIL" || kind === "STALE") return "bad";
  if (kind === "CODING") return "info";
  return "warn";
}
