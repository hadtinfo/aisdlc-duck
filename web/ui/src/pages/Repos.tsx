import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ApiError, RepoMode, UnauthorizedError, listRepos, setRepoMode } from "../api";
import { UserMenu, useMe } from "../components/UserMenu";
import { riskHref } from "../routes";

const MODE_TEXT: Record<string, string> = { local: "local — duyệt ở terminal", web: "web — duyệt trên web" };
const when = (iso: string | null) => (iso ? new Date(iso).toLocaleString("vi-VN") : "—");

/** Trang /repos (lead): chế độ duyệt từng repo, đổi chế độ có xác nhận. Server kiểm quyền lead. */
export function Repos() {
  const user = useMe();
  const [repos, setRepos] = useState<RepoMode[] | null>(null);
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    listRepos()
      .then((r) => { setRepos(r); setError(""); })
      .catch((e) => { if (!(e instanceof UnauthorizedError)) setError("Không tải được danh sách repo"); });
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
      setError(e instanceof ApiError ? e.message : "Không kết nối được máy chủ");
    } finally {
      setBusy(false);
    }
  }

  const lead = user?.role === "lead";
  return (
    <div className="page narrow">
      <header className="topbar">
        <Link to="/">← Bảng</Link>
        <h1>Chế độ duyệt theo repo</h1>
        <span className="spacer" />
        <UserMenu />
      </header>
      {error && <p className="error banner" role="alert">{error}</p>}
      {user && !lead && <p className="muted">Chỉ lead được đổi chế độ duyệt.</p>}
      <section className="box">
        {!repos ? <p className="muted">Đang tải…</p> : repos.length === 0 ? <p className="muted">Chưa có repo nào.</p> : (
          <table className="grid">
            <thead><tr><th>Repo</th><th>Chế độ</th><th>Đổi bởi</th><th>Lúc</th><th /><th /></tr></thead>
            <tbody>
              {repos.map((r) => {
                const to = r.approvals === "web" ? "local" : "web";
                return (
                  <tr key={r.id}>
                    <td><code>{r.id}</code></td>
                    <td><span className={`tag mode-${r.approvals}`}>{MODE_TEXT[r.approvals] ?? r.approvals}</span></td>
                    <td>{r.setBy ?? "—"}</td>
                    <td>{when(r.setAt)}</td>
                    <td><Link to={riskHref(r.id)}>Chính sách rủi ro</Link></td>
                    <td className="act">
                      {lead && (confirm === r.id ? (
                        <span className="confirm-inline">
                          Chuyển sang {to}?
                          <button type="button" className="danger" disabled={busy} onClick={() => change(r)}>Xác nhận</button>
                          <button type="button" className="ghost" disabled={busy} onClick={() => setConfirm(null)}>Hủy</button>
                        </span>
                      ) : (
                        <button type="button" className="ghost" onClick={() => setConfirm(r.id)}>Chuyển sang {to}</button>
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
