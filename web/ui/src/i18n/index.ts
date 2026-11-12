import { useSyncExternalStore } from "react";
import { en } from "./en";
import { vi } from "./vi";

/** Ngôn ngữ giao diện. Từ điển vi là gốc; en có kiểu Record<Key, string> nên thiếu khóa là tsc báo lỗi. */
export type Locale = "vi" | "en";
export type Key = keyof typeof vi;
export type Params = Record<string, string | number>;

const dicts: Record<Locale, Record<Key, string>> = { vi, en };

export const STORAGE_KEY = "aisdlc.locale";

let current: Locale = "vi";
const listeners = new Set<() => void>();

export function isLocale(v: unknown): v is Locale {
  return v === "vi" || v === "en";
}

/** Locale đang dùng (mặc định vi). */
export function getLocale(): Locale {
  return current;
}

/** Đổi locale và báo cho mọi component dùng useT() vẽ lại. */
export function setLocale(l: Locale): void {
  if (l === current) return;
  current = l;
  if (typeof document !== "undefined") document.documentElement.lang = l;
  listeners.forEach((f) => f());
}

function subscribe(f: () => void): () => void {
  listeners.add(f);
  return () => { listeners.delete(f); };
}

/** Dịch khóa theo locale l, thay {tên} bằng params. */
export function tIn(l: Locale, key: Key, params?: Params): string {
  const s = dicts[l][key] ?? dicts.vi[key] ?? key;
  if (!params) return s;
  return s.replace(/\{(\w+)\}/g, (m, name: string) => (name in params ? String(params[name]) : m));
}

/** Dịch khóa theo locale đang dùng (dùng được ngoài component, ví dụ trong hàm định dạng thuần). */
export function t(key: Key, params?: Params): string {
  return tIn(current, key, params);
}

/** Hook: t theo locale hiện tại, vẽ lại khi đổi locale. */
export function useT(): { t: typeof t; locale: Locale } {
  const locale = useSyncExternalStore(subscribe, getLocale, getLocale);
  return { t: (key, params) => tIn(locale, key, params), locale };
}

/** Thẻ BCP 47 cho Intl. */
export function intlTag(l: Locale = current): string {
  return l === "en" ? "en-US" : "vi-VN";
}

/** Ngày giờ theo locale (Intl); chuỗi không phải thời điểm hợp lệ thì trả nguyên. */
export function formatDateTime(iso: string, l: Locale = current): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return new Intl.DateTimeFormat(intlTag(l), { dateStyle: "short", timeStyle: "short" }).format(d);
}

/** Số theo locale (Intl). */
export function formatNumber(n: number, l: Locale = current): string {
  return new Intl.NumberFormat(intlTag(l)).format(n);
}

/** navigator.language: en* → en, còn lại vi. */
export function fromNavigator(lang: string | undefined | null): Locale {
  return lang && lang.toLowerCase().startsWith("en") ? "en" : "vi";
}

/**
 * Chọn locale: lựa chọn đã lưu trên server (đã đăng nhập, khác null) → localStorage → navigator.language.
 * Hàm thuần để test; đọc trình duyệt ở initialLocale.
 */
export function pickLocale(o: { server?: string | null; stored?: string | null; navigator?: string | null }): Locale {
  if (isLocale(o.server)) return o.server;
  if (isLocale(o.stored)) return o.stored;
  return fromNavigator(o.navigator);
}

/** Lựa chọn lưu trong trình duyệt (null khi chưa có hoặc không đọc được). */
export function storedLocale(): string | null {
  try {
    return typeof localStorage !== "undefined" ? localStorage.getItem(STORAGE_KEY) : null;
  } catch {
    return null;
  }
}

export function storeLocale(l: Locale): void {
  try {
    if (typeof localStorage !== "undefined") localStorage.setItem(STORAGE_KEY, l);
  } catch { /* chế độ riêng tư / bị chặn: chỉ giữ trong phiên */ }
}

/** Locale lúc mở trang, trước khi biết người dùng: localStorage → navigator.language. */
export function initialLocale(): Locale {
  return pickLocale({ stored: storedLocale(), navigator: typeof navigator !== "undefined" ? navigator.language : null });
}
