import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { UnauthorizedError, getControl, getEvents, getFeature } from "../api";
import { ControlPanel } from "../components/ControlPanel";
import { EditTimeline } from "../components/EditTimeline";
import { LiveStep, StepProgress } from "../components/StepProgress";
import { LiveEvent, editsByStep, mergeEvent, sessionState } from "../live";
import { eventReloadsControl } from "../control";
import { encodePath, featureHref } from "../routes";
import { subscribe } from "../sse";
import type { ControlState } from "../types";

interface FeatureResp { snapshot: { feature?: { key?: string; title?: string }; steps?: LiveStep[] }; syncedAt: string; owner?: string | null }

const SESSION_TEXT = { ACTIVE: "Agent đang chạy", STOPPED: "Đã dừng", NONE: "Chưa có phiên" } as const;

export function Live({ repoId, featureKey }: { repoId: string; featureKey: string }) {
  const [feature, setFeature] = useState<FeatureResp | null>(null);
  const [events, setEvents] = useState<LiveEvent[]>([]);
  const [control, setControl] = useState<ControlState | null>(null);
  const [error, setError] = useState("");
  const [now, setNow] = useState(() => new Date());

  const loadSnapshot = useCallback(() => {
    getFeature(repoId, featureKey)
      .then((f) => { setFeature(f as FeatureResp); setError(""); })
      .catch((e) => { if (!(e instanceof UnauthorizedError)) setError("Không tải được tính năng"); });
  }, [repoId, featureKey]);

  const loadControl = useCallback(() => {
    getControl(repoId, featureKey).then(setControl).catch(() => { /* giữ trạng thái cũ; 401 đã chuyển trang */ });
  }, [repoId, featureKey]);

  const loadAll = useCallback(() => {
    loadSnapshot();
    loadControl();
    getEvents(repoId, featureKey, 200)
      .then((ev) => setEvents(ev as LiveEvent[]))
      .catch((e) => { if (!(e instanceof UnauthorizedError)) setError("Không tải được event"); });
  }, [repoId, featureKey, loadSnapshot, loadControl]);

  useEffect(() => {
    loadAll();
    const stop = subscribe(`/api/features/${encodePath(repoId, featureKey)}/stream`, (type, data) => {
      if (type === "snapshot") { loadSnapshot(); loadControl(); }
      else if (type === "event") {
        try {
          const ev = JSON.parse(data) as LiveEvent;
          setEvents((cur) => mergeEvent(cur, ev));
          if (eventReloadsControl(ev.type)) loadControl();
        } catch { /* bỏ qua dữ liệu hỏng */ }
      }
    }, loadAll);
    const tick = setInterval(() => setNow(new Date()), 30_000);
    return () => { stop(); clearInterval(tick); };
  }, [repoId, featureKey, loadAll, loadSnapshot, loadControl]);

  const session = sessionState(events, now);
  const edits = useMemo(() => editsByStep(events), [events]);
  const steps = feature?.snapshot.steps ?? [];
  const title = feature?.snapshot.feature?.title ?? "";

  return (
    <main className="page live">
      <p className="small"><Link to="/">&larr; Bảng</Link></p>
      <header className="live-head">
        <div>
          <span className="jira-key">{featureKey}</span>
          <h1>{title}</h1>
          <p className="muted small">{repoId} · <Link to={featureHref(repoId, featureKey)}>Xem review</Link></p>
        </div>
        <span className={`session session-${session.toLowerCase()}`}><i className="dot" />{SESSION_TEXT[session]}</span>
      </header>
      {error && <p className="error banner" role="alert">{error}</p>}
      <div className="live-grid">
        <section aria-label="Các bước">
          <h2>Các bước</h2>
          <p className="muted small">Số dòng và số file chỉ cập nhật khi có snapshot mới. Số dòng cập nhật sau mỗi lệnh step</p>
          <div className="steps">
            {steps.length === 0 && feature && <p className="muted empty">Chưa có bước nào.</p>}
            {steps.map((s) => <StepProgress key={s.id} step={s} edits={edits.get(s.id) ?? []} />)}
          </div>
        </section>
        <div className="live-side">
          <ControlPanel control={control} owner={feature?.owner ?? null}
            repoId={repoId} featureKey={featureKey} onChanged={loadControl} />
          <section aria-label="Dòng thời gian">
            <h2>Sửa file trực tiếp</h2>
            <EditTimeline events={events} />
          </section>
        </div>
      </div>
    </main>
  );
}
