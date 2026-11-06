/** Thời gian tương đối bằng tiếng Việt. */
export function ago(iso: string, now: Date = new Date()): string {
  const sec = Math.max(0, Math.floor((now.getTime() - new Date(iso).getTime()) / 1000));
  if (sec < 60) return "vừa xong";
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min} phút trước`;
  const hour = Math.floor(min / 60);
  if (hour < 24) return `${hour} giờ trước`;
  return `${Math.floor(hour / 24)} ngày trước`;
}

export type BudgetLevel = "ok" | "over" | "far";

/** ok khi ≤1×, over khi ≤1,5×, far khi >1,5×; budget 0 coi như không giới hạn. */
export function budgetLevel(actual: number, budget: number): BudgetLevel {
  if (!(budget > 0) || actual <= budget) return "ok";
  return actual <= budget * 1.5 ? "over" : "far";
}

/** Dịch mã nhãn của backend sang tiếng Việt. */
export function labelText(code: string): string {
  const i = code.indexOf(":");
  const kind = i < 0 ? code : code.slice(0, i);
  const arg = i < 0 ? "" : code.slice(i + 1);
  switch (kind) {
    case "AWAITING_APPROVAL": return `Chờ duyệt ${arg}`;
    case "REJECTED": return `Bị trả lại ${arg}`;
    case "STALE": return `Đã đổi sau duyệt (STALE) ${arg}`;
    case "FINDINGS": return `Có phát hiện ${arg}`;
    case "CODING": return `Đang code ${arg}`;
    case "AWAITING_ACCEPT": return `Chờ accept ${arg}`;
    case "EVAL_FAIL": return "Eval không đạt";
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
