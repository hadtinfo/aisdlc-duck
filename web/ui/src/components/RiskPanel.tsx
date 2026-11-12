import { Link } from "react-router-dom";
import { levelTone } from "../risk";
import { riskHref } from "../routes";
import { Key, useT } from "../i18n";
import type { ReviewCardData } from "../types";
import { CopyCommand } from "./CopyCommand";
import "../risk.css";

/** Pill mức rủi ro; màu theo vị trí trong thang của dự án (không biết thang thì theo tên low/medium/high). */
export function LevelPill({ level, levels, prefix }: { level: string; levels?: string[] | null; prefix?: string }) {
  return <span className={`pill risk tone-${levelTone(level, levels)}`}>{prefix}{level}</span>;
}

const STATUS: Record<string, Key> = { OK: "eval.pass", MISSING: "risk.req.missing", LATER: "risk.req.later" };

/**
 * Rủi ro trên trang tính năng: mức, nguồn và danh sách ĐẠT / THIẾU / SAU của review card. Đổi mức chạy ở terminal
 * (`aisdlc risk set` ghi risk.yaml trong repo và cần console thật) nên panel chỉ hiện lệnh.
 * Thẻ BRD do CLI cũ nộp có mức tạm (`riskPreview`); CLI hiện tại không ghi nữa (cổng BRD không xếp loại) nhưng thẻ cũ
 * vẫn lưu trên web, nên panel còn hiện được chúng.
 */
export function RiskPanel({ card, repoId }: { card: ReviewCardData; repoId: string }) {
  const { t } = useT();
  const risk = card.risk;
  const preview = !risk ? card.riskPreview : undefined;
  if (!risk && !preview) return null;
  const reqs = card.riskRequirements ?? [];
  const missing = reqs.filter((r) => r.status === "MISSING").length;
  const level = risk?.level ?? preview!.level;
  const matched = risk?.matched ?? preview!.matched ?? [];
  const o = risk?.override;
  const a = risk?.agent;
  return (
    <section className="rv-panel rk-feature" aria-label={t("risk.title")}>
      <div className="rv-panel-head">
        <h3>{preview ? t("risk.preview") : t("risk.title")}</h3>
        <span className="src">
          {risk ? <>{t("risk.policyShort")} <span className="mono">v{risk.policyVersion}</span> · </> : <>{t("risk.keywordOnly")} · </>}
          <Link to={riskHref(repoId)}>{t("risk.viewPolicy")}</Link>
        </span>
      </div>
      <div className="rk-result">
        <div className="rk-big">{t("risk.title")} <LevelPill level={level} /></div>
        <dl className="rk-src">
          <dt>{t("riskllm.matched")}</dt>
          <dd>{matched.length === 0 ? <span className="muted">{t("riskllm.noMatch")}</span> : matched.map((m, i) => (
            <span key={i}>{i > 0 && " · "}<span className="mono">{m.rule}</span> <span className="muted">{m.on}</span> → {m.level}</span>
          ))}</dd>
          {risk && (
            <>
              <dt>Agent</dt>
              <dd>{a ? <>{a.level}{a.reason && <> — {a.reason}</>}{a.labels?.length > 0 && <>, {t("risk.labels")} {a.labels.join(", ")}</>}</>
                : <span className="muted">{t("risk.noAssessment")}</span>}</dd>
              <dt>{t("risk.devOverride")}</dt>
              <dd>{o ? <>{o.from} → {o.to}{o.by && <> {t("risk.by", { by: o.by })}</>}{o.reason && <span className="muted"> · “{o.reason}”</span>}</> : <span className="muted">{t("common.none")}</span>}</dd>
            </>
          )}
        </dl>
        {reqs.length > 0 && (
          <>
            <div className="rk-row">
              <b style={{ fontSize: 13.5 }}>{t("risk.requirementsOf", { level })}</b>
              {missing > 0 && <span className="pill tone-bad">{t("risk.missingN", { n: missing })}</span>}
            </div>
            <ul className="rk-clean">
              {reqs.map((r) => (
                <li key={`${r.id}:${r.name}`} className="rk-check">
                  <span className={`st ${r.status}`}>{STATUS[r.status] ? t(STATUS[r.status]) : r.status}</span>
                  <span>{r.name}{r.detail && <span className="muted" style={{ fontSize: 12.5 }}> · {r.detail}</span>}</span>
                </li>
              ))}
            </ul>
          </>
        )}
        {risk && (
          <div className="rk-cmd">
            <span>{t("risk.changeHint")}</span>
            <CopyCommand command={`aisdlc risk set ${t("risk.cmdLevel")} --reason "${t("risk.cmdReason")}"`} />
          </div>
        )}
      </div>
    </section>
  );
}
