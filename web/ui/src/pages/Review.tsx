import { type CSSProperties, type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { ApiError, UnauthorizedError, getControl, getEvents, getFeature, postJson } from "../api";
import { AgentReview } from "../components/AgentReview";
import { ApiTable } from "../components/ApiTable";
import { CaseCoverage } from "../components/CaseCoverage";
import { CasesTable } from "../components/CasesTable";
import { ErrorBoundary } from "../components/ErrorBoundary";
import { DiffView } from "../components/DiffView";
import { ErDiagram } from "../components/ErDiagram";
import { EvalTable } from "../components/EvalTable";
import { GateItem, GateList, Tone } from "../components/GateList";
import { approvalBadge } from "../approve";
import { GatePanel, STATE_TEXT } from "../components/GatePanel";
import { Resubmission } from "../components/Resubmission";
import { Markdown } from "../components/Markdown";
import { ReviewSummary } from "../components/ReviewSummary";
import { RiskPanel } from "../components/RiskPanel";
import { StepMapView } from "../components/StepMapView";
import { StepsTable } from "../components/StepsTable";
import { UserMenu, useMe } from "../components/UserMenu";
import { groupByStep } from "../diff";
import { ago, budgetLevel } from "../format";
import { cardFor, erLayout, stepMap, summaryTiles } from "../reviewModel";
import { encodePath, featureHref, selectionFromQuery } from "../routes";
import { eventReloadsControl } from "../control";
import { subscribe } from "../sse";
import type { ControlAction, FeatureDetail, FeatureEvent, GateData, GateName, PendingImport, ReviewCardData, Snapshot, StepData } from "../types";
import "../review.css";

const GATE_TITLE: Record<GateName, string> = { BRD: "BRD", DESIGN: "Thiết kế", CASES: "Case", REVIEW: "Review" };

const gateTone = (s: string): Tone =>
  s === "APPROVED" ? "ok" : s === "STALE" ? "bad" : s === "NOT_SUBMITTED" ? "idle" : "warn";
const stepTone = (s: string): Tone =>
  s === "ACCEPTED" ? "ok" : s === "IN_PROGRESS" ? "info" : s === "AWAITING_DEV" ? "warn" : "idle";

/** Mục chọn mặc định: cổng đang chờ theo cột, hoặc bước đầu tiên chưa accept khi đang code. */
function defaultSelection(d: FeatureDetail): string {
  const col = d.column;
  if (col === "CODE") return d.snapshot.steps.find((s) => s.status !== "ACCEPTED")?.id ?? "REVIEW";
  if (col === "DONE") return "REVIEW";
  return d.snapshot.gates.some((g) => g.gate === col) ? col : d.snapshot.gates[0]?.gate ?? "";
}

function Artifact({ snap, path }: { snap: Snapshot; path: string }) {
  const a = snap.artifacts[path];
  if (!a) return <p className="muted">Chưa có <code>{path}</code>.</p>;
  return (
    <>
      {a.truncated && <p className="pill warn trunc">đã cắt</p>}
      {path.endsWith(".md") ? <Markdown source={a.content} /> : <pre className="codeblock"><code>{a.content}</code></pre>}
    </>
  );
}

/** Nội dung 2a của cổng (tài liệu gốc), giữ nguyên khi chưa có review card. */
function GateMain2a({ gate, snap }: { gate: GateData; snap: Snapshot }) {
  switch (gate.gate) {
    case "BRD":
      return <Artifact snap={snap} path="brd.md" />;
    case "DESIGN":
      return (
        <>
          <Artifact snap={snap} path="design.md" />
          <DesignFiles snap={snap} />
        </>
      );
    case "CASES":
      return <CasesTable cases={snap.cases} />;
    case "REVIEW":
      return <ReviewGate snap={snap} />;
  }
}

const dbFiles = (snap: Snapshot) => Object.keys(snap.artifacts).filter((p) => p.startsWith("db/")).sort();

function DesignFiles({ snap }: { snap: Snapshot }) {
  return (
    <>
      <h3>Các bước</h3>
      <StepsTable steps={snap.steps} />
      {dbFiles(snap).map((p) => <div key={p}><h3>{p}</h3><Artifact snap={snap} path={p} /></div>)}
      <h3>design.yaml</h3>
      <Artifact snap={snap} path="design.yaml" />
    </>
  );
}

function ReviewGate({ snap }: { snap: Snapshot }) {
  return (
    <>
      <h3>Eval</h3>
      <EvalTable report={snap.eval} />
      <h3>Diff theo bước</h3>
      <DiffView groups={groupByStep(snap.steps)} />
      <h3>Agent review</h3>
      <AgentReview review={snap.review} />
    </>
  );
}

function Panel({ title, src, children }: { title: string; src?: ReactNode; children: ReactNode }) {
  return (
    <section className="rv-panel">
      <div className="rv-panel-head"><h3>{title}</h3>{src && <span className="src">{src}</span>}</div>
      {children}
    </section>
  );
}

function DesignCard({ card, snap }: { card: ReviewCardData; snap: Snapshot }) {
  const d = card.design;
  const map = useMemo(() => stepMap(card), [card]);
  const er = useMemo(() => (d ? erLayout(d.tables ?? []) : null), [d]);
  const createOnly = (card.flags ?? []).find((f) => f.code === "DB_CREATE_ONLY");
  const sql = dbFiles(snap);
  return (
    <>
      {!d ? <p className="muted">Không tính được phần thiết kế (xem cờ ở cột phải).</p> : (
        <>
          <Panel title="Bước nào được chạm vào đâu" src={<>sinh từ <code>design.yaml</code> + cây file hiện tại</>}>
            {map ? (
              <>
                <StepMapView map={map} />
                <div className="legend">
                  <span style={{ "--c": "var(--bad)" } as CSSProperties}>nhiều bước chạm cùng chỗ</span>
                  <span style={{ "--c": "var(--warn)" } as CSSProperties}>glob khớp file đã có</span>
                  <span style={{ "--c": "var(--ok)" } as CSSProperties}>file mới, khớp thiết kế</span>
                </div>
                {map.hidden > 0 && <p className="note">còn {map.hidden} mục không vẽ</p>}
              </>
            ) : <p className="muted">Không có bước để vẽ.</p>}
          </Panel>
          <Panel title="Thay đổi DB" src={sql.length > 0 && <>sinh từ {sql.map((p, i) => <span key={p}>{i > 0 && " + "}<code>{p}</code></span>)}</>}>
            {er ? <ErDiagram layout={er} /> : <p className="muted">Không có bảng nào để vẽ.</p>}
            {createOnly && <p className="note">{createOnly.message}</p>}
            {(d.touchesExisting ?? []).length > 0 && (
              <div className="tbl">
                <table className="grid">
                  <thead><tr><th>Lệnh</th><th>Bảng</th><th>SQL</th></tr></thead>
                  <tbody>
                    {d.touchesExisting.map((t, i) => (
                      <tr key={i}><td><code>{t.kind}</code></td><td><code>{t.table}</code></td><td><code>{t.text}</code></td></tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>
          <Panel title="API" src={<>từ <code>design.md</code> · mục API</>}>
            <ApiTable rows={d.apiRows ?? []} lines={d.apiLines ?? []} />
          </Panel>
        </>
      )}
      <details className="rv-panel">
        <summary>Tài liệu gốc design.md — gồm sơ đồ sequence do agent vẽ, chỉ để tham khảo</summary>
        <Artifact snap={snap} path="design.md" />
      </details>
      <details className="rv-panel">
        <summary>File gốc</summary>
        <DesignFiles snap={snap} />
      </details>
    </>
  );
}

function BrdCard({ card, snap }: { card: ReviewCardData; snap: Snapshot }) {
  const b = card.brd;
  return (
    <>
      {(b?.outOfScope ?? []).length > 0 && (
        <><h3>Ngoài phạm vi</h3><ul className="short">{b!.outOfScope.map((x, i) => <li key={i}>{x}</li>)}</ul></>
      )}
      {(b?.answeredQuestions ?? []).length > 0 && (
        <><h3>Câu hỏi đã trả lời</h3><ul className="short">{b!.answeredQuestions.map((x, i) => <li key={i}>{x}</li>)}</ul></>
      )}
      <h3>brd.md</h3>
      <Artifact snap={snap} path="brd.md" />
    </>
  );
}

function GateMain({ gate, snap, repoId }: { gate: GateData; snap: Snapshot; repoId: string }) {
  const card = cardFor(snap, gate.gate);
  if (!card) {
    return (
      <>
        <p className="muted">Chưa có review card cho cổng này (cổng còn khóa hoặc CLI chưa gửi)</p>
        <GateMain2a gate={gate} snap={snap} />
      </>
    );
  }
  return (
    <>
      <ReviewSummary tiles={summaryTiles(card)} />
      <RiskPanel card={card} repoId={repoId} />
      {gate.gate === "DESIGN" ? <DesignCard card={card} snap={snap} />
        : gate.gate === "CASES" ? (
          <>
            {card.cases ? <CaseCoverage cases={card.cases} /> : <p className="muted">Không tính được độ phủ case (xem cờ ở cột phải).</p>}
            <h3>Case</h3>
            <CasesTable cases={snap.cases} />
          </>
        )
        : gate.gate === "BRD" ? <BrdCard card={card} snap={snap} />
        : <ReviewGate snap={snap} />}
    </>
  );
}

function StepMain({ step, snap }: { step: StepData; snap: Snapshot }) {
  const check = step.record?.check;
  const files = check?.files ?? 0;
  const lines = check?.lines ?? 0;
  return (
    <>
      <dl className="step-grid">
        <dt>Phạm vi</dt><dd>{step.scope.map((g) => <div key={g}><code>{g}</code></div>)}</dd>
        <dt>Ngân sách</dt><dd>{step.budget.files} file, {step.budget.lines} dòng</dd>
        {check && (
          <>
            <dt>Thực tế</dt>
            <dd>
              <span className={`budget ${budgetLevel(files, step.budget.files)}`}>{files} file</span>,{" "}
              <span className={`budget ${budgetLevel(lines, step.budget.lines)}`}>{lines} dòng</span>
            </dd>
            <dt>Case đạt</dt>
            <dd>{check.casesPassed ?? 0}/{step.caseIds.length}
              {(check.casesNotPassed?.length ?? 0) > 0 && <span className="pill bad">chưa đạt: {check.casesNotPassed!.join(", ")}</span>}</dd>
            {(check.outOfScope?.length ?? 0) > 0 && (
              <><dt>Ngoài phạm vi</dt><dd>{check.outOfScope!.map((p) => <div key={p}><code>{p}</code></div>)}</dd></>
            )}
          </>
        )}
      </dl>
      <h3>Case của bước</h3>
      <CasesTable cases={snap.cases.filter((c) => c.step === step.id)} />
      <h3>Diff</h3>
      <DiffView groups={groupByStep([step])} />
    </>
  );
}

/**
 * Dải "nhập từ local" (chế độ web): bản duyệt local chờ chủ tính năng xác nhận; nút Xác nhận chỉ hiện với chủ.
 */
function PendingImports({ items, owner, repoId, featureKey, onDone }: {
  items: PendingImport[]; owner: string | null; repoId: string; featureKey: string; onDone: () => void;
}) {
  const user = useMe();
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const isOwner = !!user && owner != null && user.login === owner;
  async function confirm(gate: GateName) {
    setBusy(gate);
    setError("");
    try {
      await postJson("/api/gates/confirm-import", { repo: repoId, feature: featureKey, gate });
      onDone();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Không kết nối được máy chủ");
    } finally {
      setBusy("");
    }
  }
  return (
    <div className="imports">
      {items.map((p) => (
        <div key={`${p.gate}:${p.hash}`} className="import-strip">
          <span>
            Nhập từ local: <b>{p.gate}</b> duyệt bởi {p.originalApprover ?? "—"} lúc{" "}
            {p.originalAt ? new Date(p.originalAt).toLocaleString("vi-VN") : "—"} — chờ chủ tính năng xác nhận
          </span>
          {isOwner && <button type="button" disabled={!!busy} onClick={() => confirm(p.gate)}>Xác nhận</button>}
        </div>
      ))}
      {error && <p className="error" role="alert">{error}</p>}
    </div>
  );
}

export function Review({ repoId, featureKey }: { repoId: string; featureKey: string }) {
  const [detail, setDetail] = useState<FeatureDetail | null>(null);
  const [events, setEvents] = useState<FeatureEvent[]>([]);
  const [controlActions, setControlActions] = useState<ControlAction[]>([]);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState("");
  const [now, setNow] = useState(() => new Date());
  const [params, setParams] = useSearchParams();

  // Tính năng đang xem; phản hồi về muộn của tính năng trước (đã đổi route) bị bỏ.
  const current = useRef("");
  current.current = `${repoId}\n${featureKey}`;
  // Mục đã lấy từ URL (hay đã ghi vào URL), để `?g=` chỉ thắng khi chính nó đổi.
  const applied = useRef("");

  const loadControl = useCallback(() => {
    const mine = `${repoId}\n${featureKey}`;
    getControl(repoId, featureKey)
      .then((c) => { if (current.current === mine) setControlActions(c.actions ?? []); })
      .catch(() => { /* giữ danh sách lệnh cũ */ });
  }, [repoId, featureKey]);

  const load = useCallback(() => {
    const mine = `${repoId}\n${featureKey}`;
    getFeature(repoId, featureKey)
      .then((d) => { if (current.current !== mine) return; setDetail(d as FeatureDetail); setError(""); setNow(new Date()); })
      .catch((e) => { if (current.current === mine && !(e instanceof UnauthorizedError)) setError("Không tải được tính năng"); });
    getEvents(repoId, featureKey, 200)
      .then((e) => { if (current.current === mine) setEvents(e as FeatureEvent[]); })
      .catch(() => { /* lỗi 401 đã chuyển trang; lỗi khác giữ danh sách cũ */ });
    loadControl();
  }, [repoId, featureKey, loadControl]);

  useEffect(() => {
    // đổi tính năng: bỏ snapshot, event và lỗi của tính năng trước
    setDetail(null); setEvents([]); setControlActions([]); setError(""); setSelected(""); applied.current = "";
    load();
    const stop = subscribe(`/api/features/${encodePath(repoId, featureKey)}/stream`, (type, data) => {
      if (type === "snapshot") load();
      else if (type === "event") {
        try {
          const ev = JSON.parse(data) as FeatureEvent;
          setEvents((prev) => (ev.id != null && prev.some((p) => p.id === ev.id) ? prev : [ev, ...prev]));
          if (eventReloadsControl(ev.type)) loadControl();
        } catch { /* bỏ qua bản tin hỏng */ }
      }
    }, load);
    const tick = setInterval(() => setNow(new Date()), 30_000);
    return () => { stop(); clearInterval(tick); };
  }, [repoId, featureKey, load, loadControl]);

  // Lần tải đầu: mục trong `?g=` nếu hợp lệ, không thì mục mặc định; các lần tải lại giữ nguyên mục đang xem.
  const search = params.toString();
  useEffect(() => {
    if (!detail) return;
    const ids = [...detail.snapshot.gates.map((g) => g.gate), ...detail.snapshot.steps.map((s) => s.id)];
    const fromQuery = selectionFromQuery(search, ids);
    if (fromQuery && fromQuery !== applied.current) { applied.current = fromQuery; setSelected(fromQuery); }
    else if (!selected) setSelected(defaultSelection(detail));
  }, [detail, selected, search]);

  const select = useCallback((id: string) => {
    applied.current = id;
    setSelected(id);
    setParams((p) => { const n = new URLSearchParams(p); n.set("g", id); return n; }, { replace: true });
  }, [setParams]);

  const items = useMemo<GateItem[]>(() => {
    if (!detail) return [];
    const s = detail.snapshot;
    return [
      ...s.gates.map((g): GateItem => ({
        id: g.gate, title: GATE_TITLE[g.gate], sub: [STATE_TEXT[g.state] ?? g.state, approvalBadge(g)].filter(Boolean).join(" · "), tone: gateTone(g.state), group: "gate",
      })),
      ...s.steps.map((st): GateItem => ({ id: st.id, title: st.id, sub: st.title, tone: stepTone(st.status), group: "step" })),
    ];
  }, [detail]);

  if (!detail) {
    return (
      <main className="page">
        <p><Link to="/">← Bảng</Link></p>
        {error ? <p className="error banner" role="alert">{error}</p> : <p className="muted">Đang tải…</p>}
      </main>
    );
  }

  const snap = detail.snapshot;
  const gate = snap.gates.find((g) => g.gate === selected) ?? null;
  const step = snap.steps.find((s) => s.id === selected) ?? null;

  return (
    <div className="page">
      <header className="review-head">
        <Link to="/">← Bảng</Link>
        <h1><span className="key">{snap.feature.key}</span> {snap.feature.title}</h1>
        <span className="tag">{snap.feature.flow}</span>
        <span className="spacer" />
        <div className="meta">
          <span>{snap.repo.id} · {snap.repo.branch}</span>
          <span>Chủ: {detail.owner ?? "—"}</span>
          <span>Đồng bộ {ago(detail.syncedAt, now)}</span>
          {detail.clockSkew && <span className="warn" title="Đồng hồ máy gửi lệch">lệch giờ</span>}
          {detail.approvalsMode === "web" && <span className="tag" title="Cổng duyệt trên web">duyệt web</span>}
          <Link to={featureHref(repoId, featureKey, true)}>Theo dõi agent →</Link>
        </div>
        <UserMenu />
      </header>
      {error && <p className="error banner" role="alert">{error}</p>}
      {snap.truncated && <p className="banner">Snapshot quá lớn nên một số diff đã bị cắt.</p>}
      {(detail.pendingImports ?? []).length > 0 && (
        <PendingImports items={detail.pendingImports!} owner={detail.owner} repoId={repoId} featureKey={featureKey} onDone={load} />
      )}
      <div className="review">
        <GateList items={items} selected={selected} onSelect={select} />
        <section className="review-main" aria-label="Nội dung">
          <h2>{gate ? `${GATE_TITLE[gate.gate]}` : step ? `${step.id} · ${step.title}` : ""}</h2>
          {gate && (
            <ErrorBoundary key={`${repoId}\n${featureKey}\n${gate.gate}`} fallback={
              <>
                <p className="muted">Không vẽ được review card cho cổng này.</p>
                <GateMain2a gate={gate} snap={snap} />
              </>}>
              <Resubmission repoId={repoId} featureKey={featureKey} gate={gate.gate}
                reloadKey={`${gate.submission?.hash ?? ""}:${gate.state}:${detail.syncedAt}`} />
              <GateMain gate={gate} snap={snap} repoId={repoId} />
            </ErrorBoundary>
          )}
          {step && <StepMain step={step} snap={snap} />}
        </section>
        <GatePanel gate={gate} step={step} snapshot={snap} events={events} repoId={repoId} featureKey={featureKey}
          detail={detail} actions={controlActions} onChanged={load} />
      </div>
    </div>
  );
}
