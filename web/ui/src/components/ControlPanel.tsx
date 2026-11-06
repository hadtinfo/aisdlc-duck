import { useState } from "react";
import { ApiError, sendInstruction, setPaused } from "../api";
import { canAct } from "../approve";
import { INSTRUCTION_MAX } from "../control";
import type { ControlState } from "../types";
import { useMe } from "./UserMenu";

const hhmm = (iso: string) => new Date(iso).toLocaleTimeString("vi-VN", { hour: "2-digit", minute: "2-digit" });
const fmtTime = (iso: string) => new Date(iso).toLocaleString("vi-VN");

/** Màn C, đầu cột phải: tạm dừng / tiếp tục agent và gửi chỉ dẫn. Chỉ owner/lead thao tác; người khác chỉ xem. */
export function ControlPanel({ control, owner, repoId, featureKey, onChanged }: {
  control: ControlState | null; owner: string | null; repoId: string; featureKey: string; onChanged: () => void;
}) {
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
      setError(e instanceof ApiError ? e.message : "Không kết nối được máy chủ");
    } finally {
      setBusy(false);
    }
  }

  const paused = !!control?.paused;
  const list = [...(control?.instructions ?? [])].sort((a, b) => b.id - a.id);
  return (
    <section className="control-panel" aria-label="Điều khiển agent">
      <h2>Điều khiển</h2>
      {error && <p className="error banner" role="alert">{error}</p>}
      {paused && control?.pausedBy && (
        <p className="pill warn block">Đang tạm dừng bởi {control.pausedBy}{control.pausedAt ? ` lúc ${fmtTime(control.pausedAt)}` : ""}</p>
      )}
      {actor && (
        <>
          <button type="button" disabled={busy || !control} onClick={() => run(() => setPaused(repoId, featureKey, !paused))}>
            {paused ? "Tiếp tục" : "Tạm dừng"}
          </button>
          <p className="muted small">Agent có thể ghi thêm tối đa một lần trước khi dừng.</p>
          <form className="ctl-form" onSubmit={(e) => {
            e.preventDefault();
            const t = text.trim();
            if (t) run(() => sendInstruction(repoId, featureKey, t), () => setText(""));
          }}>
            <label className="ga-label" htmlFor="ctl-text">Gửi chỉ dẫn</label>
            <textarea id="ctl-text" rows={3} maxLength={INSTRUCTION_MAX} value={text} onChange={(e) => setText(e.target.value)} />
            <span className="muted ga-count">{text.length}/{INSTRUCTION_MAX}</span>
            <p className="muted small">Gửi rồi không sửa được.</p>
            <button type="submit" disabled={busy || !text.trim()}>Gửi</button>
          </form>
        </>
      )}
      <h3>Chỉ dẫn đã gửi</h3>
      {list.length === 0 && <p className="muted small">Chưa có chỉ dẫn.</p>}
      <ul className="ctl-list">
        {list.map((i) => (
          <li key={i.id}>
            <div className="ctl-text">{i.text}</div>
            <div className="muted small">
              {i.by} · {i.deliveredAt ? `đã giao lúc ${hhmm(i.deliveredAt)}` : "chờ giao"}
              {i.source === "feedback" && <span className="tag">từ trả lại cổng</span>}
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}
