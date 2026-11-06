import { flagTitle, flagTone, sortFlags } from "../reviewModel";
import type { ReviewFlag } from "../types";

/** Cờ của review card, nặng nhất lên đầu; mỗi cờ có viền trái theo mức. */
export function FlagList({ flags }: { flags: ReviewFlag[] }) {
  const rows = sortFlags(flags);
  return (
    <section className="panel-sec">
      <h3>Cờ đỏ <span className="src">tự động</span></h3>
      {rows.length === 0 ? <p className="muted">Không có cờ</p> : (
        <ul className="flags">
          {rows.map((f, i) => (
            <li key={`${f.code}-${f.level}-${i}`} className={`flag ${flagTone(f.level)}`}>
              <span className="lvl">{flagTitle(f)}</span>
              <p>{f.message}</p>
              {f.hint && <span className="hint">{f.hint}</span>}
              {(f.refs ?? []).length > 0 && (
                <span className="refs">{f.refs.map((r, j) => <code key={j}>{r}</code>)}</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
