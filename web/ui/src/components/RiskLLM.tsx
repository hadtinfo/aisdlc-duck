import type { RiskLLMData } from "../types";
import { LevelPill } from "./RiskPanel";
import { useT } from "../i18n";

/** Khối "Xếp loại LLM" trên review card: mức subagent đưa ra, rule khớp kèm câu trích, rule không khớp kèm lý do. */
export function RiskLLM({ data }: { data: RiskLLMData }) {
  const { t } = useT();
  return (
    <section className="box rk-llm">
      <div className="rk-big">{t("riskllm.title")} <LevelPill level={data.level} /></div>
      {data.summary && <p>{data.summary}</p>}
      {data.override && (
        <p className="rk-note warn">{t("riskllm.override", { from: data.override.from, reason: data.override.reason })}</p>
      )}
      <dl className="rk-src">
        <dt>{t("riskllm.matched")}</dt>
        <dd>{data.matched.length === 0 ? <span className="muted">{t("riskllm.noMatch")}</span> : (
          <ul>{data.matched.map((m) => (
            <li key={m.rule}><span className="mono">{m.rule}</span> → {m.level} <span className="muted">{m.on}</span></li>
          ))}</ul>
        )}</dd>
        {data.notMatched.length > 0 && (
          <>
            <dt>{t("riskllm.notMatched")}</dt>
            <dd><ul>{data.notMatched.map((n) => (
              <li key={n.rule}><span className="mono">{n.rule}</span> <span className="muted">{n.reason}</span></li>
            ))}</ul></dd>
          </>
        )}
      </dl>
    </section>
  );
}
