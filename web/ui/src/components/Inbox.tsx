import { Link } from "react-router-dom";
import { InboxItem } from "../api";
import { featureHref } from "../routes";
import { CopyCommand } from "./CopyCommand";

export function Inbox({ items }: { items: InboxItem[] }) {
  return (
    <section className="inbox" aria-label="Việc chờ tôi">
      <h2>
        Việc chờ tôi {items.length > 0 && <span className="count">{items.length}</span>}
      </h2>
      {items.length === 0 ? (
        <p className="muted">Không có việc nào chờ bạn.</p>
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
