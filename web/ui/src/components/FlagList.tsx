import { flagTitle, flagTone, sortFlags } from "../reviewModel";
import type { ReviewFlag } from "../types";
import { useT } from "../i18n";

/**
 * Cờ của review card, nặng nhất lên đầu; mỗi cờ có viền trái theo mức. `accepted`: cổng đã duyệt, cờ HIGH/WARN
 * vẫn giữ màu để nhắc nhưng ghi rõ đã được chấp nhận ở hash nào, bởi ai.
 */
export function FlagList({ flags, accepted }: { flags: ReviewFlag[]; accepted?: { hash: string; by: string } | null }) {
  const { t } = useT();
  const rows = sortFlags(flags);
  return (
    <section className="panel-sec">
      <h3>{t("flags.title")} <span className="src">{t("flags.auto")}</span></h3>
      {rows.length === 0 ? <p className="muted">{t("flags.none")}</p> : (<>
        {accepted && rows.some((f) => f.level === "HIGH" || f.level === "WARN") && (
          <p className="flags-accepted">{t("flags.accepted", { hash: accepted.hash, by: accepted.by })}</p>
        )}
        <ul className="flags">
          {rows.map((f, i) => (
            <li key={`${f.code}-${f.level}-${i}`} className={`flag ${flagTone(f.level)}`}>
              <span className="lvl">{flagTitle(f)}
                {accepted && (f.level === "HIGH" || f.level === "WARN") && <span className="lvl-accepted"> · {t("flags.acceptedTag")}</span>}
              </span>
              <p>{f.message}</p>
              {f.hint && <span className="hint">{f.hint}</span>}
              {(f.refs ?? []).length > 0 && (
                <span className="refs">{f.refs.map((r, j) => <code key={j}>{r}</code>)}</span>
              )}
            </li>
          ))}
        </ul>
      </>)}
    </section>
  );
}
