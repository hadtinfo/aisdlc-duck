import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ApiError, RepoMode, UnauthorizedError, listRepos, setRepoMode } from "../api";
import { UserMenu, useMe } from "../components/UserMenu";
import { Key, intlTag, t as tNow, useT } from "../i18n";
import { riskHref } from "../routes";

const MODE_TEXT: Record<string, Key> = { local: "repos.mode.local", web: "repos.mode.web" };
const when = (iso: string | null) => (iso ? new Date(iso).toLocaleString(intlTag()) : "—");

/** Trang /repos (lead): chế độ duyệt từng repo, đổi chế độ có xác nhận. Server kiểm quyền lead. */
export function Repos() {
  const { t } = useT();
  const user = useMe();
  const [repos, setRepos] = useState<RepoMode[] | null>(null);
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    listRepos()
      .then((r) => { setRepos(r); setError(""); })
      .catch((e) => { if (!(e instanceof UnauthorizedError)) setError(tNow("repos.loadFailed")); });
  }, []);
  useEffect(load, [load]);

  async function change(r: RepoMode) {
    setBusy(true);
    setError("");
    try {
      await setRepoMode(r.id, r.approvals === "web" ? "local" : "web");
      setConfirm(null);
      load();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : tNow("common.noServer"));
    } finally {
      setBusy(false);
    }
  }

  const lead = user?.role === "lead";
  return (
    <div className="page narrow">
      <header className="topbar">
        <Link to="/">← {t("nav.board")}</Link>
        <h1>{t("repos.title")}</h1>
        <span className="spacer" />
        <UserMenu />
      </header>
      {error && <p className="error banner" role="alert">{error}</p>}
      {user && !lead && <p className="muted">{t("repos.leadOnly")}</p>}
      <section className="box">
        {!repos ? <p className="muted">{t("common.loading")}</p> : repos.length === 0 ? <p className="muted">{t("repos.none")}</p> : (
          <table className="grid">
            <thead><tr><th>Repo</th><th>{t("repos.mode")}</th><th>{t("repos.setBy")}</th><th>{t("repos.setAt")}</th><th /><th /></tr></thead>
            <tbody>
              {repos.map((r) => {
                const to = r.approvals === "web" ? "local" : "web";
                return (
                  <tr key={r.id}>
                    <td><code>{r.id}</code></td>
                    <td><span className={`tag mode-${r.approvals}`}>{MODE_TEXT[r.approvals] ? t(MODE_TEXT[r.approvals]) : r.approvals}</span></td>
                    <td>{r.setBy ?? "—"}</td>
                    <td>{when(r.setAt)}</td>
                    <td><Link to={riskHref(r.id)}>{t("risk.policy")}</Link></td>
                    <td className="act">
                      {lead && (confirm === r.id ? (
                        <span className="confirm-inline">
                          {t("repos.switchConfirm", { to })}
                          <button type="button" className="danger" disabled={busy} onClick={() => change(r)}>{t("common.confirm")}</button>
                          <button type="button" className="ghost" disabled={busy} onClick={() => setConfirm(null)}>{t("common.cancel")}</button>
                        </span>
                      ) : (
                        <button type="button" className="ghost" onClick={() => setConfirm(r.id)}>{t("repos.switch", { to })}</button>
                      ))}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
