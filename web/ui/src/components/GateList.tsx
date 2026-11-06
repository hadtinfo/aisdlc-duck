export type Tone = "ok" | "warn" | "bad" | "info" | "idle";
export interface GateItem { id: string; title: string; sub?: string; tone: Tone; group: "gate" | "step" }

/** Danh sách cổng (theo luồng) và các bước; mỗi mục có chấm trạng thái. */
export function GateList({ items, selected, onSelect }: {
  items: GateItem[]; selected: string; onSelect: (id: string) => void;
}) {
  const groups: [string, GateItem["group"]][] = [["Cổng", "gate"], ["Bước", "step"]];
  return (
    <nav className="gatelist" aria-label="Cổng và bước">
      {groups.map(([title, g]) => {
        const rows = items.filter((i) => i.group === g);
        if (rows.length === 0) return null;
        return (
          <section key={g}>
            <h2>{title}</h2>
            <ul>
              {rows.map((it) => (
                <li key={it.id}>
                  <button type="button" className={`gate-item${it.id === selected ? " active" : ""}`}
                    aria-current={it.id === selected ? "true" : undefined} onClick={() => onSelect(it.id)}>
                    <span className={`sdot ${it.tone}`} aria-hidden="true" />
                    <span className="gate-name">{it.title}</span>
                    {it.sub && <span className="gate-sub">{it.sub}</span>}
                  </button>
                </li>
              ))}
            </ul>
          </section>
        );
      })}
    </nav>
  );
}
