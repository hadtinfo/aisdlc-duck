import { useState } from "react";
import { hasSession, saveMyLocale } from "../api";
import { Locale, setLocale, storeLocale, useT } from "../i18n";

/**
 * Bộ chọn EN/VI. Luôn đổi ngay giao diện và nhớ trong trình duyệt; đã đăng nhập (persist) thì lưu thêm lên server
 * (PUT /api/me/locale) để chữ do server sinh và lần đăng nhập sau cũng theo lựa chọn này.
 */
export function LocaleSwitch({ persist = false }: { persist?: boolean }) {
  const { t, locale } = useT();
  const [error, setError] = useState("");

  async function choose(l: Locale) {
    if (l === locale) return;
    setLocale(l);
    storeLocale(l);
    setError("");
    if (persist && hasSession()) {
      try {
        await saveMyLocale(l);
      } catch {
        setError(t("locale.saveFailed"));
      }
    }
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
