import { Link, useNavigate } from "react-router-dom";
import { CardData } from "../api";
import { ago, labelText, labelTone } from "../format";
import { useT } from "../i18n";
import { featureHref } from "../routes";
import { LevelPill } from "./RiskPanel";

function evalText(e: NonNullable<CardData["eval"]>, t: ReturnType<typeof useT>["t"]): string {
  const head = e.passed ? t("eval.pass") : t("eval.fail");
  return e.casesPassed != null && e.casesTotal != null ? `${head} ${e.casesPassed}/${e.casesTotal}` : head;
}

export function Card({ card, now }: { card: CardData; now: Date }) {
  const { t } = useT();
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
        {card.riskLevel && <span title={t("risk.levelTitle")}><LevelPill level={card.riskLevel} prefix={t("risk.prefix")} /></span>}
        {card.session === "ACTIVE" && <span className="dot" title={t("card.sessionActive")} aria-label={t("card.running")} />}
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
        <div className={`eval ${card.eval.passed ? "pass" : "fail"}`}>Eval: {evalText(card.eval, t)}</div>
      )}
      <footer>
        <span title={card.repoId}>{repoName}</span>
        <span>{card.owner ?? t("card.noOwner")}</span>
        <span className="sync">
          {card.clockSkew && <span className="warn" title={t("card.clockSkew")} aria-label={t("card.clockSkewShort")}>⚠ </span>}
          {t("card.synced", { ago: ago(card.syncedAt, now) })}
        </span>
      </footer>
      {card.column === "CODE" && (
        <Link to={featureHref(card.repoId, card.key, true)} className="live-link"
          onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
          {t("card.watch")}
        </Link>
      )}
    </article>
  );
}
