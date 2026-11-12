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
import { Key, t as tNow, useT } from "../i18n";
import "../risk.css";

const TABS: [RiskTab, Key][] = [
  ["rules", "rr.title"], ["levels", "rq.title"], ["history", "rv.versions"],
];

/** Trang /risk/<repo>: chính sách rủi ro của repo, 3 tab theo hash, nháp lưu localStorage, dev tự xuất bản (spec §6). */
export function RiskPolicy({ repoId }: { repoId: string }) {
  const { t, locale } = useT();
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
      .catch((e) => { if (!(e instanceof UnauthorizedError)) setVersError(tNow("rp.versionsFailed")); });
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
  const changes = useMemo(() => (view && draft ? draftChanges(view.policy, draft.policy) : []), [view, draft, locale]);

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
    say(tNow("rp.discarded"));
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
    say(tNow("rv.applied"));
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
      say(tNow("rp.migrated"));
    } catch (e) {
      if (!(e instanceof UnauthorizedError)) say(e instanceof ApiError ? e.message : tNow("common.noServer"));
    }
  }

  const repoName = repoId.split("/").slice(-1)[0];
  const features = view ? Object.values(view.featureCounts).reduce((a, b) => a + b, 0) : 0;
  const stale = !!(view && draft && draft.baseVersion !== view.version);

  return (
    <div className="page narrow rk">
      <header className="topbar">
        <Link to="/repos">← Repo</Link>
        <h1>{t("risk.policy")} · {repoName}</h1>
        <span className="spacer" />
        <UserMenu />
      </header>
      {state === "loading" && <p className="muted">{t("common.loading")}</p>}
      {state === "error" && <p className="error banner" role="alert">{t("rp.loadFailed")}</p>}
      {state === "none" && (
        <section className="box">
          <h2>Repo <code>{repoId}</code> {t("rp.noneOnWeb")}</h2>
          <p>{t("rp.firstImport")} <code>aisdlc/risk-policy.yaml</code> {t("rp.firstImport2")}</p>
          <CopyCommand command="aisdlc policy pull" />
        </section>
      )}
      {view && policy && (
        <>
          <div className="rk-meta">
            <span>{t("rp.live")} <b className="mono">v{view.version}</b></span>
            {view.policy.preset && <>· <span>{t("rp.origin")} <span className="tag preset">{t("rr.preset", { name: view.policy.preset.name })}</span> {t("rp.presetVersion", { v: view.policy.preset.version })}</span></>}
            · <span>{t("rp.featuresPinned", { n: features })}</span>
            · <span className="mono" title={repoId}>{repoId}</span>
          </div>
          {view.policy.schema === 1 && (
            <div className="rk-note warn" style={{ marginBottom: 16 }}>
              <div className="grow">{t("rp.schema1a")} <b>schema 1</b> {t("rp.schema1b")}</div>
              <button type="button" onClick={() => void migrate()}>{t("rp.migrate")}</button>
            </div>
          )}
          <nav className="rk-tabs" aria-label={t("risk.policy")}>
            {TABS.map(([id, name]) => (
              <Link key={id} to={{ hash: id }} aria-current={tab === id ? "page" : undefined}>
                {t(name)}
                {id === "rules" && <span className="count">{policy.rules.filter((r) => r.enabled).length}</span>}
                {id === "history" && view.presetUpdate && <span className="count">{t("rp.newPreset")}</span>}
              </Link>
            ))}
          </nav>
          {stale && (
            <div className="rk-note warn" style={{ marginBottom: 16 }}>
              <div className="grow">{t("rp.staleOn")} <b>v{draft!.baseVersion}</b>{t("rp.staleLive")} <b>v{view.version}</b>.{" "}
                {t("rp.staleRest", { live: view.version })}</div>
              <button type="button" className="ghost" onClick={() => rebase(view)}>{t("rp.continueOn", { v: view.version })}</button>
            </div>
          )}
          {rebasedTo != null && draft && rebasedTo === view.version && (
            <div className="rk-note warn" style={{ marginBottom: 16 }}>
              <div className="grow">{t("rp.rebased", { v: rebasedTo })}</div>
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
        <div className="rk-draftbar" role="region" aria-label={t("rp.draft")}>
          <div className="in">
            <div className="grow"><b>{t("rp.draftSummary", { v: view.version + 1, n: changes.length })}</b>{" "}
              <span className="muted">· {t("rp.effectiveOnPublish")}</span></div>
            <button type="button" className="secondary" onClick={discard}>{t("rp.discard")}</button>
            <button type="button" className="secondary" onClick={() => setDialog("diff")}>{t("rp.viewChanges")}</button>
            <button type="button" onClick={() => setDialog("publish")}>{t("rp.publish")}</button>
          </div>
        </div>
      )}
      {dialog === "diff" && (
        <RiskDialog title={t("rp.draftChanges")} onClose={() => setDialog(null)}>
          <ul>{changes.map((c, i) => <li key={i}>{c}</li>)}</ul>
        </RiskDialog>
      )}
      {dialog === "publish" && view && draft && (
        <PublishDialog repoId={repoId} draft={draft} changes={changes} onClose={() => setDialog(null)}
          onDone={(v) => { clearDraft(repoId); setDraft(null); setHandled({}); setRebasedTo(null); setDialog(null); void load(); say(tNow("rp.published", { v })); }}
          onReload={async () => {
            setDialog(null);
            const v = await load();
            if (v) { rebase(v); say(tNow("rp.reloaded", { v: v.version })); }
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
  const { t } = useT();
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
      .catch((e) => { if (live) setCheckError(e instanceof ApiError ? e.message : tNow("common.noServer")); });
    return () => { live = false; };
  }, [draft, repoId, attempt]);

  const loosening = (check?.changes ?? []).filter((c) => c.loosening);
  const invalid = (check?.errors ?? []).length > 0;

  async function go() {
    if (!reason.trim()) { setError(tNow("rp.needReason")); return; }
    setBusy(true);
    setError("");
    try {
      const r = await publish(repoId, draft.baseVersion, draft.policy, reason.trim(), !!draft.fromPreset);
      onDone(r.version);
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) setConflict(e.message);
      else if (!(e instanceof UnauthorizedError)) setError(e instanceof ApiError ? e.message : tNow("common.noServer"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <RiskDialog title={t("rp.publishTitle")} onClose={onClose}>
      <ul>{changes.map((c, i) => <li key={i}>{c}</li>)}</ul>
      {!check && !checkError && <p className="muted">{t("rp.checking")}</p>}
      {checkError && (
        <div className="rk-note bad">
          <div className="grow" role="alert">{t("rp.checkFailed", { error: checkError })}</div>
          <button type="button" className="secondary" onClick={() => setAttempt((n) => n + 1)}>{t("rp.retry")}</button>
        </div>
      )}
      {invalid && (
        <div className="rk-note bad"><div className="grow">
          <b>{t("rp.invalid")}</b>
          <ul className="rk-errors">{check!.errors.map((e, i) => <li key={i}>{e}</li>)}</ul>
        </div></div>
      )}
      {loosening.length > 0 && (
        <div className="rk-note warn"><div className="grow">
          <h3>{t("rp.loosens")}</h3>
          <ul>{loosening.map((c, i) => <li key={i}>{changeText(c)}</li>)}</ul>
          <p className="muted" style={{ fontSize: 12.5, marginTop: 4 }}>{t("rp.loosensNote")}</p>
        </div></div>
      )}
      <label className="rk-field"><span>{t("rp.reason")}</span>
        <input type="text" value={reason} autoFocus placeholder={t("rp.reasonPh")} onChange={(e) => setReason(e.target.value)} />
      </label>
      <p className="muted" style={{ fontSize: 12.5, margin: 0 }}>
        {t("rp.pinExplain")}
      </p>
      {conflict && (
        <div className="rk-note bad">
          <div className="grow" role="alert">{conflict}</div>
          <button type="button" onClick={onReload}>{t("rp.reload")}</button>
        </div>
      )}
      {error && <p className="error" role="alert">{error}</p>}
      <div className="rk-row end">
        <button type="button" className="secondary" onClick={onClose}>{t("common.cancel")}</button>
        <button type="button" disabled={busy || !check || invalid || !!conflict} onClick={go}>{t("rp.publish")}</button>
      </div>
    </RiskDialog>
  );
}
