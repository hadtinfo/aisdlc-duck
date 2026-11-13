import type { AttemptData } from "../types";
import { formatDateTime as when, useT } from "../i18n";

/** Các lần làm đã khép (sau rewind), chỉ đọc: không có nút hành động; nội dung artifact hiện nguyên văn trong <pre>. */
export function AttemptArchive({ attempts }: { attempts: AttemptData[] }) {
  const { t } = useT();
  if (attempts.length === 0) return null;
  return (
    <section className="attempts">
      <h3>{t("rvw.attemptsTitle")}</h3>
      {attempts.map((a) => (
        <details key={a.attempt} className="rv-panel attempt">
          <summary>
            {t("rvw.attemptHead", { n: a.attempt, checkpoint: a.rewind.checkpoint, by: a.rewind.by, at: when(a.rewind.at) })}
          </summary>
          {a.gates.map((g) => (
            <div key={g.gate} className="attempt-gate">
              <h4>{g.gate}</h4>
              <ul className="attempt-log">
                {g.submissions.map((s) => (
                  <li key={`s${s.hash}${s.at}`}>
                    <span>{t("rvw.attemptSubmitted", { by: s.by, at: when(s.at) })}</span>
                    {" "}<code>{s.hash}</code>
                    {s.artifacts.map((f) => (
                      <details key={f.path} className="attempt-file">
                        <summary>{t("rvw.attemptShowFile")} <code>{f.path}</code></summary>
                        <pre className="codeblock attempt-pre"><code>{f.content || t("rvw.attemptEmptyFile")}</code></pre>
                      </details>
                    ))}
                  </li>
                ))}
                {g.feedback.map((f) => (
                  <li key={`f${f.kind}${f.at}`}>
                    <span>{t("rvw.attemptFeedback", { kind: f.kind, by: f.by, at: when(f.at) })}</span>
                    <p className="attempt-comment">{f.comment}</p>
                  </li>
                ))}
                {g.approvals.map((p) => (
                  <li key={`a${p.approver}${p.at}`}>
                    {t("rvw.attemptApproved", { by: p.approver, at: when(p.at) })}
                    {p.revokedAt && <span className="muted"> · {t("rvw.attemptRevoked", { at: when(p.revokedAt) })}</span>}
                  </li>
                ))}
              </ul>
            </div>
          ))}
          {a.steps.length > 0 && (
            <div className="attempt-gate">
              <h4>{t("rvw.attemptSteps")}</h4>
              <ul className="attempt-log">
                {a.steps.map((s) => (
                  <li key={s.id}>
                    <code>{s.id}</code>{" "}
                    {t("rvw.attemptStepLine", { status: s.status, files: s.files, lines: s.lines, passed: s.casesPassed, total: s.casesTotal })}
                  </li>
                ))}
              </ul>
            </div>
          )}
          {a.reviewCard && (a.reviewCard.flags ?? []).length > 0 && (
            <div className="attempt-gate">
              <h4>{t("rvw.attemptReview")}</h4>
              <ul className="attempt-log">
                {a.reviewCard.flags.map((f, i) => (
                  <li key={`${f.code}${i}`}><span className="tag">{f.level}</span> <code>{f.code}</code> {f.message}</li>
                ))}
              </ul>
            </div>
          )}
        </details>
      ))}
    </section>
  );
}
