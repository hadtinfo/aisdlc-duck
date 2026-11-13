import { useCallback, useEffect, useMemo, useState } from "react";
import { BoardData, UnauthorizedError, getBoard } from "../api";
import { Card } from "../components/Card";
import { Inbox } from "../components/Inbox";
import { UserMenu } from "../components/UserMenu";
import { Key, useT } from "../i18n";
import { subscribe } from "../sse";

const COLUMNS: [string, Key][] = [
  ["BRD", "column.brd"], ["DESIGN", "column.design"], ["CASES", "column.cases"], ["CODE", "column.code"],
  ["REVIEW", "column.review"], ["DONE", "column.done"],
];

export function Board() {
  const { t } = useT();
  const [data, setData] = useState<BoardData | null>(null);
  const [error, setError] = useState("");
  const [repo, setRepo] = useState("");
  const [owner, setOwner] = useState("");
  const [hideOld, setHideOld] = useState(true);
  const [now, setNow] = useState(() => new Date());

  const load = useCallback(() => {
    getBoard({ hideDoneOlderThanDays: hideOld ? 7 : 0 })
      .then((d) => { setData(d); setError(""); setNow(new Date()); })
      .catch((e) => { if (!(e instanceof UnauthorizedError)) setError(t("board.loadFailed")); });
  }, [hideOld]);

  useEffect(() => {
    load();
    const stop = subscribe("/api/stream", () => load(), load);
    const tick = setInterval(() => setNow(new Date()), 30_000);
    return () => { stop(); clearInterval(tick); };
  }, [load]);

  // Lọc phía client để danh sách repo/chủ không co lại khi đang lọc.
  const repos = useMemo(() => [...new Set((data?.cards ?? []).map((c) => c.repoId))].sort(), [data]);
  const owners = useMemo(() => [...new Set((data?.cards ?? []).map((c) => c.owner).filter((o): o is string => !!o))].sort(), [data]);
  const cards = (data?.cards ?? []).filter((c) => (!repo || c.repoId === repo) && (!owner || c.owner === owner));

  return (
    <div className="page">
      <header className="topbar">
        <h1>AISDLC</h1>
        <div className="filters">
          <label>Repo
            <select value={repo} onChange={(e) => setRepo(e.target.value)}>
              <option value="">{t("common.all")}</option>
              {repos.map((r) => <option key={r} value={r}>{r}</option>)}
            </select>
          </label>
          <label>{t("board.owner")}
            <select value={owner} onChange={(e) => setOwner(e.target.value)}>
              <option value="">{t("common.all")}</option>
              {owners.map((o) => <option key={o} value={o}>{o}</option>)}
            </select>
          </label>
          <label className="check">
            <input type="checkbox" checked={hideOld} onChange={(e) => setHideOld(e.target.checked)} />
            {t("board.hideOld")}
          </label>
        </div>
        <UserMenu />
      </header>

      {error && <p className="error banner" role="alert">{error}</p>}
      <Inbox items={data?.inbox ?? []} />

      <div className="board">
        {COLUMNS.map(([code, titleKey]) => {
          const title = t(titleKey);
          const col = cards.filter((c) => c.column === code);
          return (
            <section key={code} className="column" aria-label={title}>
              <h2>{title} <span className="count">{col.length}</span></h2>
              <div className="cards">
                {col.map((c) => <Card key={`${c.repoId}/${c.key}`} card={c} now={now} />)}
                {data && col.length === 0 && <p className="col-empty">{t("board.colEmpty")}</p>}
              </div>
            </section>
          );
        })}
      </div>
    </div>
  );
}
