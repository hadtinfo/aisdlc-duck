import { type KeyboardEvent, useState } from "react";
import { ApiError, postJson } from "../api";
import { actionsFor, approvePlan } from "../approve";
import { t as tNow, useT } from "../i18n";
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
  const { t } = useT();
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
      setError(e instanceof ApiError ? e.message : tNow("common.noServer"));
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

  // Esc đóng hộp xác nhận/biểu mẫu đang mở (trừ lúc đang gửi), như nút Hủy.
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Escape" && mode !== "idle" && !busy) { e.stopPropagation(); open("idle"); }
  };

  return (
    <div className="gate-actions">
      {mode === "idle" && (
        <div className="ga-buttons">
          {can.approve && (
            <button type="button" onClick={() => open("approve")}>
              {t("gate.approve")} <span className="ga-hash">@ {plan.hashShort}</span>
            </button>
          )}
          {can.reject && <button type="button" className="secondary" onClick={() => open("reject")}>{t("gate.reject")}</button>}
          {can.reopen && <button type="button" className="secondary" onClick={() => open("reopen")}>{t("gate.reopen")}</button>}
        </div>
      )}

      {mode === "approve" && (
        <div className="ga-confirm" role="group" aria-label={t("gate.confirmApprove")} onKeyDown={onKeyDown}>
          <p className="ga-q">{t("gate.approve")} {gate.gate} @ <span className="mono">{plan.hashShort}</span>?</p>
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
            <p className="ga-unseen">{t("gate.unseen", { n: plan.unseenAssumptions })}</p>
          )}
          {submittedPaths.length > 0 && <SubmittedPaths paths={submittedPaths} />}
          <div className="ga-buttons">
            <button type="button" disabled={busy} onClick={approve}>{t("gate.confirmApprove")}</button>
            <button type="button" className="secondary" disabled={busy} autoFocus onClick={() => open("idle")}>{t("common.cancel")}</button>
          </div>
        </div>
      )}

      {(mode === "reject" || mode === "reopen") && (
        <form className="ga-confirm" onKeyDown={onKeyDown} onSubmit={(e) => {
          e.preventDefault();
          send(mode === "reject" ? "/api/gates/reject" : "/api/gates/reopen", { comment: comment.trim() });
        }}>
          <label className="ga-label" htmlFor="ga-comment">
            {mode === "reject" ? t("gate.rejectReason") : t("gate.reopenNote")}
          </label>
          <textarea id="ga-comment" rows={4} maxLength={MAX_COMMENT} value={comment} autoFocus
            onChange={(e) => setComment(e.target.value)} />
          <span className="muted ga-count">{comment.length}/{MAX_COMMENT}</span>
          <div className="ga-buttons">
            <button type="submit" disabled={busy || (mode === "reject" && !comment.trim())}>
              {mode === "reject" ? t("gate.reject") : t("gate.reopen")}
            </button>
            <button type="button" className="secondary" disabled={busy} onClick={() => open("idle")}>{t("common.cancel")}</button>
          </div>
        </form>
      )}

      {error && <p className="error" role="alert">{error}</p>}
    </div>
  );
}

/** Các file mà hash đang duyệt bao phủ; nhiều hơn 8 thì thu gọn. */
function SubmittedPaths({ paths }: { paths: string[] }) {
  const { t } = useT();
  const list = <ul className="ga-paths">{paths.map((p) => <li key={p}><code>{p}</code></li>)}</ul>;
  if (paths.length <= PATHS_OPEN) {
    return <div className="ga-files"><p className="ga-sub">{t("gate.submittedFiles")}</p>{list}</div>;
  }
  return (
    <details className="ga-files">
      <summary className="ga-sub">{t("gate.submittedFilesN", { n: paths.length })}</summary>
      {list}
    </details>
  );
}
