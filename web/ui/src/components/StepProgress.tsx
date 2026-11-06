import { budgetLevel } from "../format";
import { StepEdit } from "../live";

export interface LiveStep {
  id: string; title: string; scope?: string[]; budget?: { files: number; lines: number };
  status: string; record?: { check?: {
    casesPassed: number; casesNotPassed: string[]; files: number; lines: number; overBudget?: boolean; outOfScope?: string[];
  } } | null;
}

const STATUS: Record<string, [string, string]> = {
  NOT_STARTED: ["Chưa bắt đầu", "muted"],
  IN_PROGRESS: ["Đang làm", "info"],
  AWAITING_DEV: ["Chờ dev accept", "warn"],
  ACCEPTED: ["Đã accept", "ok"],
};

function Bar({ label, actual, budget }: { label: string; actual: number | null; budget: number }) {
  const level = actual == null ? "none" : budgetLevel(actual, budget);
  // Thanh đầy ở 150% ngân sách để thấy rõ mức vượt.
  const pct = actual == null || !(budget > 0) ? 0 : Math.min(100, (actual / (budget * 1.5)) * 100);
  const mark = budget > 0 ? (1 / 1.5) * 100 : 0;
  return (
    <div className="pbar">
      <div className="pbar-head"><span>{label}</span><b className={`lv-${level}`}>{actual ?? "—"} / {budget}</b></div>
      <div className="pbar-track" role="progressbar" aria-label={label} aria-valuenow={actual ?? 0} aria-valuemax={budget}>
        <div className={`pbar-fill lv-bg-${level}`} style={{ width: `${pct}%` }} />
        <i className="pbar-mark" style={{ left: `${mark}%` }} />
      </div>
    </div>
  );
}

export function StepProgress({ step, edits }: { step: LiveStep; edits: StepEdit[] }) {
  const [text, tone] = STATUS[step.status] ?? [step.status, "muted"];
  const check = step.record?.check;
  const notPassed = check?.casesNotPassed ?? [];
  const total = check ? check.casesPassed + notPassed.length : 0;
  const outside = check?.outOfScope ?? [];
  const lastEdit = edits[0];
  return (
    <article className="step-card">
      <header>
        <span className="step-id">{step.id}</span>
        <h3>{step.title}</h3>
        <span className={`pill tone-${tone}`}>{text}</span>
      </header>
      <div className="bars">
        <Bar label="File" actual={check ? check.files : null} budget={step.budget?.files ?? 0} />
        <Bar label="Dòng" actual={check ? check.lines : null} budget={step.budget?.lines ?? 0} />
      </div>
      {check ? (
        <p className="case-line">
          <b className={notPassed.length ? "lv-far" : "lv-ok"}>{check.casesPassed}/{total} pass</b>
          {notPassed.length > 0 && <span className="muted"> · chưa pass: {notPassed.map((c) => <code key={c}>{c}</code>)}</span>}
        </p>
      ) : (
        <p className="case-line muted">Chưa có kết quả finish</p>
      )}
      {outside.length > 0 && <p className="lv-far small">Ngoài phạm vi: {outside.map((p) => <code key={p}>{p}</code>)}</p>}
      {lastEdit && <p className="muted small">{edits.length} file đã sửa, gần nhất <code>{lastEdit.path}</code></p>}
    </article>
  );
}
