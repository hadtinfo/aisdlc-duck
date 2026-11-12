import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { ApiError, UnauthorizedError } from "../api";
import { CopyCommand } from "../components/CopyCommand";
import { RiskDialog } from "../components/RiskDialog";
import { RiskRequirements } from "../components/RiskRequirements";
import { RiskRules } from "../components/RiskRules";
import { RiskVersions } from "../components/RiskVersions";
import { UserMenu } from "../components/UserMenu";
import {
  type Draft, type Policy, type PolicyChange, type PolicyVersion, type PolicyView, type PresetHandled, allPresetHandled,
  applyPresetChange, changeId, changeText, clearDraft, draftChanges, loadDraft, loadPolicy, migrateDraft, presetVersionFor, publish,
  saveDraft, skipAllRemaining, validate, versions,
} from "../risk";
import { type RiskTab, riskTab } from "../routes";
import "../risk.css";

const TABS: [RiskTab, string][] = [
  ["rules", "Rule nhận diện"], ["levels", "Yêu cầu theo mức"], ["history", "Phiên bản"],
];

/** Trang /risk/<repo>: chính sách rủi ro của repo, 3 tab theo hash, nháp lưu localStorage, dev tự xuất bản (spec §6). */
export function RiskPolicy({ repoId }: { repoId: string }) {
  const tab = riskTab(useLocation().hash);
  const [view, setView] = useState<PolicyView | null>(null);
  const [state, setState] = useState<"loading" | "none" | "ok" | "error">("loading");
  const [draft, setDraft] = useState<Draft | null>(null);
  const [vers, setVers] = useState<PolicyVersion[] | null>(null);
  const [versError, setVersError] = useState("");
  const [handled, setHandled] = useState<PresetHandled>({});
  const [rebasedTo, setRebasedTo] = useState<number | null>(null);
  const [dialog, setDialog] = useState<"diff" | "publish" | null>(null);
  const [toast, setToast] = useState("");

  const say = useCallback((msg: string) => {
    setToast(msg);
    setTimeout(() => setToast((t) => (t === msg ? "" : t)), 2600);
  }, []);

  const load = useCallback(async (): Promise<PolicyView | null> => {
    versions(repoId).then((v) => { setVers(v); setVersError(""); })
      .catch((e) => { if (!(e instanceof UnauthorizedError)) setVersError("Không tải được danh sách phiên bản"); });
    try {
      const v = await loadPolicy(repoId);
      setView(v);
      setState(v ? "ok" : "none");
      return v;
    } catch (e) {
      if (!(e instanceof UnauthorizedError)) setState("error");
      return null;
    }
  }, [repoId]);

  useEffect(() => {
    setView(null); setState("loading"); setVers(null); setHandled({}); setRebasedTo(null);
    setDraft(loadDraft(repoId));
    void load();
  }, [repoId, load]);

  const policy: Policy | null = draft?.policy ?? view?.policy ?? null;
  const changes = useMemo(() => (view && draft ? draftChanges(view.policy, draft.policy) : []), [view, draft]);

  /** `fromPreset` vắng thì giữ cờ của nháp (sửa thường không đổi nguồn). */
  const update = useCallback((p: Policy, fromPreset?: boolean) => {
    if (!view) return;
    if (draftChanges(view.policy, p).length === 0) {
      clearDraft(repoId);
      setDraft(null);
      return;
    }
    const next: Draft = { baseVersion: draft?.baseVersion ?? view.version, policy: p, fromPreset: fromPreset ?? !!draft?.fromPreset };
    saveDraft(repoId, next);
    setDraft(next);
  }, [view, draft, repoId]);

  function rebase(on: PolicyView) {
    if (!draft) return;
    const next = { ...draft, baseVersion: on.version };
    saveDraft(repoId, next);
    setDraft(next);
    setRebasedTo(on.version);
  }

  function discard() {
    clearDraft(repoId);
    setDraft(null);
    setHandled({});
    setRebasedTo(null);
    say("Đã bỏ nháp");
  }

  /** Ghi bảng áp/bỏ qua mẫu mới; bản mẫu của nháp chỉ lên khi đã xem hết mọi thay đổi. */
  function handle(next: PresetHandled, from?: Policy) {
    const u = view?.presetUpdate;
    setHandled(next);
    if (!view || !u || !policy) return;
    const all = allPresetHandled(u, next);
    update(presetVersionFor(from ?? policy, view.policy, u, next), all);
  }

  function apply(c: PolicyChange) {
    const latest = view?.presetUpdate?.policy;
    if (!latest || !policy) return;
    handle({ ...handled, [changeId(c)]: "applied" }, applyPresetChange(policy, latest, c));
    say("Đã áp dụng vào nháp");
  }

  /** Nạp bản nháp schema 2 từ server vào trình sửa; xem lại rồi xuất bản như mọi phiên bản. */
  async function migrate() {
    if (!view) return;
    try {
      const r = await migrateDraft(repoId);
      const next: Draft = { baseVersion: view.version, policy: r.policy };
      saveDraft(repoId, next);
      setDraft(next);
      setHandled({});
      setRebasedTo(null);
      say("Đã nạp bản nháp schema 2; xem lại rồi xuất bản");
    } catch (e) {
      if (!(e instanceof UnauthorizedError)) say(e instanceof ApiError ? e.message : "Không kết nối được máy chủ");
    }
  }

  const repoName = repoId.split("/").slice(-1)[0];
  const features = view ? Object.values(view.featureCounts).reduce((a, b) => a + b, 0) : 0;
  const stale = !!(view && draft && draft.baseVersion !== view.version);

  return (
    <div className="page narrow rk">
      <header className="topbar">
        <Link to="/repos">← Repo</Link>
        <h1>Chính sách rủi ro · {repoName}</h1>
        <span className="spacer" />
        <UserMenu />
      </header>
      {state === "loading" && <p className="muted">Đang tải…</p>}
      {state === "error" && <p className="error banner" role="alert">Không tải được chính sách rủi ro</p>}
      {state === "none" && (
        <section className="box">
          <h2>Repo <code>{repoId}</code> chưa có chính sách rủi ro trên web</h2>
          <p>Chính sách được nhập lần đầu từ <code>aisdlc/risk-policy.yaml</code> (không có thì mẫu generic) khi CLI kéo chính sách.
            Chạy trong repo:</p>
          <CopyCommand command="aisdlc policy pull" />
        </section>
      )}
      {view && policy && (
        <>
          <div className="rk-meta">
            <span>Đang hiệu lực <b className="mono">v{view.version}</b></span>
            {view.policy.preset && <>· <span>Gốc: <span className="tag preset">mẫu {view.policy.preset.name}</span> bản {view.policy.preset.version}</span></>}
            · <span>{features} tính năng đang gắn phiên bản</span>
            · <span className="mono" title={repoId}>{repoId}</span>
          </div>
          {view.policy.schema === 1 && (
            <div className="rk-note warn" style={{ marginBottom: 16 }}>
              <div className="grow">Chính sách đang hiệu lực dùng <b>schema 1</b> (luật so khớp) và chưa phân loại được.
                Chuyển sang schema 2: mỗi luật thành một mô tả bằng lời; bản nháp nạp vào trình sửa để bạn xem lại và xuất bản
                (cần ghi lý do vì mô tả mới có thể nới luật).</div>
              <button type="button" onClick={() => void migrate()}>Chuyển sang schema 2</button>
            </div>
          )}
          <nav className="rk-tabs" aria-label="Chính sách rủi ro">
            {TABS.map(([id, name]) => (
              <Link key={id} to={{ hash: id }} aria-current={tab === id ? "page" : undefined}>
                {name}
                {id === "rules" && <span className="count">{policy.rules.filter((r) => r.enabled).length}</span>}
                {id === "history" && view.presetUpdate && <span className="count">mẫu mới</span>}
              </Link>
            ))}
          </nav>
          {stale && (
            <div className="rk-note warn" style={{ marginBottom: 16 }}>
              <div className="grow">Nháp này làm trên <b>v{draft!.baseVersion}</b>; bản đang hiệu lực là <b>v{view.version}</b>.
                Danh sách thay đổi đã so với v{view.version}; xem lại rồi làm tiếp trên bản này.</div>
              <button type="button" className="ghost" onClick={() => rebase(view)}>Làm tiếp trên v{view.version}</button>
            </div>
          )}
          {rebasedTo != null && draft && rebasedTo === view.version && (
            <div className="rk-note warn" style={{ marginBottom: 16 }}>
              <div className="grow">Nháp đã chuyển lên v{rebasedTo}; các dòng xóa/đổi trong danh sách có thể đang ghi đè thay đổi
                của người khác — xem kỹ trước khi xuất bản.</div>
            </div>
          )}
          {tab === "rules" && <RiskRules policy={policy} base={view.policy} presetRuleIds={view.presetRuleIds ?? []} onChange={update} />}
          {tab === "levels" && <RiskRequirements policy={policy} base={view.policy} onChange={update} />}
          {tab === "history" && (
            <RiskVersions view={view} versions={vers} error={versError} handled={handled} onApply={apply}
              onSkip={(c) => handle({ ...handled, [changeId(c)]: "skipped" })}
              onSkipAll={() => view.presetUpdate && handle(skipAllRemaining(view.presetUpdate, handled))} />
          )}
        </>
      )}

      {view && draft && changes.length > 0 && (
        <div className="rk-draftbar" role="region" aria-label="Bản nháp">
          <div className="in">
            <div className="grow"><b>Bản nháp v{view.version + 1}: {changes.length} thay đổi</b>{" "}
              <span className="muted">· có hiệu lực ngay khi xuất bản</span></div>
            <button type="button" className="secondary" onClick={discard}>Bỏ nháp</button>
            <button type="button" className="secondary" onClick={() => setDialog("diff")}>Xem thay đổi</button>
            <button type="button" onClick={() => setDialog("publish")}>Xuất bản</button>
          </div>
        </div>
      )}
      {dialog === "diff" && (
        <RiskDialog title="Thay đổi trong nháp" onClose={() => setDialog(null)}>
          <ul>{changes.map((c, i) => <li key={i}>{c}</li>)}</ul>
        </RiskDialog>
      )}
      {dialog === "publish" && view && draft && (
        <PublishDialog repoId={repoId} draft={draft} changes={changes} onClose={() => setDialog(null)}
          onDone={(v) => { clearDraft(repoId); setDraft(null); setHandled({}); setRebasedTo(null); setDialog(null); void load(); say(`Đã xuất bản v${v}`); }}
          onReload={async () => {
            setDialog(null);
            const v = await load();
            if (v) { rebase(v); say(`Đã tải v${v.version}; thay đổi trong nháp giờ so với v${v.version}`); }
          }} />
      )}
      {toast && <div className="rk-toast" role="status">{toast}</div>}
    </div>
  );
}

/** Xuất bản: validate trên server, bắt buộc lý do, liệt kê phần bị nới; 409 thì hiện câu của server và nút "Tải lại". */
function PublishDialog({ repoId, draft, changes, onClose, onDone, onReload }: {
  repoId: string; draft: Draft; changes: string[]; onClose: () => void; onDone: (version: number) => void; onReload: () => void;
}) {
  const [check, setCheck] = useState<{ errors: string[]; changes: PolicyChange[] } | null>(null);
  const [checkError, setCheckError] = useState("");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState("");

  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    setCheck(null);
    setCheckError("");
    validate(draft.policy, repoId)
      .then((r) => { if (live) setCheck(r); })
      .catch((e) => { if (live) setCheckError(e instanceof ApiError ? e.message : "Không kết nối được máy chủ"); });
    return () => { live = false; };
  }, [draft, repoId, attempt]);

  const loosening = (check?.changes ?? []).filter((c) => c.loosening);
  const invalid = (check?.errors ?? []).length > 0;

  async function go() {
    if (!reason.trim()) { setError("Ghi lý do trước khi xuất bản"); return; }
    setBusy(true);
    setError("");
    try {
      const r = await publish(repoId, draft.baseVersion, draft.policy, reason.trim(), !!draft.fromPreset);
      onDone(r.version);
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) setConflict(e.message);
      else if (!(e instanceof UnauthorizedError)) setError(e instanceof ApiError ? e.message : "Không kết nối được máy chủ");
    } finally {
      setBusy(false);
    }
  }

  return (
    <RiskDialog title="Xuất bản chính sách" onClose={onClose}>
      <ul>{changes.map((c, i) => <li key={i}>{c}</li>)}</ul>
      {!check && !checkError && <p className="muted">Đang kiểm tra chính sách…</p>}
      {checkError && (
        <div className="rk-note bad">
          <div className="grow" role="alert">Không kiểm tra được chính sách: {checkError}</div>
          <button type="button" className="secondary" onClick={() => setAttempt((n) => n + 1)}>Thử lại</button>
        </div>
      )}
      {invalid && (
        <div className="rk-note bad"><div className="grow">
          <b>Chính sách chưa hợp lệ, sửa rồi xuất bản lại:</b>
          <ul className="rk-errors">{check!.errors.map((e, i) => <li key={i}>{e}</li>)}</ul>
        </div></div>
      )}
      {loosening.length > 0 && (
        <div className="rk-note warn"><div className="grow">
          <h3>Thay đổi này nới yêu cầu</h3>
          <ul>{loosening.map((c, i) => <li key={i}>{changeText(c)}</li>)}</ul>
          <p className="muted" style={{ fontSize: 12.5, marginTop: 4 }}>Vẫn xuất bản được vì dev tự duyệt; kiểm lại phần bị nới.</p>
        </div></div>
      )}
      <label className="rk-field"><span>Lý do thay đổi (bắt buộc)</span>
        <input type="text" value={reason} autoFocus placeholder="Ví dụ: thêm module hoàn tiền" onChange={(e) => setReason(e.target.value)} />
      </label>
      <p className="muted" style={{ fontSize: 12.5, margin: 0 }}>
        Tính năng đang làm giữ phiên bản cũ. Tính năng mới phân loại sau thời điểm có hiệu lực dùng phiên bản này.
      </p>
      {conflict && (
        <div className="rk-note bad">
          <div className="grow" role="alert">{conflict}</div>
          <button type="button" onClick={onReload}>Tải lại</button>
        </div>
      )}
      {error && <p className="error" role="alert">{error}</p>}
      <div className="rk-row end">
        <button type="button" className="secondary" onClick={onClose}>Hủy</button>
        <button type="button" disabled={busy || !check || invalid || !!conflict} onClick={go}>Xuất bản</button>
      </div>
    </RiskDialog>
  );
}
