import { describe, expect, it } from "vitest";
import { featureHref, parseFeaturePath, parseRiskPath, riskHref, riskTab, selectionFromQuery } from "./routes";

describe("parseFeaturePath", () => {
  it("repoId nhiều đoạn", () =>
    expect(parseFeaturePath("gitlab.corp/pay/svc/PAY-1")).toEqual({ repoId: "gitlab.corp/pay/svc", key: "PAY-1", live: false }));
  it("trang live", () =>
    expect(parseFeaturePath("gitlab.corp/pay/svc/PAY-1/live")).toEqual({ repoId: "gitlab.corp/pay/svc", key: "PAY-1", live: true }));
  it("repoId một đoạn", () =>
    expect(parseFeaturePath("local/PAY-1")).toEqual({ repoId: "local", key: "PAY-1", live: false }));
  it("bỏ qua dấu / thừa và giải mã", () =>
    expect(parseFeaturePath("/a%20b/K-1/")).toEqual({ repoId: "a b", key: "K-1", live: false }));
  it("thiếu đoạn thì repoId rỗng", () =>
    expect(parseFeaturePath("K-1")).toEqual({ repoId: "", key: "K-1", live: false }));
});

describe("featureHref", () => {
  it("mã hóa từng đoạn, không sinh %2F", () => {
    expect(featureHref("gitlab.corp/pay/svc", "PAY-1")).toBe("/f/gitlab.corp/pay/svc/PAY-1");
    expect(featureHref("a b/c", "K 1", true)).toBe("/f/a%20b/c/K%201/live");
  });
});

describe("selectionFromQuery", () => {
  const ids = ["BRD", "DESIGN", "CASES", "REVIEW", "S1"];
  it("?g= có trong danh sách", () => expect(selectionFromQuery("?g=DESIGN", ids)).toBe("DESIGN"));
  it("bước cũng chọn được", () => expect(selectionFromQuery("?x=1&g=S1", ids)).toBe("S1"));
  it("mục lạ thì null", () => expect(selectionFromQuery("?g=XYZ", ids)).toBeNull());
  it("không có ?g= thì null", () => expect(selectionFromQuery("", ids)).toBeNull());
  it("?g= rỗng thì null", () => expect(selectionFromQuery("?g=", ids)).toBeNull());
});

describe("riskHref / parseRiskPath", () => {
  it("parseRiskPath_multiSegmentRepo", () => {
    expect(parseRiskPath("gitlab.corp/pay/svc")).toBe("gitlab.corp/pay/svc");
    expect(parseRiskPath("/a%20b/c/")).toBe("a b/c");
    expect(parseRiskPath("local")).toBe("local");
  });
  it("riskHref_neverEncodesSlash", () => {
    expect(riskHref("gitlab.corp/pay/svc")).toBe("/risk/gitlab.corp/pay/svc");
    expect(riskHref("a b/c#d")).toBe("/risk/a%20b/c%23d");
    expect(riskHref("a b/c#d")).not.toContain("%2F");
    expect(parseRiskPath(riskHref("a b/c#d").slice("/risk/".length))).toBe("a b/c#d");
  });
  it("tab theo hash, mặc định rules", () => {
    expect(riskTab("#levels")).toBe("levels");
    expect(riskTab("#history")).toBe("history");
    expect(riskTab("#try")).toBe("rules"); // tab Thử phân loại đã bỏ
    expect(riskTab("")).toBe("rules");
    expect(riskTab("#feature")).toBe("rules");
  });
});
