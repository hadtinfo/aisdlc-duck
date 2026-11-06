import { useRef, useState } from "react";
import { type PolicyChange, type PolicyVersion, type PolicyView, changeId, changeText } from "../risk";
import { RiskDialog } from "./RiskDialog";

const SOURCE: Record<string, string> = { import: "nhập", web: "web", "preset-update": "cập nhật mẫu" };
const when = (iso: string) => new Date(iso).toLocaleString("vi-VN");

/**
 * Tab "Phiên bản": dòng thời gian (số tính năng đang gắn mỗi phiên bản), thông báo mẫu mới kèm diff — mỗi thay đổi
 * "Áp dụng vào nháp" hoặc "Bỏ qua" (chỉ ẩn trong phiên làm việc) — và nút "Tải YAML".
 */
export function RiskVersions({ view, versions, error, handled, onApply, onSkip, onSkipAll }: {
  view: PolicyView; versions: PolicyVersion[] | null; error: string;
  handled: Record<string, "applied" | "skipped">; onApply: (c: PolicyChange) => void; onSkip: (c: PolicyChange) => void;
  onSkipAll: () => void;
}) {
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
            <b>aisdlc có mẫu {u.name} mới</b> (bản {u.from} → {u.to}) · {u.changes.length} thay đổi
            {u.baseMissing ? " so với chính sách đang dùng (không còn mẫu bản " + u.from + " để so)" : " so với mẫu bạn đã nhập"}.
            {" "}Không tự ghi đè; chọn phần muốn áp dụng.
          </div>
          <button type="button" className="ghost" onClick={() => setShowDiff((x) => !x)} aria-expanded={showDiff}>
            {showDiff ? "Ẩn diff mẫu" : "Xem diff mẫu"}
          </button>
        </div>
      )}
      {u && showDiff && (
        <section className="box">
          <div className="rk-head">
            <h2>Mẫu {u.name}: {u.from} → {u.to}</h2>
            {open > 0 && <button type="button" className="secondary" onClick={onSkipAll}>Bỏ qua tất cả còn lại</button>}
          </div>
          <p className="muted" style={{ fontSize: 12.5, margin: "0 0 10px" }}>
            {open > 0
              ? `Còn ${open} thay đổi chưa áp dụng hoặc bỏ qua; nháp lên mẫu bản ${u.to} khi đã xem hết.`
              : `Đã xem hết; nháp ghi mẫu bản ${u.to}, xuất bản để đóng thông báo.`}
          </p>
          {pending.length === 0 ? <p className="muted">Không còn thay đổi nào cần xem.</p> : (
            <div className="rk-stack">
              {pending.map((c) => (
                <div key={changeId(c)} className="rk-row between">
                  <pre className="rk-diff"><span className={c.kind.startsWith("REMOVE") ? "del" : c.kind.startsWith("ADD") ? "add" : ""}>{changeText(c)}</span>
                    {c.loosening && <span className="del">{"\n"}(nới yêu cầu)</span>}</pre>
                  {handled[changeId(c)] === "applied" ? <span className="muted" style={{ fontSize: 12.5 }}>Đã áp dụng vào nháp</span> : (
                    <span className="rk-row" style={{ flexWrap: "nowrap" }}>
                      <button type="button" className="ghost" disabled={!u.policy} onClick={() => onApply(c)}>Áp dụng vào nháp</button>
                      <button type="button" className="ghost" onClick={() => onSkip(c)}>Bỏ qua</button>
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
          <h2>Phiên bản</h2>
          <span className="rk-row">
            <span className="muted" style={{ fontSize: 12.5 }}>Tính năng giữ phiên bản lúc được phân loại; chuyển phiên bản phải chủ động</span>
            <button type="button" className="secondary" disabled={!view.yaml} onClick={() => setYaml(true)}>Tải YAML</button>
          </span>
        </div>
        {error && <p className="error" role="alert">{error}</p>}
        {!versions ? (!error && <p className="muted">Đang tải…</p>) : (
          <div>
            {versions.map((v) => {
              const n = view.featureCounts[String(v.version)] ?? 0;
              return (
                <div key={v.version} className="rk-ver">
                  <span className="v">v{v.version}</span>
                  <div>
                    <div>
                      {v.version === view.version && <><span className="rk-live">ĐANG HIỆU LỰC</span> · </>}
                      <span className="who">{v.author} · {when(v.createdAt)} · {SOURCE[v.source] ?? v.source}</span>
                    </div>
                    <div style={{ fontSize: 13.5, marginTop: 2 }}>{v.reason}</div>
                    {v.changes.length > 0 && (
                      <ul>{v.changes.map((c, i) => <li key={i} className={c.loosening ? "warn" : ""}>{changeText(c)}{c.loosening && " (nới)"}</li>)}</ul>
                    )}
                  </div>
                  <span className="muted" style={{ fontSize: 12.5, whiteSpace: "nowrap" }}>{n > 0 ? `${n} tính năng` : ""}</span>
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
        Chép vào <code>aisdlc/risk-policy.yaml</code> rồi commit nếu muốn repo giữ bản này (dùng khi chạy không có web).
      </p>
      <textarea ref={ref} className="rk-yaml" readOnly value={yaml} onFocus={(e) => e.currentTarget.select()} />
      <div className="rk-row end">
        <button type="button" onClick={copy}>{state === "copied" ? "Đã chép" : state === "selected" ? "Nhấn Ctrl+C" : "Copy"}</button>
      </div>
    </RiskDialog>
  );
}
