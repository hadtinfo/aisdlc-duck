import type { KeyboardEvent } from "react";
import { useT } from "../i18n";

export type Tone = "ok" | "warn" | "bad" | "info" | "idle";
/** `status`: chữ trạng thái cho trình đọc màn hình khi `sub` không nói trạng thái (bước: sub là tiêu đề). */
export interface GateItem { id: string; title: string; sub?: string; status?: string; tone: Tone; group: "gate" | "step" }

const NEXT = new Set(["ArrowDown", "j"]);
const PREV = new Set(["ArrowUp", "k"]);

/**
 * Danh sách cổng (theo luồng) và các bước; mỗi mục có chấm trạng thái. Mũi tên lên/xuống hoặc j/k chuyển mục
 * (chọn luôn và giữ focus), Home/End về đầu/cuối.
 */
export function GateList({ items, selected, onSelect }: {
  items: GateItem[]; selected: string; onSelect: (id: string) => void;
}) {
  const { t } = useT();
  const groups: [string, GateItem["group"]][] = [[t("gates.title"), "gate"], [t("steps.title"), "step"]];
  const order = groups.flatMap(([, g]) => items.filter((i) => i.group === g));
  function onKeyDown(e: KeyboardEvent<HTMLElement>) {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    const at = order.findIndex((i) => i.id === selected);
    let to = -1;
    if (NEXT.has(e.key)) to = Math.min(order.length - 1, at + 1);
    else if (PREV.has(e.key)) to = Math.max(0, at < 0 ? 0 : at - 1);
    else if (e.key === "Home") to = 0;
    else if (e.key === "End") to = order.length - 1;
    if (to < 0 || order.length === 0) return;
    e.preventDefault();
    const id = order[to].id;
    onSelect(id);
    e.currentTarget.querySelector<HTMLButtonElement>(`button[data-id="${CSS.escape(id)}"]`)?.focus();
  }
  return (
    <nav className="gatelist" aria-label={t("gates.andSteps")} onKeyDown={onKeyDown}>
      <p className="sr-only">{t("gates.keysHint")}</p>
      {groups.map(([title, g]) => {
        const rows = items.filter((i) => i.group === g);
        if (rows.length === 0) return null;
        return (
          <section key={g}>
            <h2>{title}</h2>
            <ul>
              {rows.map((it) => (
                <li key={it.id}>
                  <button type="button" data-id={it.id} className={`gate-item${it.id === selected ? " active" : ""}`}
                    aria-current={it.id === selected ? "true" : undefined} onClick={() => onSelect(it.id)}>
                    <span className={`sdot ${it.tone}`} aria-hidden="true" />
                    <span className="gate-name">{it.title}</span>
                    {it.sub && <span className="gate-sub">{it.sub}</span>}
                    {it.status && <span className="sr-only">, {it.status}</span>}
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
