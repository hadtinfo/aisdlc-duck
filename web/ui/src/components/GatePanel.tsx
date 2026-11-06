import { useState } from "react";
import { actionsFor, approvalBadge, canAct } from "../approve";
import { ago } from "../format";
import { cardFor, loadSeen, seenKey } from "../reviewModel";
import type { ControlAction, FeatureDetail, FeatureEvent, GateData, ReviewCardData, Snapshot, StepData } from "../types";
import { Assumptions } from "./Assumptions";
import { CopyCommand } from "./CopyCommand";
import { FlagList } from "./FlagList";
import { GateActions } from "./GateActions";
import { StepActions } from "./StepActions";
import { useMe } from "./UserMenu";

export const STATE_TEXT: Record<string, string> = {
  NOT_SUBMITTED: "Chưa nộp", AWAITING_APPROVAL: "Chờ duyệt", APPROVED: "Đã duyệt",
  STALE: "Đã đổi sau duyệt (STALE)", SUBMITTED_WITH_FINDINGS: "Có phát hiện",
  LOCKED_BY_PREVIOUS: "Chờ cổng trước",
};
export const STEP_TEXT: Record<string, string> = {
  NOT_STARTED: "Chưa bắt đầu", IN_PROGRESS: "Đang code", AWAITING_DEV: "Chờ accept", ACCEPTED: "Đã accept",
};

const short = (h?: string | null) => (h ? h.slice(0, 8) : "—");

/** Lệnh terminal gợi ý cho cổng ở trạng thái hiện tại. Lệnh chỉ để copy, web không ghi trạng thái. */
function gateCommands(g: GateData, snap: Snapshot): string[] {
  const name = g.gate.toLowerCase();
  switch (g.state) {
    case "AWAITING_APPROVAL": return [`aisdlc approve ${name}`];
    case "STALE": return [`aisdlc submit ${name}`, `aisdlc reopen ${name}`];
    case "SUBMITTED_WITH_FINDINGS": return [`aisdlc submit ${name}`];
    case "APPROVED": return [`aisdlc reopen ${name}`];
    default:
      if (g.gate === "REVIEW" && !snap.eval?.passed) return ["aisdlc eval"];
      return [`aisdlc submit ${name}`];
  }
}

function eventText(e: FeatureEvent): string {
  switch (e.type) {
    case "COMMAND": return `aisdlc ${e.detail ?? ""}`.trim();
    case "FILE_EDIT": return `Sửa ${e.path ?? "file"}`;
    case "SESSION_START": return "Bắt đầu phiên Claude Code";
    case "SESSION_STOP": return "Kết thúc phiên Claude Code";
    case "SCOPE_DENIED": return `Bị chặn ghi ngoài phạm vi: ${e.path ?? "file"}`;
    case "INSTRUCTION_DELIVERED": return "Agent đã nhận chỉ dẫn";
    case "ACTION_RESULT": return actionResultText(e.detail);
    default: return e.type;
  }
}

function actionResultText(detail: string | null): string {
  try {
    const r = JSON.parse(detail ?? "") as { status?: string; reason?: string };
    if (r.status === "APPLIED") return "Lệnh từ web đã được áp dụng";
    if (r.status === "STALE") return `Lệnh từ web hết hiệu lực${r.reason ? `: ${r.reason}` : ""}`;
  } catch { /* detail không phải JSON */ }
  return "Kết quả lệnh từ web";
}

function Events({ events }: { events: FeatureEvent[] }) {
  const rows = events.slice(0, 20);
  return (
    <section className="panel-sec">
      <h3>Hoạt động gần đây</h3>
      {rows.length === 0 && <p className="muted">Chưa có hoạt động.</p>}
      <ul className="events">
        {rows.map((e, i) => (
          <li key={e.id ?? `${e.at}-${i}`} className={e.outOfScope ? "oos" : ""}>
            <div className="ev-text">{eventText(e)}{e.step && <span className="tag">{e.step}</span>}</div>
            <div className="muted ev-meta">{ago(e.at)}{e.sender ? ` · ${e.sender}` : ""}{e.outOfScope ? " · ngoài phạm vi" : ""}</div>
          </li>
        ))}
      </ul>
    </section>
  );
}

/**
 * Cột phải: trạng thái và lệnh của cổng luôn ở đầu (không bị đẩy xuống dưới cuộn), rồi cờ và giả định
 * của review card (khi có), phát hiện, câu hỏi, phản hồi gần nhất và hoạt động.
 */
export function GatePanel({ gate, step, snapshot, events, repoId, featureKey, detail, actions = [], onChanged }: {
  gate: GateData | null; step: StepData | null; snapshot: Snapshot; events: FeatureEvent[];
  repoId: string; featureKey: string; detail: FeatureDetail; actions?: ControlAction[]; onChanged: () => void;
}) {
  const user = useMe();
  const stepEvents = step ? events.filter((e) => e.step === step.id) : events;
  const card = gate ? cardFor(snapshot, gate.gate) : null;
  const key = gate ? seenKey(repoId, featureKey, gate.gate, gate.currentHash) : "";
  // Đếm lại giả định đã tick khi người dùng tick (Assumptions tự lưu localStorage), để hộp xác nhận duyệt cập nhật.
  const [, setSeenTick] = useState(0);
  return (
    <aside className="gatepanel" aria-label="Chi tiết cổng">
      {gate && (
        <GateHead gate={gate} snapshot={snapshot} card={card} seen={key ? loadSeen(key) : []}
          detail={detail} repoId={repoId} featureKey={featureKey} onChanged={onChanged} />
      )}
      {gate && card && <FlagList flags={card.flags ?? []} />}
      {gate && card && card.assumptions !== undefined && (
        <Assumptions key={key} items={card.assumptions} section={card.assumptionsSection} storageKey={key}
          onChange={() => setSeenTick((t) => t + 1)} />
      )}
      {gate && <GateInfo gate={gate} snapshot={snapshot} />}
      {step && (
        <StepInfo step={step} events={events} actions={actions} actor={!!user && canAct(user, detail.owner)}
          repoId={repoId} featureKey={featureKey} onChanged={onChanged} />
      )}
      <Events events={stepEvents} />
    </aside>
  );
}

/**
 * Trạng thái, hash và thao tác của cổng. Chế độ web + chủ tính năng/lead: nút Duyệt/Trả lại/Mở lại (lệnh nộp ở
 * terminal vẫn hiện để copy). Còn lại: khối lệnh copy như 2a; chế độ web thêm dòng nhắc duyệt trên web.
 */
function GateHead({ gate, snapshot, card, seen, detail, repoId, featureKey, onChanged }: {
  gate: GateData; snapshot: Snapshot; card: ReviewCardData | null; seen: number[]; detail: FeatureDetail;
  repoId: string; featureKey: string; onChanged: () => void;
}) {
  const user = useMe();
  const web = detail.approvalsMode === "web";
  const actor = web && !!user && canAct(user, detail.owner);
  const name = gate.gate.toLowerCase();
  const commands = gateCommands(gate, snapshot)
    .filter((c) => !actor || (c !== `aisdlc approve ${name}` && c !== `aisdlc reopen ${name}`));
  const badge = approvalBadge(gate);
  const edited = !!gate.approval && gate.approval.hash !== gate.currentHash;
  return (
    <>
      <section className="panel-sec">
        <h3>Cổng {gate.gate}</h3>
        <p className={`state ${gate.state}`}>{STATE_TEXT[gate.state] ?? gate.state}</p>
        {edited && <p className="pill bad block">Đã sửa sau duyệt</p>}
        {badge && <p><span className={`tag ${gate.superseded && !gate.approval ? "badge-superseded" : "badge-auto"}`}>{badge}</span></p>}
        <dl className="kv">
          <dt>Hash đã nộp</dt><dd className="mono">{short(gate.submission?.hash)}</dd>
          <dt>Hash đã duyệt</dt><dd className="mono">{short(gate.approval?.hash)}</dd>
          <dt>Hash hiện tại</dt><dd className="mono">{short(gate.currentHash)}</dd>
          {gate.approval && <><dt>Người duyệt</dt><dd>{gate.approval.label ?? gate.approval.approver}<div className="muted">{ago(gate.approval.at)}</div></dd></>}
          {gate.submission && <><dt>Nộp lúc</dt><dd>{ago(gate.submission.at)}</dd></>}
        </dl>
      </section>
      {actor && Object.values(actionsFor(gate.state)).some(Boolean) && (
        <section className="panel-sec">
          <h3>Thao tác</h3>
          <GateActions key={`${gate.gate}:${gate.currentHash}:${gate.state}`} gate={gate} card={card} seen={seen}
            submittedPaths={detail.submittedPaths?.[gate.gate] ?? []} repoId={repoId} featureKey={featureKey}
            onDone={onChanged} />
        </section>
      )}
      {commands.length > 0 && (
        <section className="panel-sec">
          <h3>Lệnh</h3>
          <div className="cmds">{commands.map((c) => <CopyCommand key={c} command={c} />)}</div>
          {web && !actor && gate.state === "AWAITING_APPROVAL" && (
            <p className="muted hint-line">Duyệt trên web hoặc <code>aisdlc approve {name}</code> (cần token approve)</p>
          )}
        </section>
      )}
    </>
  );
}

/** Phần còn lại của cổng: phát hiện khi nộp, câu hỏi mở, phản hồi gần nhất. */
function GateInfo({ gate, snapshot }: { gate: GateData; snapshot: Snapshot }) {
  const questions = snapshot.openQuestions.filter((q) => q.gate === gate.gate);
  const findings = gate.submission?.findings ?? [];
  return (
    <>
      {findings.length > 0 && (
        <section className="panel-sec">
          <h3>Phát hiện ({findings.length})</h3>
          <ul className="findings">
            {findings.map((f, i) => <li key={i}><code>{f.code}</code> {f.message}</li>)}
          </ul>
        </section>
      )}
      {questions.length > 0 && (
        <section className="panel-sec">
          <h3>Câu hỏi mở ({questions.length})</h3>
          <ul className="findings">{questions.map((q, i) => <li key={i}>{q.text}</li>)}</ul>
        </section>
      )}
      {gate.lastFeedback && (
        <section className="panel-sec feedback">
          <h3>{gate.lastFeedback.kind === "REJECTED" ? "Bị trả lại" : "Mở lại"}</h3>
          <p>{gate.lastFeedback.comment || <span className="muted">(không có ghi chú)</span>}</p>
          <p className="muted">{gate.lastFeedback.by} · {ago(gate.lastFeedback.at)}</p>
        </section>
      )}
    </>
  );
}

function StepInfo({ step, events, actions, actor, repoId, featureKey, onChanged }: {
  step: StepData; events: FeatureEvent[]; actions: ControlAction[]; actor: boolean;
  repoId: string; featureKey: string; onChanged: () => void;
}) {
  return (
    <>
      <section className="panel-sec">
        <h3>Bước {step.id}</h3>
        <p className={`state ${step.status}`}>{STEP_TEXT[step.status] ?? step.status}</p>
        <dl className="kv">
          <dt>Ngân sách</dt><dd>{step.budget.files} file, {step.budget.lines} dòng</dd>
          {step.record?.acceptedBy && <><dt>Accept bởi</dt><dd>{step.record.acceptedBy}</dd></>}
        </dl>
      </section>
      <StepActions step={step} events={events} actions={actions} actor={actor}
        repoId={repoId} featureKey={featureKey} onChanged={onChanged} />
    </>
  );
}
