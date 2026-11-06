import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UnauthorizedError } from "./api";
import { subscribe } from "./sse";

const okProbe = () => Promise.resolve();

class FakeES {
  static all: FakeES[] = [];
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  listeners: Record<string, (e: { data: string }) => void> = {};
  constructor(public url: string) { FakeES.all.push(this); }
  addEventListener(n: string, f: (e: { data: string }) => void) { this.listeners[n] = f; }
  close() { this.closed = true; }
}

let delays: number[];
beforeEach(() => {
  FakeES.all = [];
  delays = [];
  vi.useFakeTimers();
  vi.stubGlobal("EventSource", FakeES);
  const real = globalThis.setTimeout;
  vi.spyOn(globalThis, "setTimeout").mockImplementation(((f: () => void, ms?: number) => {
    delays.push(ms ?? 0);
    return real(f, ms);
  }) as typeof setTimeout);
});
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

const last = () => FakeES.all[FakeES.all.length - 1];

describe("subscribe", () => {
  it("lỗi liên tiếp thì backoff 1000, 2000, 4000", () => {
    subscribe("/api/stream", () => {}, () => {}, okProbe);
    for (let i = 0; i < 3; i++) {
      last().onerror!();
      expect(last().closed).toBe(true);
      vi.advanceTimersByTime(30_000);
    }
    expect(delays).toEqual([1000, 2000, 4000]);
    expect(FakeES.all).toHaveLength(4);
  });

  it("backoff tối đa 30 giây", () => {
    subscribe("/api/stream", () => {}, () => {}, okProbe);
    for (let i = 0; i < 8; i++) { last().onerror!(); vi.advanceTimersByTime(60_000); }
    expect(Math.max(...delays)).toBe(30_000);
  });

  it("lần mở đầu không reset, mở lại sau lỗi thì onReset đúng một lần", () => {
    const onReset = vi.fn();
    subscribe("/api/stream", () => {}, onReset, okProbe);
    last().onopen!();
    expect(onReset).not.toHaveBeenCalled();
    last().onerror!();
    vi.advanceTimersByTime(1000);
    last().onopen!();
    expect(onReset).toHaveBeenCalledTimes(1);
  });

  it("chuyển tiếp sự kiện có tên tới onMessage", () => {
    const got: [string, string][] = [];
    subscribe("/api/stream", (t, d) => got.push([t, d]), () => {}, okProbe);
    last().listeners["card"]({ data: '{"key":"A"}' });
    expect(got).toEqual([["card", '{"key":"A"}']]);
  });

  it("hủy đăng ký thì đóng và không nối lại", () => {
    const stop = subscribe("/api/stream", () => {}, () => {}, okProbe);
    last().onerror!();
    stop();
    vi.advanceTimersByTime(60_000);
    expect(FakeES.all).toHaveLength(1);
    expect(last().closed).toBe(true);
  });

  it("lỗi mà probe trả 401 thì dừng nối lại", async () => {
    const probe = vi.fn(() => Promise.reject(new UnauthorizedError("401")));
    subscribe("/api/stream", () => {}, () => {}, probe);
    last().onerror!();
    await Promise.resolve();
    await Promise.resolve();
    vi.advanceTimersByTime(60_000);
    expect(probe).toHaveBeenCalledTimes(1);
    expect(FakeES.all).toHaveLength(1);
  });

  it("probe lỗi khác 401 thì vẫn nối lại", async () => {
    subscribe("/api/stream", () => {}, () => {}, () => Promise.reject(new Error("HTTP 500")));
    last().onerror!();
    await Promise.resolve();
    await Promise.resolve();
    vi.advanceTimersByTime(1000);
    expect(FakeES.all).toHaveLength(2);
  });
});
