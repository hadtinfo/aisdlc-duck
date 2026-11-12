import type { Tile } from "../reviewModel";
import { useT } from "../i18n";

/** Dải ô tóm tắt đầu trang (vẽ nguyên văn `summaryTiles`). */
export function ReviewSummary({ tiles }: { tiles: Tile[] }) {
  const { t } = useT();
  return (
    <section className="rv-summary" aria-label={t("review.summary")}>
      {tiles.map((t) => (
        <div key={t.label}>
          <span className="eyebrow">{t.label}</span>
          <b className={t.tone ?? ""}>{t.value}</b>
          {t.sub && <span className="muted">{t.sub}</span>}
        </div>
      ))}
    </section>
  );
}
