import { describe, expect, it } from "vitest";
import { codeSpans, renderMarkdown } from "./markdown";

describe("renderMarkdown", () => {
  it("heading", () => {
    expect(renderMarkdown("# A")).toBe("<h1>A</h1>");
  });
  it("checkbox chưa tick là disabled", () => {
    const h = renderMarkdown("- [ ] Câu hỏi");
    expect(h).toContain('<input type="checkbox" disabled>');
    expect(h).not.toContain("checked");
    expect(h).toContain("Câu hỏi");
  });
  it("checkbox đã tick", () => {
    expect(renderMarkdown("- [x] Xong")).toContain('<input type="checkbox" disabled checked>');
  });
  it("bảng pipe ra table", () => {
    const h = renderMarkdown("| a | b |\n|---|---|\n| 1 | 2 |");
    expect(h).toContain("<table>");
    expect(h).toContain("<th>a</th>");
    expect(h).toContain("<td>2</td>");
  });
  it("escape HTML trong nguồn", () => {
    const h = renderMarkdown("<script>alert(1)</script>");
    expect(h).toContain("&lt;script&gt;");
    expect(h).not.toContain("<script>");
  });
  it("khối mermaid ra data-mermaid đã escape", () => {
    const h = renderMarkdown("```mermaid\nA-->B\nB-->|<x>|C\n```");
    expect(h).toContain("<div data-mermaid");
    expect(h).toContain("A--&gt;B");
    expect(h).toContain("&lt;x&gt;");
  });
  it("code fence thường giữ nguyên và escape", () => {
    expect(renderMarkdown("```sql\nselect '<a>';\n```")).toContain("<pre><code>select &#39;&lt;a&gt;&#39;;");
  });
  it("đậm, code inline, link", () => {
    const h = renderMarkdown("**đậm** và `x<y` và [BRD](https://a.vn/x?a=1&b=2)");
    expect(h).toContain("<strong>đậm</strong>");
    expect(h).toContain("<code>x&lt;y</code>");
    expect(h).toContain('<a href="https://a.vn/x?a=1&amp;b=2"');
  });
  it("không cho link javascript:", () => {
    const h = renderMarkdown("[x](javascript:alert(1))");
    expect(h).not.toContain("<a ");
  });
  it("không chạy markup trong code inline", () => {
    expect(renderMarkdown("`**a**`")).toContain("<code>**a**</code>");
  });
  it("danh sách và đoạn văn", () => {
    const h = renderMarkdown("- a\n- b\n\nvăn bản\ntiếp");
    expect(h).toContain("<ul><li>a</li><li>b</li></ul>");
    expect(h).toContain("<p>văn bản tiếp</p>");
  });

  describe("đối kháng", () => {
    const noLink = (md: string) => expect(renderMarkdown(md)).not.toContain("<a ");
    it("chặn data:, javascript: lẫn kiểu chữ, //host", () => {
      noLink("[x](data:text/html;base64,PHNjcmlwdD4=)");
      noLink("[x](JaVaScRiPt:alert(1))");
      noLink("[x](//evil.com/a)");
      noLink("[x](/\\evil.com)");
      noLink("[x](/\\/evil.com)");
      noLink("[x](\\\\evil.com)");
      noLink("[x](https://a.vn\\@evil.com)");
      noLink("[x](./\\evil.com)");
    });
    it("cho phép đường dẫn tương đối hợp lệ", () => {
      expect(renderMarkdown("[x](/a/b)")).toContain('href="/a/b"');
      expect(renderMarkdown("[x](./a)")).toContain('href="./a"');
      expect(renderMarkdown("[x](#m)")).toContain('href="#m"');
    });
    it("dấu nháy và entity trong URL không thoát khỏi thuộc tính", () => {
      const h = renderMarkdown('[x](https://a/"onmouseover=alert(1))');
      expect(h).not.toMatch(/href="[^"]*"[^>]*onmouseover/);
      expect(h).toContain("&quot;");
      noLink("[x](&quot;javascript:alert(1))");
      noLink("[x](&#106;avascript:alert(1))");
      expect(renderMarkdown("[x](https://a/&quot;b)")).toContain("&amp;quot;");
    });
    it("** hoặc code span trong URL không chen thẻ vào href", () => {
      const h = renderMarkdown("[x](https://a/**b**) [y](https://a/`c`)");
      expect(h).toContain('href="https://a/**b**"');
      expect(h).not.toContain('href="https://a/<code>');
      expect(h).not.toMatch(/href="[^"]*<[^"]*"/);
    });
    it("HTML trong text link và ô bảng bị escape", () => {
      const l = renderMarkdown("[<img src=x onerror=alert(1)>](https://a.vn)");
      expect(l).not.toContain("<img");
      expect(l).toContain("&lt;img");
      const t = renderMarkdown("| <b>a</b> | b |\n|---|---|\n| <img src=x> | 2 |");
      expect(t).not.toContain("<b>");
      expect(t).not.toContain("<img");
    });
    it("không giả được chỗ giữ \\u0000N\\u0000", () => {
      const h = renderMarkdown("`a` \u00000\u0000 \u00007\u0000");
      expect(h).not.toContain("undefined");
      expect(h).not.toContain("\u0000");
      expect(h).toContain("<code>a</code>");
    });
  });
});

describe("codeSpans", () => {
  it("tách đoạn `code` khỏi chữ thường", () =>
    expect(codeSpans("`POST /a`, body `{\"x\": 1}`.")).toEqual([
      { code: true, text: "POST /a" }, { code: false, text: ", body " }, { code: true, text: "{\"x\": 1}" }, { code: false, text: "." },
    ]));
  it("không có backtick thì một đoạn chữ", () =>
    expect(codeSpans("GET /x <b>")).toEqual([{ code: false, text: "GET /x <b>" }]));
  it("backtick lẻ giữ nguyên", () =>
    expect(codeSpans("a `b")).toEqual([{ code: false, text: "a `b" }]));
  it("chuỗi rỗng", () => expect(codeSpans("")).toEqual([]));
});
