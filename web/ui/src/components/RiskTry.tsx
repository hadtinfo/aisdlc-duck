import { useEffect, useRef, useState } from "react";
import { ApiError } from "../api";
import { type ClassifyResult, type Policy, classify, splitList } from "../risk";
import { LevelPill } from "./RiskPanel";

/**
 * Tab "Thử phân loại": gọi POST /api/policy/classify (chính bộ phân loại của core) với chính sách đang xem, kể cả nháp.
 * Gõ xong 300 ms mới gọi; phản hồi cũ về muộn bị bỏ.
 */
export function RiskTry({ policy }: { policy: Policy }) {
  const [text, setText] = useState("");
  const [paths, setPaths] = useState("");
  const [tables, setTables] = useState("");
  const [endpoints, setEndpoints] = useState("");
  const [labels, setLabels] = useState("");
  const [agent, setAgent] = useState("");
  const [out, setOut] = useState<ClassifyResult | null>(null);
  const [error, setError] = useState("");
  const seq = useRef(0);

  useEffect(() => {
    const mine = ++seq.current;
    const t = setTimeout(() => {
      classify(policy, {
        text, paths: splitList(paths), tables: splitList(tables), endpoints: splitList(endpoints), labels: splitList(labels),
        agentLevel: agent || null,
      })
        .then((r) => { if (seq.current === mine) { setOut(r); setError(""); } })
        .catch((e) => {
          if (seq.current !== mine) return;
          setOut(null);
          setError(e instanceof ApiError ? `Chính sách nháp chưa hợp lệ: ${e.message}` : "Không kết nối được máy chủ");
        });
    }, 300);
    return () => clearTimeout(t);
  }, [policy, text, paths, tables, endpoints, labels, agent]);

  const r = out?.result;
  return (
    <div className="rk-grid2">
      <section className="box">
        <div className="rk-head"><h2>Thử phân loại</h2><span className="muted" style={{ fontSize: 12.5 }}>Dùng chính sách đang xem, kể cả nháp</span></div>
        <div className="rk-form">
          <label className="rk-field wide"><span>Story / BRD</span>
            <textarea rows={4} value={text} onChange={(e) => setText(e.target.value)} placeholder="Tiêu đề và nội dung story" />
          </label>
          <label className="rk-field wide"><span>Đường dẫn code các bước sẽ chạm (mỗi dòng một)</span>
            <textarea rows={3} className="mono" value={paths} onChange={(e) => setPaths(e.target.value)} placeholder="src/main/java/com/cty/pay/PaymentService.java" />
          </label>
          <label className="rk-field"><span>Bảng trong migration</span>
            <input type="text" value={tables} onChange={(e) => setTables(e.target.value)} placeholder="payment, payment.amount" />
          </label>
          <label className="rk-field"><span>Endpoint mới / đổi</span>
            <input type="text" value={endpoints} onChange={(e) => setEndpoints(e.target.value)} placeholder="POST /payments" />
          </label>
          <label className="rk-field"><span>Nhãn agent gắn</span>
            <input type="text" value={labels} onChange={(e) => setLabels(e.target.value)} placeholder="money-movement" />
          </label>
          <label className="rk-field"><span>Mức agent đề xuất</span>
            <select value={agent} onChange={(e) => setAgent(e.target.value)}>
              <option value="">(không có)</option>
              {policy.levels.map((l) => <option key={l} value={l}>{l}</option>)}
            </select>
          </label>
        </div>
      </section>
      <section className="box" aria-live="polite">
        <div className="rk-head"><h2>Kết quả</h2></div>
        {error ? <p className="error" role="alert">{error}</p> : !r ? <p className="muted">Đang tính…</p> : (
          <div className="rk-result">
            <div className="rk-big">Mức: <LevelPill level={r.level} levels={policy.levels} /></div>
            <dl className="rk-src">
              <dt>Rule khớp</dt>
              <dd>{r.matched.length === 0 ? <span className="muted">Không rule nào khớp (mức rule: {r.ruleLevel})</span> : (
                <ul className="rk-clean">
                  {r.matched.map((m, i) => (
                    <li key={i}><span className="mono">{m.rule}</span> <LevelPill level={m.level} levels={policy.levels} /> <span className="muted">{m.on}</span></li>
                  ))}
                </ul>
              )}</dd>
              <dt>Agent</dt>
              <dd>{agent ? <>đề xuất <LevelPill level={agent} levels={policy.levels} /></> : <span className="muted">không đề xuất</span>}
                {policy.classification.agent === "off" && <span className="muted"> (đang tắt)</span>}</dd>
            </dl>
            <h3 style={{ fontSize: 14, margin: "6px 0 0" }}>Yêu cầu case cho mức {r.level}</h3>
            {out!.requirements.length === 0 ? <p className="muted" style={{ margin: 0 }}>Không có yêu cầu thêm.</p> : (
              <ul className="rk-clean">{out!.requirements.map((q, i) => <li key={i}>• {q.name}</li>)}</ul>
            )}
          </div>
        )}
      </section>
    </div>
  );
}
