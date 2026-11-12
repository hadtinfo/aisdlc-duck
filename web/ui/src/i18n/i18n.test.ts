import { afterEach, describe, expect, it, vi as vitest } from "vitest";
import { ago, labelText } from "../format";
import { en } from "./en";
import { formatDateTime, formatNumber, fromNavigator, getLocale, pickLocale, setLocale, t, tIn } from "./index";
import { vi } from "./vi";

const VI_CHARS = /[àáạảãâầấậẩẫăằắặẳẵèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]/i;
const params = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort().join(",");

afterEach(() => setLocale("vi"));

describe("từ điển", () => {
  it("en có đúng bộ khóa của vi, không rỗng", () => {
    expect(Object.keys(en).sort()).toEqual(Object.keys(vi).sort());
    for (const [k, v] of Object.entries(en)) expect(v, k).not.toBe("");
  });
  it("cùng tham số {tên} giữa vi và en", () => {
    for (const k of Object.keys(vi) as (keyof typeof vi)[]) expect(params(en[k]), k).toBe(params(vi[k]));
  });
  it("bản en không còn ký tự tiếng Việt", () => {
    for (const [k, v] of Object.entries(en)) expect(VI_CHARS.test(v), `${k}: ${v}`).toBe(false);
  });
});

describe("t", () => {
  it("mặc định vi, thay tham số", () => {
    expect(getLocale()).toBe("vi");
    expect(t("ago.minutes", { n: 5 })).toBe("5 phút trước");
  });
  it("đổi locale thì t theo en; tIn tường minh", () => {
    setLocale("en");
    expect(t("ago.minutes", { n: 5 })).toBe("5 min ago");
    expect(tIn("vi", "common.cancel")).toBe("Hủy");
  });
  it("tham số thiếu giữ nguyên chỗ {tên}", () => expect(tIn("en", "label.rejected")).toBe("Rejected {arg}"));
  it("hàm thuần theo locale đang dùng", () => {
    const now = new Date("2026-11-03T12:00:00Z");
    setLocale("en");
    expect(labelText("AWAITING_APPROVAL:BRD")).toBe("Awaiting approval BRD");
    expect(ago(new Date(now.getTime() - 2 * 86400_000).toISOString(), now)).toBe("2 days ago");
    setLocale("vi");
    expect(labelText("AWAITING_APPROVAL:BRD")).toBe("Chờ duyệt BRD");
  });
  it("setLocale báo cho người nghe chỉ khi đổi", () => {
    setLocale("vi");
    expect(getLocale()).toBe("vi");
  });
});

describe("chọn locale", () => {
  it("server (đã chọn) thắng localStorage và trình duyệt", () =>
    expect(pickLocale({ server: "en", stored: "vi", navigator: "vi-VN" })).toBe("en"));
  it("server null → localStorage", () => expect(pickLocale({ server: null, stored: "en", navigator: "vi" })).toBe("en"));
  it("không có gì → navigator.language", () => {
    expect(pickLocale({ navigator: "en-GB" })).toBe("en");
    expect(pickLocale({ navigator: "fr-FR" })).toBe("vi");
    expect(pickLocale({})).toBe("vi");
  });
  it("giá trị lạ trong localStorage bị bỏ qua", () => expect(pickLocale({ stored: "de", navigator: "EN" })).toBe("en"));
  it("fromNavigator: en* → en", () => {
    expect(fromNavigator("en")).toBe("en");
    expect(fromNavigator("vi-VN")).toBe("vi");
    expect(fromNavigator(undefined)).toBe("vi");
  });
});

describe("Intl", () => {
  it("số theo locale", () => {
    expect(formatNumber(1234.5, "en")).toBe("1,234.5");
    expect(formatNumber(1234.5, "vi")).toBe("1.234,5");
  });
  it("ngày giờ theo locale; chuỗi hỏng giữ nguyên", () => {
    const iso = "2026-11-03T12:00:00Z";
    expect(formatDateTime(iso, "en")).not.toBe(formatDateTime(iso, "vi"));
    expect(formatDateTime("không phải ngày", "en")).toBe("không phải ngày");
  });
});

describe("localStorage", () => {
  afterEach(() => vitest.unstubAllGlobals());
  it("đọc/ghi lựa chọn, lỗi truy cập thì bỏ qua", async () => {
    const { storeLocale, storedLocale, initialLocale, STORAGE_KEY } = await import("./index");
    const mem: Record<string, string> = {};
    vitest.stubGlobal("localStorage", { getItem: (k: string) => mem[k] ?? null, setItem: (k: string, v: string) => { mem[k] = v; } });
    vitest.stubGlobal("navigator", { language: "vi-VN" });
    expect(initialLocale()).toBe("vi");
    storeLocale("en");
    expect(mem[STORAGE_KEY]).toBe("en");
    expect(storedLocale()).toBe("en");
    expect(initialLocale()).toBe("en");
    vitest.stubGlobal("localStorage", { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("blocked"); } });
    expect(storedLocale()).toBeNull();
    expect(() => storeLocale("vi")).not.toThrow();
  });
});
