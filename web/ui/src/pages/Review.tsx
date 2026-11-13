import { type CSSProperties, type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { ApiError, UnauthorizedError, getAttempts, getControl, getEvents, getFeature, postJson } from "../api";
import { AgentReview } from "../components/AgentReview";
import { AttemptArchive } from "../components/AttemptArchive";
import { ApiTable } from "../components/ApiTable";
import { CaseCoverage } from "../components/CaseCoverage";
import { CasesTable } from "../components/CasesTable";
import { ErrorBoundary } from "../components/ErrorBoundary";
import { DiffView } from "../components/DiffView";
import { ErDiagram } from "../components/ErDiagram";
import { EvalTable } from "../components/EvalTable";
import { GateItem, GateList, Tone } from "../components/GateList";
import { approvalBadge } from "../approve";
import { GatePanel, stateText, stepText } from "../components/GatePanel";
import { Resubmission } from "../components/Resubmission";
import { Markdown } from "../components/Markdown";
import { ReviewSummary } from "../components/ReviewSummary";
import { RiskPanel } from "../components/RiskPanel";
import { RiskLLM } from "../components/RiskLLM";
import { StepMapView } from "../components/StepMapView";
import { StepsTable } from "../components/StepsTable";
import { UserMenu, useMe } from "../components/UserMenu";
import { groupByStep } from "../diff";
import { ago, budgetLevel, caseRatio } from "../format";
import { acceptedApproval, cardFor, erLayout, stepMap, summaryTiles } from "../reviewModel";
import { encodePath, featureHref, selectionFromQuery } from "../routes";
import { eventReloadsControl } from "../control";
import { subscribe } from "../sse";
import type { AttemptData, ControlAction, FeatureDetail, FeatureEvent, GateData, GateName, PendingImport, ReviewCardData, Snapshot, StepData } from "../types";
import { intlTag, t as tNow, useT } from "../i18n";
import "../review.css";

// getter: tên cổng theo locale đang dùng mỗi lần đọc
const GATE_TITLE: Record<GateName, string> = {
  BRD: "BRD", get DESIGN() { return tNow("column.design"); }, get CASES() { return tNow("column.cases"); }, REVIEW: "Review",
};

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
  const { t } = useT();
  const a = snap.artifacts[path];
  if (!a) return <p className="muted">{t("rvw.noArtifact")} <code>{path}</code>.</p>;
  return (
    <>
      {a.truncated && <p className="pill warn trunc">{t("diff.truncated")}</p>}
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
  const { t } = useT();
  return (
    <>
      <h3>{t("live.steps")}</h3>
      <StepsTable steps={snap.steps} />
      {dbFiles(snap).map((p) => <div key={p}><h3>{p}</h3><Artifact snap={snap} path={p} /></div>)}
      <h3>design.yaml</h3>
      <Artifact snap={snap} path="design.yaml" />
    </>
  );
}

function ReviewGate({ snap }: { snap: Snapshot }) {
  const { t } = useT();
  return (
    <>
      <h3>Eval</h3>
      <EvalTable report={snap.eval} />
      <h3>{t("rvw.diffByStep")}</h3>
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
  const { t, locale } = useT();
  const d = card.design;
  const map = useMemo(() => stepMap(card), [card, locale]);
  const er = useMemo(() => (d ? erLayout(d.tables ?? []) : null), [d, locale]);
  const createOnly = (card.flags ?? []).find((f) => f.code === "DB_CREATE_ONLY");
  const sql = dbFiles(snap);
  return (
    <>
      {!d ? <p className="muted">{t("rvw.noDesign")}</p> : (
        <>
          <Panel title={t("rvw.mapTitle")} src={<>{t("rvw.generatedFrom")} <code>design.yaml</code> {t("rvw.plusTree")}</>}>
            {map ? (
              <>
                <StepMapView map={map} />
                <div className="legend">
                  <span style={{ "--c": "var(--bad)" } as CSSProperties}>{t("rvw.legendOverlap")}</span>
                  <span style={{ "--c": "var(--warn)" } as CSSProperties}>{t("rvw.legendExisting")}</span>
                  <span style={{ "--c": "var(--ok)" } as CSSProperties}>{t("rvw.legendNew")}</span>
                </div>
                {map.hidden > 0 && <p className="note">{t("rvw.hidden", { n: map.hidden })}</p>}
              </>
            ) : <p className="muted">{t("rvw.noStepsToDraw")}</p>}
          </Panel>
          <Panel title={t("rvw.dbChanges")} src={sql.length > 0 && <>{t("rvw.generatedFrom")} {sql.map((p, i) => <span key={p}>{i > 0 && " + "}<code>{p}</code></span>)}</>}>
            {er ? <ErDiagram layout={er} />
              : sql.length > 0 ? <p className="note">{t("rvw.tablesUnparsed", { n: sql.length })}</p>
              : <p className="muted">{t("rvw.noTables")}</p>}
            {createOnly && <p className="note">{createOnly.message}</p>}
            {(d.touchesExisting ?? []).length > 0 && (
              <div className="tbl">
                <table className="grid">
                  <thead><tr><th>{t("rvw.statement")}</th><th>{t("rvw.table")}</th><th>SQL</th></tr></thead>
                  <tbody>
                    {d.touchesExisting.map((t, i) => (
                      <tr key={i}><td><code>{t.kind}</code></td><td><code>{t.table}</code></td><td><code>{t.text}</code></td></tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>
          <Panel title="API" src={<>{t("rvw.from")} <code>design.md</code> · {t("rvw.apiSection")}</>}>
            <ApiTable rows={d.apiRows ?? []} lines={d.apiLines ?? []} />
          </Panel>
        </>
      )}
      <details className="rv-panel">
        <summary>{t("rvw.designDoc")}</summary>
        <Artifact snap={snap} path="design.md" />
      </details>
      <details className="rv-panel">
        <summary>{t("rvw.sourceFiles")}</summary>
        <DesignFiles snap={snap} />
      </details>
    </>
  );
}

function BrdCard({ card, snap }: { card: ReviewCardData; snap: Snapshot }) {
  const { t } = useT();
  const b = card.brd;
  return (
    <>
      {(b?.outOfScope ?? []).length > 0 && (
        <><h3>{t("tile.outOfScope")}</h3><ul className="short">{b!.outOfScope.map((x, i) => <li key={i}>{x}</li>)}</ul></>
      )}
      {(b?.answeredQuestions ?? []).length > 0 && (
        <><h3>{t("rvw.answered")}</h3><ul className="short">{b!.answeredQuestions.map((x, i) => <li key={i}>{x}</li>)}</ul></>
      )}
      <h3>brd.md</h3>
      <Artifact snap={snap} path="brd.md" />
    </>
  );
}

function GateMain({ gate, snap, repoId }: { gate: GateData; snap: Snapshot; repoId: string }) {
  const { t } = useT();
  const card = cardFor(snap, gate.gate);
  if (!card) {
    return (
      <>
        <p className="muted">{t("rvw.noCard")}</p>
        <GateMain2a gate={gate} snap={snap} />
      </>
    );
  }
  return (
    <>
      <ReviewSummary tiles={summaryTiles(card, dbFiles(snap).length)} acceptedHash={acceptedApproval(gate)?.hash} />
      <RiskPanel card={card} repoId={repoId} />
      {card.riskLLM && <RiskLLM data={card.riskLLM} />}
      {gate.gate === "DESIGN" ? <DesignCard card={card} snap={snap} />
        : gate.gate === "CASES" ? (
          <>
            {card.cases ? <CaseCoverage cases={card.cases} /> : <p className="muted">{t("rvw.noCoverage")}</p>}
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
  const { t } = useT();
  const check = step.record?.check;
  const files = check?.files ?? 0;
  const lines = check?.lines ?? 0;
  return (
    <>
      <dl className="step-grid">
        <dt>{t("steps.scope")}</dt><dd>{step.scope.map((g) => <div key={g}><code>{g}</code></div>)}</dd>
        <dt>{t("steps.budget")}</dt><dd>{t("steps.budgetValue", { files: step.budget.files, lines: step.budget.lines })}</dd>
        {check && (
          <>
            <dt>{t("rvw.actual")}</dt>
            <dd>
              <span className={`budget ${budgetLevel(files, step.budget.files)}`}>{t("rvw.files", { n: files })}</span>,{" "}
              <span className={`budget ${budgetLevel(lines, step.budget.lines)}`}>{t("rvw.lines", { n: lines })}</span>
            </dd>
            <dt>{t("step.casesPassed")}</dt>
            <dd>{caseRatio(check).passed}/{caseRatio(check).total}
              {(check.casesNotPassed?.length ?? 0) > 0 && <span className="pill bad">{t("rvw.notPassed", { list: check.casesNotPassed!.join(", ") })}</span>}</dd>
            {(check.outOfScope?.length ?? 0) > 0 && (
              <><dt>{t("tile.outOfScope")}</dt><dd>{check.outOfScope!.map((p) => <div key={p}><code>{p}</code></div>)}</dd></>
            )}
          </>
        )}
      </dl>
      <h3>{t("rvw.stepCases")}</h3>
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
  const { t } = useT();
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
      setError(e instanceof ApiError ? e.message : tNow("common.noServer"));
    } finally {
      setBusy("");
    }
  }
  return (
    <div className="imports">
      {items.map((p) => (
        <div key={`${p.gate}:${p.hash}`} className="import-strip">
          <span>
            {t("rvw.importFrom")} <b>{p.gate}</b> {t("rvw.importBy", { by: p.originalApprover ?? "—", at: p.originalAt ? new Date(p.originalAt).toLocaleString(intlTag()) : "—" })}
          </span>
          {isOwner && <button type="button" disabled={!!busy} onClick={() => confirm(p.gate)}>{t("common.confirm")}</button>}
        </div>
      ))}
      {error && <p className="error" role="alert">{error}</p>}
    </div>
  );
}

export function Review({ repoId, featureKey }: { repoId: string; featureKey: string }) {
  const { t, locale } = useT();
  const [detail, setDetail] = useState<FeatureDetail | null>(null);
  const [events, setEvents] = useState<FeatureEvent[]>([]);
  const [controlActions, setControlActions] = useState<ControlAction[]>([]);
  const [attempts, setAttempts] = useState<AttemptData[]>([]);
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
      .catch((e) => { if (current.current === mine && !(e instanceof UnauthorizedError)) setError(tNow("feature.loadFailed")); });
    getEvents(repoId, featureKey, 200)
      .then((e) => { if (current.current === mine) setEvents(e as FeatureEvent[]); })
      .catch(() => { /* lỗi 401 đã chuyển trang; lỗi khác giữ danh sách cũ */ });
    getAttempts(repoId, featureKey)
      .then((a) => { if (current.current === mine) setAttempts(a); })
      .catch(() => { /* giữ danh sách cũ */ });
    loadControl();
  }, [repoId, featureKey, loadControl]);

  useEffect(() => {
    // đổi tính năng: bỏ snapshot, event và lỗi của tính năng trước
    setDetail(null); setEvents([]); setControlActions([]); setAttempts([]); setError(""); setSelected(""); applied.current = "";
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
        id: g.gate, title: GATE_TITLE[g.gate], sub: [stateText(g.state), approvalBadge(g)].filter(Boolean).join(" · "), tone: gateTone(g.state), group: "gate",
      })),
      ...s.steps.map((st): GateItem => ({ id: st.id, title: st.id, sub: st.title, status: stepText(st.status), tone: stepTone(st.status), group: "step" })),
    ];
  }, [detail, locale]);

  if (!detail) {
    return (
      <main className="page">
        <p><Link to="/">← {t("nav.board")}</Link></p>
        {error ? <p className="error banner" role="alert">{error}</p> : <p className="muted">{t("common.loading")}</p>}
      </main>
    );
  }

  const snap = detail.snapshot;
  const gate = snap.gates.find((g) => g.gate === selected) ?? null;
  const step = snap.steps.find((s) => s.id === selected) ?? null;

  return (
    <div className="page">
      <a className="skip-link" href="#rv-main">{t("rvw.skipToContent")}</a>
      <header className="review-head">
        <Link to="/">← {t("nav.board")}</Link>
        <h1><span className="key">{snap.feature.key}</span>{snap.feature.title && snap.feature.title !== snap.feature.key && <> {snap.feature.title}</>}</h1>
        <span className="tag">{snap.feature.flow}</span>
        {(detail.attempt ?? 1) > 1 && <span className="tag">{t("card.attempt", { n: detail.attempt! })}</span>}
        <span className="spacer" />
        <div className="meta">
          <span>{snap.repo.id} · {snap.repo.branch}</span>
          <span>{t("rvw.owner", { owner: detail.owner ?? "—" })}</span>
          <span>{t("rvw.synced", { ago: ago(detail.syncedAt, now) })}</span>
          {detail.clockSkew && <span className="warn" title={t("rvw.skewTitle")}>{t("rvw.skew")}</span>}
          {detail.approvalsMode === "web" && <span className="tag" title={t("rvw.webModeTitle")}>{t("rvw.webMode")}</span>}
          <Link to={featureHref(repoId, featureKey, true)}>{t("rvw.watchAgent")} →</Link>
        </div>
        <UserMenu />
      </header>
      {error && <p className="error banner" role="alert">{error}</p>}
      {snap.truncated && <p className="banner">{t("rvw.truncatedSnapshot")}</p>}
      {(detail.pendingImports ?? []).length > 0 && (
        <PendingImports items={detail.pendingImports!} owner={detail.owner} repoId={repoId} featureKey={featureKey} onDone={load} />
      )}
      <div className="review">
        <GateList items={items} selected={selected} onSelect={select} />
        <section className="review-main" id="rv-main" tabIndex={-1} aria-label={t("rvw.content")}>
          <h2>{gate ? `${GATE_TITLE[gate.gate]}` : step ? `${step.id} · ${step.title}` : ""}</h2>
          {gate && (
            <ErrorBoundary key={`${repoId}\n${featureKey}\n${gate.gate}`} fallback={
              <>
                <p className="muted">{t("rvw.cardFailed")}</p>
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
      <AttemptArchive attempts={attempts} />
    </div>
  );
}
