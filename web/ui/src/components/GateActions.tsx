import { useState } from "react";
import { ApiError, postJson } from "../api";
import { actionsFor, approvePlan } from "../approve";
import type { GateData, ReviewCardData } from "../types";

const MAX_COMMENT = 2000;
const PATHS_OPEN = 8;

type Mode = "idle" | "approve" | "reject" | "reopen";

/**
 * Nút Duyệt / Trả lại / Mở lại của cổng (chế độ web, người có quyền). Duyệt qua hộp xác nhận nội tuyến liệt kê
 * cờ HIGH/WARN, số giả định chưa đánh dấu và các file đã nộp (R3). Lỗi hiện nguyên câu server trả.
 */
export function GateActions({ gate, card, seen, submittedPaths, repoId, featureKey, onDone }: {
  gate: GateData; card: ReviewCardData | null; seen: number[]; submittedPaths: string[];
  repoId: string; featureKey: string; onDone: () => void;
}) {
  const [mode, setMode] = useState<Mode>("idle");
  const [comment, setComment] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const can = actionsFor(gate.state);
  const plan = approvePlan(card, gate, seen);

  function open(m: Mode) {
    setMode(m);
    setComment("");
    setError("");
  }

  async function send(path: string, body: Record<string, unknown>) {
    setBusy(true);
    setError("");
    try {
      await postJson(path, { repo: repoId, feature: featureKey, gate: gate.gate, ...body });
      setMode("idle");
      setComment("");
      onDone();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Không kết nối được máy chủ");
    } finally {
      setBusy(false);
    }
  }

  const approve = () => send("/api/gates/approve", {
    hash: gate.currentHash,
    flags: plan.flags.map(({ code, level }) => ({ code, level })),
    assumptionsSeen: plan.seenTexts,
  });

  if (!can.approve && !can.reject && !can.reopen) return null;

  return (
    <div className="gate-actions">
      {mode === "idle" && (
        <div className="ga-buttons">
          {can.approve && (
            <button type="button" onClick={() => open("approve")}>
              Duyệt <span className="ga-hash">@ {plan.hashShort}</span>
            </button>
          )}
          {can.reject && <button type="button" className="secondary" onClick={() => open("reject")}>Trả lại</button>}
          {can.reopen && <button type="button" className="secondary" onClick={() => open("reopen")}>Mở lại</button>}
        </div>
      )}

      {mode === "approve" && (
        <div className="ga-confirm" role="group" aria-label="Xác nhận duyệt">
          <p className="ga-q">Duyệt {gate.gate} @ <span className="mono">{plan.hashShort}</span>?</p>
          {plan.flags.length > 0 && (
            <ul className="ga-flags">
              {plan.flags.map((f, i) => (
                <li key={`${f.code}-${i}`} className={f.level === "HIGH" ? "bad" : "warn"}>
                  <span className="lvl">{f.level}</span> {f.title} <code>{f.code}</code>
                </li>
              ))}
            </ul>
          )}
          {plan.unseenAssumptions > 0 && (
            <p className="ga-unseen">Còn {plan.unseenAssumptions} giả định chưa đánh dấu đã xem</p>
          )}
          {submittedPaths.length > 0 && <SubmittedPaths paths={submittedPaths} />}
          <div className="ga-buttons">
            <button type="button" disabled={busy} onClick={approve}>Xác nhận duyệt</button>
            <button type="button" className="secondary" disabled={busy} onClick={() => open("idle")}>Hủy</button>
          </div>
        </div>
      )}

      {(mode === "reject" || mode === "reopen") && (
        <form className="ga-confirm" onSubmit={(e) => {
          e.preventDefault();
          send(mode === "reject" ? "/api/gates/reject" : "/api/gates/reopen", { comment: comment.trim() });
        }}>
          <label className="ga-label" htmlFor="ga-comment">
            {mode === "reject" ? "Lý do trả lại (bắt buộc)" : "Ghi chú mở lại (tùy chọn)"}
          </label>
          <textarea id="ga-comment" rows={4} maxLength={MAX_COMMENT} value={comment} autoFocus
            onChange={(e) => setComment(e.target.value)} />
          <span className="muted ga-count">{comment.length}/{MAX_COMMENT}</span>
          <div className="ga-buttons">
            <button type="submit" disabled={busy || (mode === "reject" && !comment.trim())}>
              {mode === "reject" ? "Trả lại" : "Mở lại"}
            </button>
            <button type="button" className="secondary" disabled={busy} onClick={() => open("idle")}>Hủy</button>
          </div>
        </form>
      )}

      {error && <p className="error" role="alert">{error}</p>}
    </div>
  );
}

/** Các file mà hash đang duyệt bao phủ; nhiều hơn 8 thì thu gọn. */
function SubmittedPaths({ paths }: { paths: string[] }) {
  const list = <ul className="ga-paths">{paths.map((p) => <li key={p}><code>{p}</code></li>)}</ul>;
  if (paths.length <= PATHS_OPEN) {
    return <div className="ga-files"><p className="ga-sub">Các file đã nộp</p>{list}</div>;
  }
  return (
    <details className="ga-files">
      <summary className="ga-sub">Các file đã nộp ({paths.length})</summary>
      {list}
    </details>
  );
}
