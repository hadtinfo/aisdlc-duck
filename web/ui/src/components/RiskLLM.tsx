import type { RiskLLMData } from "../types";
import { LevelPill } from "./RiskPanel";

/** Khối "Xếp loại LLM" trên review card: mức subagent đưa ra, rule khớp kèm câu trích, rule không khớp kèm lý do. */
export function RiskLLM({ data }: { data: RiskLLMData }) {
  return (
    <section className="box rk-llm">
      <div className="rk-big">Xếp loại LLM <LevelPill level={data.level} /></div>
      {data.summary && <p>{data.summary}</p>}
      {data.override && (
        <p className="rk-note warn">Lệch mức rule ({data.override.from}): {data.override.reason}</p>
      )}
      <dl className="rk-src">
        <dt>Rule khớp</dt>
        <dd>{data.matched.length === 0 ? <span className="muted">Không rule nào khớp</span> : (
          <ul>{data.matched.map((m) => (
            <li key={m.rule}><span className="mono">{m.rule}</span> → {m.level} <span className="muted">{m.on}</span></li>
          ))}</ul>
        )}</dd>
        {data.notMatched.length > 0 && (
          <>
            <dt>Không khớp</dt>
            <dd><ul>{data.notMatched.map((n) => (
              <li key={n.rule}><span className="mono">{n.rule}</span> <span className="muted">{n.reason}</span></li>
            ))}</ul></dd>
          </>
        )}
      </dl>
    </section>
  );
}
