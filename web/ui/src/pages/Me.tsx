import { FormEvent, useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ApiError, TokenInfo, UnauthorizedError, createToken, listTokens, revokeToken } from "../api";
import { CopyCommand } from "../components/CopyCommand";
import { UserMenu } from "../components/UserMenu";
import { Key, intlTag, t as tNow, useT } from "../i18n";

const KIND_TEXT: Record<string, Key> = {
  login: "token.kind.login", ingest: "token.kind.ingest", approve: "token.kind.approve", ci: "token.kind.ci",
};
const SELF_SERVICE = new Set(["ingest", "approve"]);
const when = (iso: string | null) => (iso ? new Date(iso).toLocaleString(intlTag()) : "—");
const errText = (e: unknown) => (e instanceof ApiError ? e.message : tNow("common.noServer"));

/** Trang /me: token cá nhân — liệt kê, tạo ingest/approve (hiện một lần), thu hồi có xác nhận. */
export function Me() {
  const { t } = useT();
  const tr = t;
  const [tokens, setTokens] = useState<TokenInfo[] | null>(null);
  const [error, setError] = useState("");
  const [kind, setKind] = useState<"ingest" | "approve">("ingest");
  const [label, setLabel] = useState("");
  const [busy, setBusy] = useState(false);
  const [created, setCreated] = useState<{ kind: string; label: string; token: string } | null>(null);
  const [confirmRevoke, setConfirmRevoke] = useState<number | null>(null);

  const load = useCallback(() => {
    listTokens()
      .then((t) => { setTokens(t); setError(""); })
      .catch((e) => { if (!(e instanceof UnauthorizedError)) setError(t("token.loadFailed")); });
  }, []);
  useEffect(load, [load]);

  async function create(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const r = await createToken(kind, label.trim());
      setCreated({ kind, label: label.trim(), token: r.token });
      setLabel("");
      load();
    } catch (err) {
      setError(errText(err));
    } finally {
      setBusy(false);
    }
  }

  async function revoke(id: number) {
    setBusy(true);
    setError("");
    try {
      await revokeToken(id);
      setConfirmRevoke(null);
      load();
    } catch (err) {
      setError(errText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="page narrow">
      <header className="topbar">
        <Link to="/">← {t("nav.board")}</Link>
        <h1>{t("token.title")}</h1>
        <span className="spacer" />
        <UserMenu />
      </header>
      {error && <p className="error banner" role="alert">{error}</p>}

      <section className="box">
        <h2>{t("token.create")}</h2>
        <form className="token-form" onSubmit={create}>
          <label>{t("token.kind")}
            <select value={kind} onChange={(e) => setKind(e.target.value as "ingest" | "approve")}>
              <option value="ingest">{t("token.opt.ingest")}</option>
              <option value="approve">{t("token.opt.approve")}</option>
            </select>
          </label>
          <label>{t("token.label")}
            <input type="text" required maxLength={100} placeholder={t("token.labelPlaceholder")} value={label}
              onChange={(e) => setLabel(e.target.value)} />
          </label>
          <button type="submit" disabled={busy || !label.trim()}>{t("token.create")}</button>
        </form>
        {created && (
          <div className="new-token" role="status">
            <p><b>Token {created.kind} “{created.label}”</b> — {t("token.saveNow")}</p>
            <CopyCommand command={created.token} />
            <button type="button" className="ghost" onClick={() => setCreated(null)}>{t("token.savedHide")}</button>
          </div>
        )}
        <ul className="howto muted">
          <li><code>aisdlc web login</code> {t("token.howtoPaste")} <b>ingest</b>.</li>
          <li><code>aisdlc web approve-token</code> {t("token.howtoPaste")} <b>approve</b>.</li>
        </ul>
      </section>

      <section className="box">
        <h2>{t("token.list")}</h2>
        {!tokens ? <p className="muted">{t("common.loading")}</p> : tokens.length === 0 ? <p className="muted">{t("token.none")}</p> : (
          <table className="grid tokens">
            <thead>
              <tr><th>{t("token.kind")}</th><th>{t("token.label")}</th><th>{t("token.createdAt")}</th><th>{t("token.lastUsed")}</th><th>{t("token.revoked")}</th><th /></tr>
            </thead>
            <tbody>
              {tokens.map((t) => (
                <tr key={t.id} className={t.revokedAt ? "revoked" : ""}>
                  <td><span className="tag">{KIND_TEXT[t.kind] ? tr(KIND_TEXT[t.kind]) : t.kind}</span></td>
                  <td>{t.label ?? <span className="muted">—</span>}</td>
                  <td>{when(t.createdAt)}</td>
                  <td>{when(t.lastUsedAt)}</td>
                  <td>{t.revokedAt ? when(t.revokedAt) : <span className="muted">{tr("token.active")}</span>}</td>
                  <td className="act">
                    {!t.revokedAt && SELF_SERVICE.has(t.kind) && (confirmRevoke === t.id ? (
                      <span className="confirm-inline">
                        {tr("token.revokeConfirm")}
                        <button type="button" className="danger" disabled={busy} onClick={() => revoke(t.id)}>{tr("token.revoke")}</button>
                        <button type="button" className="ghost" disabled={busy} onClick={() => setConfirmRevoke(null)}>{tr("common.cancel")}</button>
                      </span>
                    ) : (
                      <button type="button" className="ghost" onClick={() => setConfirmRevoke(t.id)}>{tr("token.revoke")}</button>
                    ))}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
