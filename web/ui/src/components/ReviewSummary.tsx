import type { Tile } from "../reviewModel";

/** Dải ô tóm tắt đầu trang (vẽ nguyên văn `summaryTiles`). */
export function ReviewSummary({ tiles }: { tiles: Tile[] }) {
  return (
    <section className="rv-summary" aria-label="Tóm tắt">
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
