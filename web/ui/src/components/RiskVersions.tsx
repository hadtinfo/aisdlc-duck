import { useRef, useState } from "react";
import { type PolicyChange, type PolicyVersion, type PolicyView, changeId, changeText } from "../risk";
import { RiskDialog } from "./RiskDialog";
import { Key, intlTag, useT } from "../i18n";

const SOURCE: Record<string, Key> = { import: "rv.src.import", web: "rv.src.web", "preset-update": "rv.src.preset" };
const when = (iso: string) => new Date(iso).toLocaleString(intlTag());

/**
 * Tab "Phiên bản": dòng thời gian (số tính năng đang gắn mỗi phiên bản), thông báo mẫu mới kèm diff — mỗi thay đổi
 * "Áp dụng vào nháp" hoặc "Bỏ qua" (chỉ ẩn trong phiên làm việc) — và nút "Tải YAML".
 */
export function RiskVersions({ view, versions, error, handled, onApply, onSkip, onSkipAll }: {
  view: PolicyView; versions: PolicyVersion[] | null; error: string;
  handled: Record<string, "applied" | "skipped">; onApply: (c: PolicyChange) => void; onSkip: (c: PolicyChange) => void;
  onSkipAll: () => void;
}) {
  const { t } = useT();
  const [showDiff, setShowDiff] = useState(false);
  const [yaml, setYaml] = useState(false);
  const u = view.presetUpdate;
  const pending = u ? u.changes.filter((c) => handled[changeId(c)] !== "skipped") : [];
  const open = u ? u.changes.filter((c) => !handled[changeId(c)]).length : 0;
  return (
    <div className="rk-stack">
      {u && (
        <div className="rk-note info">
          <div className="grow">
            <b>{t("rv.newPreset", { name: u.name })}</b> {t("rv.presetRange", { from: u.from, to: u.to, n: u.changes.length })}
            {u.baseMissing ? t("rv.vsCurrent", { from: u.from }) : t("rv.vsImported")}.
            {" "}{t("rv.noOverwrite")}
          </div>
          <button type="button" className="ghost" onClick={() => setShowDiff((x) => !x)} aria-expanded={showDiff}>
            {showDiff ? t("rv.hideDiff") : t("rv.showDiff")}
          </button>
        </div>
      )}
      {u && showDiff && (
        <section className="box">
          <div className="rk-head">
            <h2>{t("rv.presetTitle", { name: u.name, from: u.from, to: u.to })}</h2>
            {open > 0 && <button type="button" className="secondary" onClick={onSkipAll}>{t("rv.skipAll")}</button>}
          </div>
          <p className="muted" style={{ fontSize: 12.5, margin: "0 0 10px" }}>
            {open > 0
              ? t("rv.openLeft", { n: open, to: u.to })
              : t("rv.allSeen", { to: u.to })}
          </p>
          {pending.length === 0 ? <p className="muted">{t("rv.nothingLeft")}</p> : (
            <div className="rk-stack">
              {pending.map((c) => (
                <div key={changeId(c)} className="rk-row between">
                  <pre className="rk-diff"><span className={c.kind.startsWith("REMOVE") ? "del" : c.kind.startsWith("ADD") ? "add" : ""}>{changeText(c)}</span>
                    {c.loosening && <span className="del">{"\n"}{t("rv.loosening")}</span>}</pre>
                  {handled[changeId(c)] === "applied" ? <span className="muted" style={{ fontSize: 12.5 }}>{t("rv.applied")}</span> : (
                    <span className="rk-row" style={{ flexWrap: "nowrap" }}>
                      <button type="button" className="ghost" disabled={!u.policy} onClick={() => onApply(c)}>{t("rv.apply")}</button>
                      <button type="button" className="ghost" onClick={() => onSkip(c)}>{t("rv.skip")}</button>
                    </span>
                  )}
                </div>
              ))}
            </div>
          )}
        </section>
      )}
      <section className="box">
        <div className="rk-head">
          <h2>{t("rv.versions")}</h2>
          <span className="rk-row">
            <span className="muted" style={{ fontSize: 12.5 }}>{t("rv.pinNote")}</span>
            <button type="button" className="secondary" disabled={!view.yaml} onClick={() => setYaml(true)}>{t("rv.downloadYaml")}</button>
          </span>
        </div>
        {error && <p className="error" role="alert">{error}</p>}
        {!versions ? (!error && <p className="muted">{t("common.loading")}</p>) : (
          <div>
            {versions.map((v) => {
              const n = view.featureCounts[String(v.version)] ?? 0;
              return (
                <div key={v.version} className="rk-ver">
                  <span className="v">v{v.version}</span>
                  <div>
                    <div>
                      {v.version === view.version && <><span className="rk-live">{t("rv.live")}</span> · </>}
                      <span className="who">{v.author} · {when(v.createdAt)} · {SOURCE[v.source] ? t(SOURCE[v.source]) : v.source}</span>
                    </div>
                    <div style={{ fontSize: 13.5, marginTop: 2 }}>{v.reason}</div>
                    {v.changes.length > 0 && (
                      <ul>{v.changes.map((c, i) => <li key={i} className={c.loosening ? "warn" : ""}>{changeText(c)}{c.loosening && ` ${t("rv.loosenedShort")}`}</li>)}</ul>
                    )}
                  </div>
                  <span className="muted" style={{ fontSize: 12.5, whiteSpace: "nowrap" }}>{n > 0 ? t("rv.features", { n }) : ""}</span>
                </div>
              );
            })}
          </div>
        )}
      </section>
      {yaml && view.yaml && <YamlDialog version={view.version} yaml={view.yaml} onClose={() => setYaml(false)} />}
    </div>
  );
}

/** YAML của bản đang hiệu lực trong khung chọn được, kèm nút Copy (không dùng <a download>). */
function YamlDialog({ version, yaml, onClose }: { version: number; yaml: string; onClose: () => void }) {
  const { t } = useT();
  const ref = useRef<HTMLTextAreaElement>(null);
  const [state, setState] = useState<"idle" | "copied" | "selected">("idle");
  async function copy() {
    try {
      await navigator.clipboard.writeText(yaml);
      setState("copied");
    } catch {
      ref.current?.focus();
      ref.current?.select();
      setState("selected");
    }
    setTimeout(() => setState("idle"), 1800);
  }
  return (
    <RiskDialog title={`aisdlc/risk-policy.yaml · v${version}`} onClose={onClose}>
      <p className="muted" style={{ margin: 0, fontSize: 12.5 }}>
        {t("rv.copyInto")} <code>aisdlc/risk-policy.yaml</code> {t("rv.copyCommit")}
      </p>
      <textarea ref={ref} className="rk-yaml" readOnly value={yaml} onFocus={(e) => e.currentTarget.select()} />
      <div className="rk-row end">
        <button type="button" onClick={copy}>{state === "copied" ? t("copy.done") : state === "selected" ? t("copy.pressCtrlC") : "Copy"}</button>
      </div>
    </RiskDialog>
  );
}
