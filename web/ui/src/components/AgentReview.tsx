import { Key, useT } from "../i18n";
import type { AgentReviewData } from "../types";

const LABEL: Record<string, [Key, string]> = {
  DAT: ["eval.pass", "ok"], CAN_SUA: ["agentReview.needsFix", "warn"], NGHIEM_TRONG: ["agentReview.severe", "bad"],
};

/** Kết quả agent review: nhãn từng tiêu chí kèm lý do, và danh sách vấn đề. */
export function AgentReview({ review }: { review: AgentReviewData | null }) {
  const { t } = useT();
  if (!review) return <p className="muted">{t("agentReview.none")}</p>;
  return (
    <div className="agent-review">
      {review.summary && <p className="summary">{review.summary}</p>}
      <ul className="criteria">
        {review.criteria.map((c) => {
          const [text, tone] = LABEL[c.label] ? [t(LABEL[c.label][0]), LABEL[c.label][1]] : [c.label, "warn"];
          return (
            <li key={c.id}>
              <span className={`pill ${tone}`}>{text}</span>
              <div><strong>{c.id}</strong><div className="muted">{c.reason}</div></div>
            </li>
          );
        })}
      </ul>
      {review.issues && review.issues.length > 0 && (
        <>
          <h3>{t("agentReview.issues")}</h3>
          <ul className="issues">
            {review.issues.map((x, i) => (
              <li key={i}>
                {x.severity && <span className={`pill ${x.severity === "low" ? "info" : x.severity === "medium" ? "warn" : "bad"}`}>{x.severity}</span>}
                <span>{x.message}
                  {x.file && <span className="muted mono"> {x.step ? `${x.step} · ` : ""}{x.file}{x.line ? `:${x.line}` : ""}</span>}
                </span>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
