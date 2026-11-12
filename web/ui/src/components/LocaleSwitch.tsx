import { useState } from "react";
import { hasSession, saveMyLocale } from "../api";
import { Locale, setLocale, storeLocale, tIn, useT } from "../i18n";

/**
 * Áp lựa chọn ngôn ngữ: đổi giao diện và nhớ trong trình duyệt; persist + đã đăng nhập thì lưu lên server. Server
 * lỗi thì trả về lựa chọn cũ (server vẫn giữ nó, lần tải /api/me sau cũng về đó) và trả câu lỗi theo ngôn ngữ cũ
 * (ngôn ngữ đang dùng); thành công trả "".
 */
export async function applyLocaleChoice(l: Locale, prev: Locale, persist: boolean): Promise<string> {
  setLocale(l);
  storeLocale(l);
  if (!persist || !hasSession()) return "";
  try {
    await saveMyLocale(l);
    return "";
  } catch {
    setLocale(prev);
    storeLocale(prev);
    return tIn(prev, "locale.saveFailed");
  }
}

/**
 * Bộ chọn EN/VI. Luôn đổi ngay giao diện và nhớ trong trình duyệt; đã đăng nhập (persist) thì lưu thêm lên server
 * (PUT /api/me/locale) để chữ do server sinh và lần đăng nhập sau cũng theo lựa chọn này.
 */
export function LocaleSwitch({ persist = false }: { persist?: boolean }) {
  const { t, locale } = useT();
  const [error, setError] = useState("");

  async function choose(l: Locale) {
    if (l === locale) return;
    setError("");
    setError(await applyLocaleChoice(l, locale, persist));
  }

  return (
    <span className="locale-switch" role="group" aria-label={t("locale.label")}>
      {(["en", "vi"] as const).map((l) => (
        <button key={l} type="button" className={l === locale ? "ghost active" : "ghost"} aria-pressed={l === locale}
          lang={l} onClick={() => void choose(l)}>
          {l.toUpperCase()}
        </button>
      ))}
      {error && <span className="error" role="alert">{error}</span>}
    </span>
  );
}
