import { FormEvent, useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ApiError, TokenInfo, UnauthorizedError, createToken, listTokens, revokeToken } from "../api";
import { CopyCommand } from "../components/CopyCommand";
import { UserMenu } from "../components/UserMenu";

const KIND_TEXT: Record<string, string> = {
  login: "đăng nhập", ingest: "ingest", approve: "approve", ci: "CI",
};
const SELF_SERVICE = new Set(["ingest", "approve"]);
const when = (iso: string | null) => (iso ? new Date(iso).toLocaleString("vi-VN") : "—");
const errText = (e: unknown) => (e instanceof ApiError ? e.message : "Không kết nối được máy chủ");

/** Trang /me: token cá nhân — liệt kê, tạo ingest/approve (hiện một lần), thu hồi có xác nhận. */
export function Me() {
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
      .catch((e) => { if (!(e instanceof UnauthorizedError)) setError("Không tải được danh sách token"); });
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
        <Link to="/">← Bảng</Link>
        <h1>Token của tôi</h1>
        <span className="spacer" />
        <UserMenu />
      </header>
      {error && <p className="error banner" role="alert">{error}</p>}

      <section className="box">
        <h2>Tạo token</h2>
        <form className="token-form" onSubmit={create}>
          <label>Loại
            <select value={kind} onChange={(e) => setKind(e.target.value as "ingest" | "approve")}>
              <option value="ingest">ingest — CLI gửi snapshot, nộp cổng</option>
              <option value="approve">approve — duyệt từ terminal</option>
            </select>
          </label>
          <label>Nhãn
            <input type="text" required maxLength={100} placeholder="vd. laptop công ty" value={label}
              onChange={(e) => setLabel(e.target.value)} />
          </label>
          <button type="submit" disabled={busy || !label.trim()}>Tạo token</button>
        </form>
        {created && (
          <div className="new-token" role="status">
            <p><b>Token {created.kind} “{created.label}”</b> — Lưu ngay, token không hiện lại.</p>
            <CopyCommand command={created.token} />
            <button type="button" className="ghost" onClick={() => setCreated(null)}>Đã lưu, ẩn đi</button>
          </div>
        )}
        <ul className="howto muted">
          <li><code>aisdlc web login</code> rồi dán token <b>ingest</b>.</li>
          <li><code>aisdlc web approve-token</code> rồi dán token <b>approve</b>.</li>
        </ul>
      </section>

      <section className="box">
        <h2>Danh sách token</h2>
        {!tokens ? <p className="muted">Đang tải…</p> : tokens.length === 0 ? <p className="muted">Chưa có token.</p> : (
          <table className="grid tokens">
            <thead>
              <tr><th>Loại</th><th>Nhãn</th><th>Tạo lúc</th><th>Dùng lần cuối</th><th>Thu hồi</th><th /></tr>
            </thead>
            <tbody>
              {tokens.map((t) => (
                <tr key={t.id} className={t.revokedAt ? "revoked" : ""}>
                  <td><span className="tag">{KIND_TEXT[t.kind] ?? t.kind}</span></td>
                  <td>{t.label ?? <span className="muted">—</span>}</td>
                  <td>{when(t.createdAt)}</td>
                  <td>{when(t.lastUsedAt)}</td>
                  <td>{t.revokedAt ? when(t.revokedAt) : <span className="muted">còn hiệu lực</span>}</td>
                  <td className="act">
                    {!t.revokedAt && SELF_SERVICE.has(t.kind) && (confirmRevoke === t.id ? (
                      <span className="confirm-inline">
                        Thu hồi token này?
                        <button type="button" className="danger" disabled={busy} onClick={() => revoke(t.id)}>Thu hồi</button>
                        <button type="button" className="ghost" disabled={busy} onClick={() => setConfirmRevoke(null)}>Hủy</button>
                      </span>
                    ) : (
                      <button type="button" className="ghost" onClick={() => setConfirmRevoke(t.id)}>Thu hồi</button>
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
