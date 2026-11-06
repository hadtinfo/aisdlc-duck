import { LiveEvent } from "../live";

const hhmmss = (iso: string) => new Date(iso).toLocaleTimeString("vi-VN", { hour12: false });

/** Dòng thời gian FILE_EDIT, mới nhất trước. */
export function EditTimeline({ events }: { events: LiveEvent[] }) {
  const edits = events.filter((e) => e.type === "FILE_EDIT");
  if (edits.length === 0) return <p className="muted empty">Chưa có lần sửa file nào.</p>;
  return (
    <ol className="timeline">
      {edits.map((e) => (
        <li key={e.id} className={e.outOfScope ? "out" : ""}>
          <time dateTime={e.at}>{hhmmss(e.at)}</time>
          <span className="tl-step">{e.step ?? "—"}</span>
          <code className="tl-path">{e.path}</code>
          {e.outOfScope && <span className="pill tone-bad">ngoài phạm vi</span>}
        </li>
      ))}
    </ol>
  );
}
