import { useEffect, useMemo, useRef } from "react";
import { createRoot } from "react-dom/client";
import { renderMarkdown } from "../markdown";
import { Mermaid } from "./Mermaid";

/** Chỉ đầu ra đã escape của renderMarkdown được đưa vào dangerouslySetInnerHTML. */
export function Markdown({ source }: { source: string }) {
  const html = useMemo(() => renderMarkdown(source), [source]);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const roots = [...(ref.current?.querySelectorAll<HTMLElement>("[data-mermaid]") ?? [])].map((node) => {
      const code = node.textContent ?? "";
      node.textContent = "";
      const root = createRoot(node);
      root.render(<Mermaid code={code} />);
      return root;
    });
    return () => { setTimeout(() => roots.forEach((r) => r.unmount())); };
  }, [html]);

  return <div className="md" ref={ref} dangerouslySetInnerHTML={{ __html: html }} />;
}
