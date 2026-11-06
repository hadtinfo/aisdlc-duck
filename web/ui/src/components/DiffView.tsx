import type { StepDiffGroup } from "../diff";

const BIG = 200;

/** Diff nhóm theo bước. File ngoài phạm vi có viền đỏ; nhãn "đã cắt" khi diff bị cắt. */
export function DiffView({ groups }: { groups: StepDiffGroup[] }) {
  if (groups.length === 0) return <p className="muted">Chưa có diff.</p>;
  return (
    <div className="diffview">
      {groups.map((g) => (
        <section key={g.stepId} className="diff-step">
          <h3>{g.stepId} <span className="muted">{g.title}</span>
            {g.truncated && <span className="pill warn">đã cắt</span>}</h3>
          {g.files.length === 0 && <p className="muted">Diff rỗng.</p>}
          {g.files.map((f) => {
            const oos = g.outOfScope.has(f.path);
            const size = f.hunks.reduce((n, h) => n + h.lines.length, 0);
            return (
              <details key={f.path} className={`diff-file${oos ? " oos" : ""}`} open={size <= BIG}>
                <summary>
                  <span className="mono path">{f.path}</span>
                  {f.isNew && <span className="pill info">mới</span>}
                  {oos && <span className="pill bad">ngoài phạm vi</span>}
                  <span className="add">+{f.added}</span> <span className="del">−{f.deleted}</span>
                </summary>
                <pre className="diff-body">
                  {f.hunks.map((h, i) => (
                    <div key={i}>
                      <div className="hunk">{h.header}</div>
                      {h.lines.map((l, j) => (
                        <div key={j} className={`dl ${l.kind === "+" ? "a" : l.kind === "-" ? "d" : ""}`}>
                          <span className="sign">{l.kind}</span>{l.text}
                        </div>
                      ))}
                    </div>
                  ))}
                </pre>
              </details>
            );
          })}
        </section>
      ))}
    </div>
  );
}
