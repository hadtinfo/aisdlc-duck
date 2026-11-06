import { expect, test } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ApiTable } from "./ApiTable";

test("ApiTable: when/body vẽ đoạn `code` thành <code>, không còn dấu backtick, HTML bị escape", () => {
  const html = renderToStaticMarkup(
    <ApiTable rows={[{ status: "200", when: "trong hạn mức `limit` còn", body: "`{\"ok\":true}` <b>x</b>" }]} lines={[]} />);
  expect(html).toContain("<code>limit</code>");
  expect(html).toContain("<code>{&quot;ok&quot;:true}</code>");
  expect(html).not.toContain("`");
  expect(html).toContain("&lt;b&gt;x&lt;/b&gt;");
});
