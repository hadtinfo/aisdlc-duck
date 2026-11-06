import type { AgentReviewData } from "../types";

const LABEL: Record<string, [string, string]> = {
  DAT: ["ĐẠT", "ok"], CAN_SUA: ["CẦN SỬA", "warn"], NGHIEM_TRONG: ["NGHIÊM TRỌNG", "bad"],
};

/** Kết quả agent review: nhãn từng tiêu chí kèm lý do, và danh sách vấn đề. */
export function AgentReview({ review }: { review: AgentReviewData | null }) {
  if (!review) return <p className="muted">Chưa có kết quả agent review.</p>;
  return (
    <div className="agent-review">
      {review.summary && <p className="summary">{review.summary}</p>}
      <ul className="criteria">
        {review.criteria.map((c) => {
          const [text, tone] = LABEL[c.label] ?? [c.label, "warn"];
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
          <h3>Vấn đề</h3>
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
