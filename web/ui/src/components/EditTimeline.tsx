import { LiveEvent } from "../live";
import { intlTag, useT } from "../i18n";

const hhmmss = (iso: string) => new Date(iso).toLocaleTimeString(intlTag(), { hour12: false });

/** Dòng thời gian FILE_EDIT, mới nhất trước. */
export function EditTimeline({ events }: { events: LiveEvent[] }) {
  const { t } = useT();
  const edits = events.filter((e) => e.type === "FILE_EDIT");
  if (edits.length === 0) return <p className="muted empty">{t("edits.none")}</p>;
  return (
    <ol className="timeline">
      {edits.map((e) => (
        <li key={e.id} className={e.outOfScope ? "out" : ""}>
          <time dateTime={e.at}>{hhmmss(e.at)}</time>
          <span className="tl-step">{e.step ?? "—"}</span>
          <code className="tl-path">{e.path}</code>
          {e.outOfScope && <span className="pill tone-bad">{t("common.outOfScope")}</span>}
        </li>
      ))}
    </ol>
  );
}
