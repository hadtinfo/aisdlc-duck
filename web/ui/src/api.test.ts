import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, boardQuery, login, logout, me, postJson, resetSession } from "./api";

describe("boardQuery", () => {
  it("bỏ ẩn thì gửi 0, không bỏ param", () => expect(boardQuery({ hideDoneOlderThanDays: 0 })).toBe("?hideDoneOlderThanDays=0"));
  it("ẩn quá 7 ngày", () => expect(boardQuery({ hideDoneOlderThanDays: 7 })).toBe("?hideDoneOlderThanDays=7"));
  it("không tham số thì rỗng", () => expect(boardQuery()).toBe(""));
  it("repo và owner được mã hóa", () => expect(boardQuery({ repo: "a/b", owner: "x" })).toBe("?repo=a%2Fb&owner=x"));
});

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const ME = { login: "dev", displayName: "Dev", role: "dev", csrf: "c-1" };
async function failure(p: Promise<unknown>): Promise<ApiError> {
  try { await p; } catch (e) { return e as ApiError; }
  throw new Error("mong đợi lỗi");
}
const headerOf = (init: RequestInit | undefined, name: string) => new Headers(init?.headers).get(name);

describe("phiên + CSRF", () => {
  afterEach(() => { vi.unstubAllGlobals(); resetSession(); });

  it("postJson gửi X-CSRF-Token lấy từ GET /api/me khi chưa có phiên trong bộ nhớ", async () => {
    const fetchMock = vi.fn(async (path: string, _init?: RequestInit) =>
      path === "/api/me" ? json(200, ME) : json(200, { ok: true }));
    vi.stubGlobal("fetch", fetchMock);
    const r = await postJson<{ ok: boolean }>("/api/gates/approve", { a: 1 });
    expect(r.ok).toBe(true);
    const [path, init] = fetchMock.mock.calls[1];
    expect(path).toBe("/api/gates/approve");
    expect(init?.method).toBe("POST");
    expect(headerOf(init, "X-CSRF-Token")).toBe("c-1");
    expect(headerOf(init, "Content-Type")).toBe("application/json");
    expect(init?.body).toBe(JSON.stringify({ a: 1 }));
  });

  it("login lưu CSRF trả về; postJson sau đó không gọi lại /api/me", async () => {
    const fetchMock = vi.fn(async (path: string, _init?: RequestInit) =>
      path === "/api/login" ? json(200, { ...ME, csrf: "c-login" }) : json(200, {}));
    vi.stubGlobal("fetch", fetchMock);
    expect(await login("t")).toBe(true);
    await postJson("/api/me/tokens", { kind: "ingest", label: "x" });
    expect(fetchMock.mock.calls.map((c) => c[0])).toEqual(["/api/login", "/api/me/tokens"]);
    expect(headerOf(fetchMock.mock.calls[1][1], "X-CSRF-Token")).toBe("c-login");
  });

  it("409 → ApiError mang nguyên câu tiếng Việt của server", async () => {
    vi.stubGlobal("fetch", vi.fn(async (path: string) =>
      path === "/api/me" ? json(200, ME) : json(409, { error: "Nội dung đã đổi kể từ lúc nộp" })));
    const err = await failure(postJson("/api/gates/approve", {}));
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(409);
    expect(err.message).toBe("Nội dung đã đổi kể từ lúc nộp");
  });

  it("400 không có body JSON → ApiError với câu chung", async () => {
    vi.stubGlobal("fetch", vi.fn(async (path: string) =>
      path === "/api/me" ? json(200, ME) : new Response("oops", { status: 400 })));
    const err = await failure(postJson("/api/x", {}));
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(400);
    expect(err.message).toBe("Lỗi máy chủ (HTTP 400)");
  });

  it("logout gửi X-CSRF-Token", async () => {
    const fetchMock = vi.fn(async (path: string, _init?: RequestInit) =>
      path === "/api/me" ? json(200, ME) : json(200, { ok: true }));
    vi.stubGlobal("fetch", fetchMock);
    await logout();
    const call = fetchMock.mock.calls.find((c) => c[0] === "/api/logout");
    expect(headerOf(call?.[1], "X-CSRF-Token")).toBe("c-1");
  });

  it("me() trả người dùng và vai trò", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json(200, { ...ME, role: "lead" })));
    expect(await me()).toEqual({ ...ME, role: "lead" });
  });
});
