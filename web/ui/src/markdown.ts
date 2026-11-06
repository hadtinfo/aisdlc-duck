/**
 * Bộ render markdown tối giản. Nội dung snapshot là văn bản không tin cậy nên mọi ký tự HTML được
 * escape TRƯỚC khi xử lý cú pháp; chỉ các thẻ do chính file này sinh ra mới xuất hiện trong kết quả.
 */

export function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}

/** Chỉ http(s), mailto, đường dẫn tương đối một dấu '/', '#' hoặc './' (không nhận '//host', '/\\host'; cấm mọi dấu '\\' vì trình duyệt coi nó như '/'). */
const SAFE_URL = /^(?!.*\\)(https?:\/\/|mailto:|\/(?![\/\\])|#|\.\/)/i;

/**
 * Inline. Code span và thẻ <a> được cất vào bảng tạm (giữ chỗ bằng \u0000N\u0000) để các bước
 * đậm/nghiêng không chạm vào href hay nội dung code. Ký tự \u0000 trong nguồn bị bỏ trước để
 * văn bản không giả được chỗ giữ.
 */
function inline(raw: string): string {
  const stash: string[] = [];
  const hold = (html: string) => `\u0000${stash.push(html) - 1}\u0000`;
  let s = escapeHtml(raw.replace(/\u0000/g, ""));
  s = s.replace(/`([^`]+)`/g, (_, c: string) => hold(`<code>${c}</code>`));
  s = s.replace(/\[([^\]]+)\]\(([^)\s\u0000]+)\)/g, (m, text: string, url: string) =>
    SAFE_URL.test(url)
      ? `${hold(`<a href="${url}" target="_blank" rel="noopener noreferrer">`)}${text}${hold("</a>")}`
      : m);
  s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  s = s.replace(/(^|[\s(])\*([^*\s][^*]*)\*(?=$|[\s).,;:])/g, "$1<em>$2</em>");
  return s.replace(/\u0000(\d+)\u0000/g, (_, i: string) => stash[Number(i)]);
}

const splitRow = (line: string) =>
  line.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map((c) => c.trim());
const isTableSep = (line: string) => /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/.test(line) && line.includes("-");

export function renderMarkdown(src: string): string {
  const lines = src.replace(/\r\n?/g, "\n").split("\n");
  const out: string[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const fence = /^\s*```\s*([\w-]*)\s*$/.exec(line);
    if (fence) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !/^\s*```\s*$/.test(lines[i])) body.push(lines[i++]);
      i++; // dòng đóng fence
      const code = escapeHtml(body.join("\n"));
      out.push(fence[1] === "mermaid" ? `<div data-mermaid class="mermaid-src">${code}</div>` : `<pre><code>${code}</code></pre>`);
      continue;
    }
    if (line.trim() === "") { i++; continue; }
    const h = /^(#{1,6})\s+(.*?)\s*#*\s*$/.exec(line);
    if (h) { out.push(`<h${h[1].length}>${inline(h[2])}</h${h[1].length}>`); i++; continue; }
    if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(line)) { out.push("<hr>"); i++; continue; }
    if (line.includes("|") && i + 1 < lines.length && isTableSep(lines[i + 1])) {
      const head = splitRow(line);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && lines[i].includes("|") && lines[i].trim() !== "") rows.push(splitRow(lines[i++]));
      out.push("<table><thead><tr>" + head.map((c) => `<th>${inline(c)}</th>`).join("") + "</tr></thead><tbody>"
        + rows.map((r) => "<tr>" + r.map((c) => `<td>${inline(c)}</td>`).join("") + "</tr>").join("") + "</tbody></table>");
      continue;
    }
    const bullet = /^\s*([-*+]|\d+\.)\s+(.*)$/.exec(line);
    if (bullet) {
      const ordered = /\d/.test(bullet[1]);
      const items: string[] = [];
      while (i < lines.length) {
        const m = /^\s*([-*+]|\d+\.)\s+(.*)$/.exec(lines[i]);
        if (!m || /\d/.test(m[1]) !== ordered) break;
        const task = /^\[([ xX])\]\s+(.*)$/.exec(m[2]);
        items.push(task
          ? `<li class="task"><input type="checkbox" disabled${task[1] === " " ? "" : " checked"}> ${inline(task[2])}</li>`
          : `<li>${inline(m[2])}</li>`);
        i++;
      }
      const tag = ordered ? "ol" : "ul";
      out.push(`<${tag}>${items.join("")}</${tag}>`);
      continue;
    }
    if (/^>\s?/.test(line)) {
      const q: string[] = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) q.push(lines[i++].replace(/^>\s?/, ""));
      out.push(`<blockquote>${inline(q.join(" "))}</blockquote>`);
      continue;
    }
    const para: string[] = [];
    while (i < lines.length && lines[i].trim() !== "" && !/^(#{1,6}\s|\s*```|\s*([-*+]|\d+\.)\s|>)/.test(lines[i])
      && !(lines[i].includes("|") && i + 1 < lines.length && isTableSep(lines[i + 1]))) para.push(lines[i++].trim());
    out.push(`<p>${inline(para.join(" "))}</p>`);
  }
  return out.join("");
}

/** Tách một dòng thành đoạn chữ và đoạn `code` (backtick đơn) để vẽ bằng React, không qua HTML. */
export function codeSpans(s: string): { code: boolean; text: string }[] {
  const out: { code: boolean; text: string }[] = [];
  const re = /`([^`]+)`/g;
  let last = 0;
  for (let m = re.exec(s); m; m = re.exec(s)) {
    if (m.index > last) out.push({ code: false, text: s.slice(last, m.index) });
    out.push({ code: true, text: m[1] });
    last = m.index + m[0].length;
  }
  if (last < s.length) out.push({ code: false, text: s.slice(last) });
  return out;
}
