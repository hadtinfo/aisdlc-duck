import { useState } from "react";
import { ApiError, sendAction } from "../api";
import { acceptStatusShown, actionLabel, latestAction, outOfScopeFiles, validScopePath } from "../control";
import type { ControlAction, FeatureEvent, StepData } from "../types";
import { CopyCommand } from "./CopyCommand";

/**
 * Nút Accept bước và Cho phép file ngoài phạm vi (trang B). `actor` = owner/lead; người khác chỉ thấy nhãn trạng thái.
 * Lệnh gửi qua /api/control/actions, áp dụng ở máy dev nên hiện "chờ áp dụng" tới khi có kết quả.
 */
export function StepActions({ step, events, actions, actor, repoId, featureKey, onChanged }: {
  step: StepData; events: FeatureEvent[]; actions: ControlAction[]; actor: boolean;
  repoId: string; featureKey: string; onChanged: () => void;
}) {
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function send(kind: "ACCEPT_STEP" | "ALLOW_SCOPE", path?: string) {
    setBusy(true);
    setError("");
    try {
      // Accept gắn với cây code dev đang xem: bước đổi sau khi trang tải thì server trả 409
      await sendAction(repoId, featureKey, kind, step.id,
        kind === "ACCEPT_STEP" ? { endTree: step.record?.endTree } : { path });
      setConfirm(false);
      onChanged();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Không kết nối được máy chủ");
    } finally {
      setBusy(false);
    }
  }

  const awaiting = step.status === "AWAITING_DEV";
  const oos = outOfScopeFiles(step, events);
  const acc = latestAction(actions, "ACCEPT_STEP", step.id);
  const accRetry = !acc || acc.status === "STALE" || acc.status === "EXPIRED";
  const check = step.record?.check;
  const notPassed = check?.casesNotPassed ?? [];
  const canAllow = step.status === "IN_PROGRESS" || step.status === "AWAITING_DEV";
  const showAccept = awaiting && (acc || actor);
  const showAccStatus = acceptStatusShown(step.status, acc);
  if (!showAccept && !showAccStatus && oos.length === 0) return null;

  return (
    <section className="panel-sec step-actions">
      <h3>Thao tác bước</h3>
      {showAccStatus && acc && (
        <p className={`ctl-status ${acc.status.toLowerCase()}`}>{actionLabel("ACCEPT_STEP", acc.status, acc.reason)}</p>
      )}
      {awaiting && actor && accRetry && !confirm && (
        <button type="button" onClick={() => { setConfirm(true); setError(""); }}>Accept {step.id}</button>
      )}
      {awaiting && actor && accRetry && confirm && (
        <div className="ga-confirm" role="group" aria-label="Xác nhận accept">
          <p className="ga-q">Accept {step.id}?</p>
          <ul className="ga-paths">
            <li>Case đạt: {check ? `${check.casesPassed ?? 0}/${step.caseIds.length}` : "chưa có kết quả"}
              {notPassed.length > 0 && <span className="pill bad">chưa qua: {notPassed.join(", ")}</span>}</li>
            <li>{check?.files ?? 0} file, {check?.lines ?? 0} dòng (ngân sách {step.budget.files} file, {step.budget.lines} dòng)</li>
            {oos.length > 0 && <li className="bad">File ngoài phạm vi: {oos.join(", ")}</li>}
          </ul>
          <div className="ga-buttons">
            <button type="button" disabled={busy} onClick={() => send("ACCEPT_STEP")}>Xác nhận accept</button>
            <button type="button" className="secondary" disabled={busy} onClick={() => setConfirm(false)}>Hủy</button>
          </div>
        </div>
      )}
      {oos.length > 0 && (
        <>
          <p className="ga-sub">File ngoài phạm vi</p>
          <ul className="ctl-list">
            {oos.map((p) => {
              const a = latestAction(actions, "ALLOW_SCOPE", step.id, p);
              const retry = !a || a.status === "STALE" || a.status === "EXPIRED";
              return (
                <li key={p}>
                  <code className="path">{p}</code>
                  {a && <span className={`ctl-status ${a.status.toLowerCase()}`}>{actionLabel("ALLOW_SCOPE", a.status, a.reason)}</span>}
                  {actor && canAllow && retry && validScopePath(p) && (
                    <button type="button" className="secondary" disabled={busy} onClick={() => send("ALLOW_SCOPE", p)}>Cho phép</button>
                  )}
                  {actor && canAllow && retry && !validScopePath(p) && (
                    <span className="ga-sub">Dùng <code>aisdlc scope allow {p}</code> ở terminal</span>
                  )}
                </li>
              );
            })}
          </ul>
        </>
      )}
      {error && <p className="error" role="alert">{error}</p>}
      {awaiting && <div className="cmds"><CopyCommand command={`aisdlc step accept ${step.id}`} /></div>}
    </section>
  );
}
