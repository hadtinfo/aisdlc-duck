import { type ReactNode, useEffect } from "react";

/** Hộp thoại của trang chính sách rủi ro: bấm nền hoặc Esc để đóng. */
export function RiskDialog({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div className="rk-dialog-bg" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="rk-dialog" role="dialog" aria-modal="true" aria-label={title}>
        <div className="rk-head">
          <h2>{title}</h2>
          <button type="button" className="ghost" onClick={onClose}>Đóng</button>
        </div>
        {children}
      </div>
    </div>
  );
}
