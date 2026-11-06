import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Me, logout, me } from "../api";

/** Người đang đăng nhập (null khi đang tải hoặc lỗi; 401 đã chuyển về /login). */
export function useMe(): Me | null {
  const [user, setUser] = useState<Me | null>(null);
  useEffect(() => {
    let alive = true;
    me().then((m) => { if (alive) setUser(m); }).catch(() => { /* 401 đã chuyển trang */ });
    return () => { alive = false; };
  }, []);
  return user;
}

/** Đầu trang: tên người, link Token, (lead) Repo, nút Đăng xuất. */
export function UserMenu() {
  const user = useMe();
  return (
    <div className="me">
      {user && <span title={user.login}>{user.displayName || user.login}</span>}
      <Link to="/me">Token</Link>
      {user?.role === "lead" && <Link to="/repos">Repo</Link>}
      <button type="button" className="ghost" onClick={() => logout().then(() => location.assign("/login"))}>Đăng xuất</button>
    </div>
  );
}
