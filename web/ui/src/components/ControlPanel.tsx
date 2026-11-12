import { useState } from "react";
import { ApiError, sendInstruction, setPaused } from "../api";
import { canAct } from "../approve";
import { INSTRUCTION_MAX } from "../control";
import { intlTag, t as tNow, useT } from "../i18n";
import type { ControlState } from "../types";
import { useMe } from "./UserMenu";

const hhmm = (iso: string) => new Date(iso).toLocaleTimeString(intlTag(), { hour: "2-digit", minute: "2-digit" });
const fmtTime = (iso: string) => new Date(iso).toLocaleString(intlTag());

/** Màn C, đầu cột phải: tạm dừng / tiếp tục agent và gửi chỉ dẫn. Chỉ owner/lead thao tác; người khác chỉ xem. */
export function ControlPanel({ control, owner, repoId, featureKey, onChanged }: {
  control: ControlState | null; owner: string | null; repoId: string; featureKey: string; onChanged: () => void;
}) {
  const { t } = useT();
  const user = useMe();
  const actor = !!user && canAct(user, owner);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function run(fn: () => Promise<unknown>, after?: () => void) {
    setBusy(true);
    setError("");
    try {
      await fn();
      after?.();
      onChanged();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : tNow("common.noServer"));
    } finally {
      setBusy(false);
    }
  }

  const paused = !!control?.paused;
  const list = [...(control?.instructions ?? [])].sort((a, b) => b.id - a.id);
  return (
    <section className="control-panel" aria-label={t("control.aria")}>
      <h2>{t("control.title")}</h2>
      {error && <p className="error banner" role="alert">{error}</p>}
      {paused && control?.pausedBy && (
        <p className="pill warn block">{control.pausedAt ? t("control.pausedByAt", { by: control.pausedBy, at: fmtTime(control.pausedAt) }) : t("control.pausedBy", { by: control.pausedBy })}</p>
      )}
      {actor && (
        <>
          <button type="button" disabled={busy || !control} onClick={() => run(() => setPaused(repoId, featureKey, !paused))}>
            {paused ? t("control.resume") : t("control.pause")}
          </button>
          <p className="muted small">{t("control.pauseNote")}</p>
          <form className="ctl-form" onSubmit={(e) => {
            e.preventDefault();
            const msg = text.trim();
            if (msg) run(() => sendInstruction(repoId, featureKey, msg), () => setText(""));
          }}>
            <label className="ga-label" htmlFor="ctl-text">{t("control.send.label")}</label>
            <textarea id="ctl-text" rows={3} maxLength={INSTRUCTION_MAX} value={text} onChange={(e) => setText(e.target.value)} />
            <span className="muted ga-count">{text.length}/{INSTRUCTION_MAX}</span>
            <p className="muted small">{t("control.send.note")}</p>
            <button type="submit" disabled={busy || !text.trim()}>{t("control.send")}</button>
          </form>
        </>
      )}
      <h3>{t("control.sent")}</h3>
      {list.length === 0 && <p className="muted small">{t("control.none")}</p>}
      <ul className="ctl-list">
        {list.map((i) => (
          <li key={i.id}>
            <div className="ctl-text">{i.text}</div>
            <div className="muted small">
              {i.by} · {i.deliveredAt ? t("control.deliveredAt", { at: hhmm(i.deliveredAt) }) : t("control.waiting")}
              {i.source === "feedback" && <span className="tag">{t("control.fromFeedback")}</span>}
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}
