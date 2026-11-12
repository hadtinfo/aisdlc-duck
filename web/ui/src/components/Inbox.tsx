import { Link } from "react-router-dom";
import { InboxItem } from "../api";
import { useT } from "../i18n";
import { featureHref } from "../routes";
import { CopyCommand } from "./CopyCommand";

export function Inbox({ items }: { items: InboxItem[] }) {
  const { t } = useT();
  return (
    <section className="inbox" aria-label={t("inbox.title")}>
      <h2>
        {t("inbox.title")} {items.length > 0 && <span className="count">{items.length}</span>}
      </h2>
      {items.length === 0 ? (
        <p className="muted">{t("inbox.empty")}</p>
      ) : (
        <ul>
          {items.map((it, i) => (
            <li key={`${it.repoId}/${it.key}/${i}`}>
              <div className="inbox-main">
                <Link to={featureHref(it.repoId, it.key)} className="key">{it.key}</Link>
                <span>{it.text}</span>
              </div>
              {it.command && <CopyCommand command={it.command} />}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
