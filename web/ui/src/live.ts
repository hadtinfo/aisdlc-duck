/** Dòng event do backend trả (GET /events và SSE `event`). */
export interface LiveEvent {
  id: number; type: string; at: string; step: string | null; path: string | null;
  outOfScope: boolean; sender: string | null; detail: string | null;
}

export type SessionState = "ACTIVE" | "STOPPED" | "NONE";
export interface StepEdit { path: string; at: string; outOfScope: boolean }

const ACTIVE_MS = 30 * 60_000;

/**
 * Cùng quy tắc với server: ACTIVE khi SESSION_START/FILE_EDIT cuối cùng mới hơn 30 phút
 * và không có SESSION_STOP nào sau nó; STOPPED khi đã có event phiên; NONE khi chưa có.
 */
export function sessionState(events: LiveEvent[], now: Date): SessionState {
  let up = -Infinity, stop = -Infinity, any = false;
  for (const e of events) {
    const t = Date.parse(e.at);
    if (e.type === "SESSION_START" || e.type === "FILE_EDIT") { up = Math.max(up, t); any = true; }
    else if (e.type === "SESSION_STOP") { stop = Math.max(stop, t); any = true; }
  }
  if (!any) return "NONE";
  return up > now.getTime() - ACTIVE_MS && !(stop > up) ? "ACTIVE" : "STOPPED";
}

/** FILE_EDIT theo bước; mỗi path chỉ giữ lần sửa mới nhất, danh sách mới nhất trước. */
export function editsByStep(events: LiveEvent[]): Map<string, StepEdit[]> {
  const latest = new Map<string, Map<string, StepEdit>>();
  for (const e of events) {
    if (e.type !== "FILE_EDIT" || !e.step || !e.path) continue;
    const byPath = latest.get(e.step) ?? new Map<string, StepEdit>();
    latest.set(e.step, byPath);
    const cur = byPath.get(e.path);
    if (!cur || Date.parse(e.at) >= Date.parse(cur.at)) byPath.set(e.path, { path: e.path, at: e.at, outOfScope: e.outOfScope });
  }
  const out = new Map<string, StepEdit[]>();
  for (const [step, byPath] of latest) {
    out.set(step, [...byPath.values()].sort((a, b) => Date.parse(b.at) - Date.parse(a.at)));
  }
  return out;
}

/** Chèn event mới (từ SSE) lên đầu, bỏ trùng id; giữ tối đa `max` dòng. */
export function mergeEvent(events: LiveEvent[], e: LiveEvent, max = 500): LiveEvent[] {
  if (events.some((x) => x.id === e.id)) return events;
  return [e, ...events].slice(0, max);
}
