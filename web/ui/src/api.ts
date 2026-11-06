import { encodePath } from "./routes";
import type { ControlState, Resubmission } from "./types";

export interface EvalSummary { passed: boolean; casesPassed: number | null; casesTotal: number | null }
export interface CardData {
  repoId: string; branch: string; key: string; title: string; flow: "FULL" | "LITE";
  owner: string | null; column: "BRD" | "DESIGN" | "CASES" | "CODE" | "REVIEW" | "DONE";
  labels: string[]; eval: EvalSummary | null; syncedAt: string; clockSkew: boolean;
  session: "ACTIVE" | "STOPPED" | "NONE";
  /** Mức rủi ro từ risk.yaml; null khi tính năng chưa được phân loại. */
  riskLevel?: string | null;
}
export interface InboxItem { repoId: string; key: string; kind: string; text: string; command: string | null }
export interface BoardData { me: string; cards: CardData[]; inbox: InboxItem[] }
export interface BoardParams { repo?: string; owner?: string; hideDoneOlderThanDays?: number }

/** Lỗi 401 đã được chuyển hướng về /login; giữ lỗi để caller dừng xử lý. */
export class UnauthorizedError extends Error {}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, { credentials: "same-origin", ...init });
  if (res.status === 401 && path !== "/api/login") {
    session = null;
    if (typeof location !== "undefined" && location.pathname !== "/login") location.assign("/login");
    throw new UnauthorizedError("401");
  }
  if (!res.ok) throw await apiError(res);
  return (await res.json()) as T;
}

/** GET JSON bằng phiên; lỗi → ApiError (401 đã chuyển về /login). */
export function getJson<T>(path: string): Promise<T> {
  return request<T>(path);
}

/** Lỗi API có HTTP status; message là câu tiếng Việt server trả trong `{error}` (không có thì câu chung). */
export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function apiError(res: Response): Promise<ApiError> {
  let msg = "";
  try {
    const b = (await res.json()) as { error?: unknown };
    if (typeof b?.error === "string") msg = b.error;
  } catch { /* body không phải JSON */ }
  return new ApiError(res.status, msg || `Lỗi máy chủ (HTTP ${res.status})`);
}

export interface Me { login: string; displayName: string; role: "dev" | "lead"; csrf: string | null }

/** Phiên trong bộ nhớ (CSRF không lưu localStorage); mất khi tải lại trang thì lấy lại qua GET /api/me. */
let session: Me | null = null;
let pending: Promise<Me> | null = null;

export function resetSession(): void {
  session = null;
  pending = null;
}

/** Người đang đăng nhập (gọi chung một request khi nhiều nơi cùng hỏi). */
export function me(): Promise<Me> {
  if (session) return Promise.resolve(session);
  if (!pending) {
    pending = request<Me>("/api/me")
      .then((m) => { session = m; return m; })
      .finally(() => { pending = null; });
  }
  return pending;
}

/** POST JSON bằng phiên, kèm header X-CSRF-Token. Lỗi 400/403/409 → ApiError mang câu của server. */
export async function postJson<T>(path: string, body: unknown): Promise<T> {
  const csrf = (await me()).csrf ?? "";
  return request<T>(path, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
    body: JSON.stringify(body),
  });
}

/** Chuỗi query của /api/board. hideDoneOlderThanDays=0 nghĩa là hiện tất cả (backend mặc định 7 khi vắng). */
export function boardQuery(params: BoardParams = {}): string {
  const q = new URLSearchParams();
  if (params.repo) q.set("repo", params.repo);
  if (params.owner) q.set("owner", params.owner);
  if (params.hideDoneOlderThanDays != null) q.set("hideDoneOlderThanDays", String(params.hideDoneOlderThanDays));
  const s = q.toString();
  return s ? `?${s}` : "";
}

export function getBoard(params: BoardParams = {}): Promise<BoardData> {
  return request<BoardData>(`/api/board${boardQuery(params)}`);
}

/** Gọi nhẹ để biết phiên còn hợp lệ không: 401 thì chuyển về /login và ném UnauthorizedError. */
export async function probeAuth(): Promise<void> {
  await request("/api/board?hideDoneOlderThanDays=7");
}

// Task 7 xác định kiểu chi tiết; ở đây giữ lỏng.
export function getFeature(repoId: string, key: string): Promise<unknown> {
  return request(`/api/features/${encodePath(repoId, key)}`);
}

export function getEvents(repoId: string, key: string, limit = 200): Promise<unknown[]> {
  return request(`/api/features/${encodePath(repoId, key)}/events?limit=${limit}`);
}

/** Trả true nếu đăng nhập được (lưu phiên + CSRF), false nếu token sai (401). */
export async function login(token: string): Promise<boolean> {
  resetSession();
  const res = await fetch("/api/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token }),
    credentials: "same-origin",
  });
  if (res.status === 401) return false;
  if (!res.ok) throw await apiError(res);
  session = (await res.json()) as Me;
  return true;
}

/** Đăng xuất cần CSRF của phiên; lỗi (phiên đã hết) vẫn coi như đã đăng xuất. */
export async function logout(): Promise<void> {
  try {
    await postJson("/api/logout", {});
  } catch { /* phiên đã hết hạn hoặc mạng lỗi: vẫn về trang đăng nhập */ }
  resetSession();
}

// ---- Token cá nhân (/me) ----
export interface TokenInfo {
  id: number; kind: "login" | "ingest" | "approve" | "ci" | string; label: string | null;
  createdAt: string | null; lastUsedAt: string | null; revokedAt: string | null;
}
export const listTokens = () => request<TokenInfo[]>("/api/me/tokens");
export const createToken = (kind: "ingest" | "approve", label: string) =>
  postJson<{ id: number; token: string }>("/api/me/tokens", { kind, label });
export const revokeToken = (id: number) => postJson<TokenInfo>(`/api/me/tokens/${id}/revoke`, {});

// ---- Chế độ duyệt theo repo (/repos) ----
export interface RepoMode { id: string; approvals: "local" | "web" | string; setBy: string | null; setAt: string | null }
export const listRepos = () => request<RepoMode[]>("/api/repos");
export const setRepoMode = (repo: string, approvals: "local" | "web") =>
  postJson<RepoMode>("/api/repos/mode", { repo, approvals });

// ---- Điều khiển (màn C) và diff giữa các lần nộp (trang B) ----
export const getControl = (repoId: string, key: string) =>
  request<ControlState>(`/api/features/${encodePath(repoId, key)}/control`);
export const getResubmission = (repoId: string, key: string, gate: string) =>
  request<Resubmission>(`/api/features/${encodePath(repoId, key)}/resubmission?gate=${encodeURIComponent(gate)}`);
export const setPaused = (repo: string, feature: string, paused: boolean) =>
  postJson("/api/control/pause", { repo, feature, paused });
export const sendInstruction = (repo: string, feature: string, text: string) =>
  postJson("/api/control/instructions", { repo, feature, text });
/** ACCEPT_STEP gửi `endTree` của bước trang đang hiện: server từ chối (409) nếu bước đã đổi từ lúc dev xem. */
export const sendAction = (repo: string, feature: string, kind: "ACCEPT_STEP" | "ALLOW_SCOPE", step: string,
  opts: { path?: string; endTree?: string | null } = {}) =>
  postJson("/api/control/actions", {
    repo, feature, kind, step, ...(opts.path ? { path: opts.path } : {}), ...(opts.endTree ? { endTree: opts.endTree } : {}),
  });
