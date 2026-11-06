import { Link, useNavigate } from "react-router-dom";
import { CardData } from "../api";
import { ago, labelText, labelTone } from "../format";
import { featureHref } from "../routes";
import { LevelPill } from "./RiskPanel";

function evalText(e: NonNullable<CardData["eval"]>): string {
  const head = e.passed ? "ĐẠT" : "KHÔNG ĐẠT";
  return e.casesPassed != null && e.casesTotal != null ? `${head} ${e.casesPassed}/${e.casesTotal}` : head;
}

export function Card({ card, now }: { card: CardData; now: Date }) {
  const nav = useNavigate();
  const href = featureHref(card.repoId, card.key);
  const repoName = card.repoId.split("/").slice(-1)[0];
  return (
    <article
      className="card" tabIndex={0} role="link"
      onClick={() => nav(href)}
      onKeyDown={(e) => { if (e.key === "Enter" && e.target === e.currentTarget) nav(href); }}
    >
      <header>
        <span className="key">{card.key}</span>
        {card.flow === "LITE" && <span className="tag">LITE</span>}
        {card.riskLevel && <span title="Mức rủi ro"><LevelPill level={card.riskLevel} prefix="Rủi ro " /></span>}
        {card.session === "ACTIVE" && <span className="dot" title="Phiên Claude Code đang chạy" aria-label="Đang chạy" />}
      </header>
      <h3>{card.title}</h3>
      {card.labels.length > 0 && (
        <div className="labels">
          {card.labels.map((l) => (
            <span key={l} className={`label ${labelTone(l)}`}>{labelText(l)}</span>
          ))}
        </div>
      )}
      {card.eval && (
        <div className={`eval ${card.eval.passed ? "pass" : "fail"}`}>Eval: {evalText(card.eval)}</div>
      )}
      <footer>
        <span title={card.repoId}>{repoName}</span>
        <span>{card.owner ?? "chưa có chủ"}</span>
        <span className="sync">
          {card.clockSkew && <span className="warn" title="Đồng hồ máy gửi lệch so với máy chủ" aria-label="Lệch đồng hồ">⚠ </span>}
          đồng bộ {ago(card.syncedAt, now)}
        </span>
      </footer>
      {card.column === "CODE" && (
        <Link to={featureHref(card.repoId, card.key, true)} className="live-link"
          onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
          Theo dõi
        </Link>
      )}
    </article>
  );
}
