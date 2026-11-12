import { useEffect, useRef, useState } from "react";
import { useT } from "../i18n";

let seq = 0;

function useDark(): boolean {
  const [dark, setDark] = useState(() => window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false);
  useEffect(() => {
    const mq = window.matchMedia?.("(prefers-color-scheme: dark)");
    if (!mq) return;
    const on = () => setDark(mq.matches);
    mq.addEventListener("change", on);
    return () => mq.removeEventListener("change", on);
  }, []);
  return dark;
}

/** Vẽ một sơ đồ Mermaid. Nguồn không tin cậy nên luôn dùng securityLevel 'strict'. */
export function Mermaid({ code }: { code: string }) {
  const { t } = useT();
  const ref = useRef<HTMLDivElement>(null);
  const dark = useDark();
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    setError("");
    import("mermaid").then(async ({ default: mermaid }) => {
      mermaid.initialize({ startOnLoad: false, securityLevel: "strict", theme: dark ? "dark" : "default" });
      const { svg } = await mermaid.render(`mmd-${++seq}`, code);
      // svg do mermaid sinh ra và đã được nó làm sạch ở chế độ strict.
      if (!cancelled && ref.current) ref.current.innerHTML = svg;
    }).catch((e) => { if (!cancelled) setError(e instanceof Error ? e.message : String(e)); });
    return () => { cancelled = true; };
  }, [code, dark]);

  if (error) {
    return (
      <div className="mermaid-error">
        <p className="warn">{t("mermaid.failed", { error: error.split("\n")[0] })}</p>
        <pre><code>{code}</code></pre>
      </div>
    );
  }
  return <div className="mermaid" ref={ref} role="img" aria-label={t("mermaid.aria")} />;
}
