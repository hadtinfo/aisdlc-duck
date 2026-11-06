import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ApiError, login } from "../api";

export function Login() {
  const nav = useNavigate();
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      if (await login(token.trim())) nav("/", { replace: true });
      else setError("Token không đúng hoặc không phải token đăng nhập");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Không kết nối được máy chủ");
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="login">
      <form className="login-box" onSubmit={submit}>
        <h1>AISDLC</h1>
        <p className="muted">Nhập token đăng nhập (admin cấp) để xem bảng tiến độ và duyệt.</p>
        <input
          type="password" autoFocus autoComplete="current-password" placeholder="Token"
          value={token} onChange={(e) => setToken(e.target.value)} aria-label="Token"
        />
        {error && <p className="error" role="alert">{error}</p>}
        <button type="submit" disabled={busy || !token.trim()}>Đăng nhập</button>
      </form>
    </main>
  );
}
