import type { Tile } from "../reviewModel";
import { useT } from "../i18n";

/**
 * Dải ô tóm tắt đầu trang (vẽ nguyên văn `summaryTiles`). `acceptedHash`: cổng đã duyệt, ô cờ có giá trị
 * thêm dòng "đã chấp nhận khi duyệt @hash" (giữ màu đỏ/vàng để vẫn nhắc rủi ro).
 */
export function ReviewSummary({ tiles, acceptedHash }: { tiles: Tile[]; acceptedHash?: string }) {
  const { t } = useT();
  return (
    <section className="rv-summary" aria-label={t("review.summary")}>
      {tiles.map((tile) => (
        <div key={tile.label}>
          <span className="tile-label">{tile.label}</span>
          <b className={tile.tone ?? ""}>{tile.value}</b>
          {tile.sub && <span className="muted">{tile.sub}</span>}
          {acceptedHash && tile.flag && tile.tone && (
            <span className="accepted-note">{t("flags.acceptedShort", { hash: acceptedHash })}</span>
          )}
        </div>
      ))}
    </section>
  );
}
