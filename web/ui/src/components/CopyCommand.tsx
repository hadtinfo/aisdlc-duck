import { useRef, useState } from "react";

/** Hiện lệnh terminal kèm nút Chép; không có clipboard API thì chọn sẵn text để người dùng Ctrl+C. */
export function CopyCommand({ command }: { command: string }) {
  const ref = useRef<HTMLElement>(null);
  const [state, setState] = useState<"idle" | "copied" | "selected">("idle");

  async function copy() {
    try {
      await navigator.clipboard.writeText(command);
      setState("copied");
    } catch {
      const sel = window.getSelection();
      if (ref.current && sel) {
        const r = document.createRange();
        r.selectNodeContents(ref.current);
        sel.removeAllRanges();
        sel.addRange(r);
      }
      setState("selected");
    }
    setTimeout(() => setState("idle"), 1800);
  }

  return (
    <span className="cmd">
      <code ref={ref}>{command}</code>
      <button type="button" className="ghost" onClick={copy}>
        {state === "copied" ? "Đã chép" : state === "selected" ? "Nhấn Ctrl+C" : "Chép"}
      </button>
    </span>
  );
}
