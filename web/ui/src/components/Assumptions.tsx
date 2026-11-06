import { useId, useState } from "react";
import { loadSeen, saveSeen } from "../reviewModel";
import type { Assumption } from "../types";

/** Giả định của agent; ô "đã xem" chỉ lưu ở localStorage của trình duyệt (khóa theo hash cổng). */
export function Assumptions({ items, section, storageKey, onChange }: {
  items: Assumption[]; section: boolean | undefined; storageKey: string; onChange?: (seen: number[]) => void;
}) {
  // Khóa đổi (cổng/hash khác) thì nơi gọi gắn `key={storageKey}` để mount lại, không có lượt vẽ tick cũ.
  const [seen, setSeen] = useState<number[]>(() => loadSeen(storageKey));
  const id = useId();
  const toggle = (i: number) => {
    const next = seen.includes(i) ? seen.filter((x) => x !== i) : [...seen, i].sort((a, b) => a - b);
    setSeen(next);
    saveSeen(storageKey, next);
    onChange?.(next);
  };
  const done = items.filter((_, i) => seen.includes(i)).length;
  return (
    <section className="panel-sec">
      <h3>Giả định của agent</h3>
      {section === false ? <p className="muted">Tài liệu chưa có mục Giả định</p>
        : items.length === 0 ? <p className="muted">Agent không ghi giả định nào</p> : (
          <>
            <ul className="assume">
              {items.map((a, i) => (
                <li key={i}>
                  <input type="checkbox" id={`${id}-${i}`} checked={seen.includes(i)} onChange={() => toggle(i)} />
                  <label htmlFor={`${id}-${i}`}>{a.text}{a.source && <span className="from">{a.source}</span>}</label>
                </li>
              ))}
            </ul>
            <p className="progress">Đã xem <b>{done}/{items.length}</b> giả định</p>
          </>
        )}
    </section>
  );
}
