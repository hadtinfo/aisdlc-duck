import { describe, expect, it } from "vitest";
import { groupByStep, parseDiff } from "./diff";

const TWO_FILES = `diff --git a/a.txt b/a.txt
index 111..222 100644
--- a/a.txt
+++ b/a.txt
@@ -1,2 +1,3 @@ ctx
 keep
-old
+new1
+new2
diff --git a/b.txt b/b.txt
new file mode 100644
index 0000000..333
--- /dev/null
+++ b/b.txt
@@ -0,0 +1,2 @@
+x
+y
`;

describe("parseDiff", () => {
  it("đếm đúng dòng + và - cho 2 file", () => {
    const files = parseDiff(TWO_FILES);
    expect(files.map((f) => f.path)).toEqual(["a.txt", "b.txt"]);
    expect(files.map((f) => [f.added, f.deleted])).toEqual([[2, 1], [2, 0]]);
    expect(files[0].hunks).toHaveLength(1);
    expect(files[0].hunks[0].header).toBe("@@ -1,2 +1,3 @@ ctx");
    expect(files[0].hunks[0].lines.map((l) => l.kind)).toEqual([" ", "-", "+", "+"]);
  });
  it("nhận file mới (--- /dev/null)", () => {
    const [, b] = parseDiff(TWO_FILES);
    expect(b.isNew).toBe(true);
    expect(parseDiff(TWO_FILES)[0].isNew).toBe(false);
  });
  it("diff rỗng", () => {
    expect(parseDiff("")).toEqual([]);
  });
  it("dòng '\\ No newline' không tính", () => {
    const [f] = parseDiff("--- a/z\n+++ b/z\n@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+b\n");
    expect([f.added, f.deleted]).toEqual([1, 1]);
  });
});

describe("groupByStep", () => {
  it("đánh dấu README2.txt ngoài phạm vi và cờ cắt", () => {
    const groups = groupByStep([
      { id: "S1", title: "Một", record: null, diff: null },
      {
        id: "S2", title: "Hai", record: { check: { outOfScope: ["b.txt"] } },
        diff: { text: TWO_FILES, truncated: true },
      },
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0].stepId).toBe("S2");
    expect(groups[0].truncated).toBe(true);
    expect(groups[0].outOfScope.has("b.txt")).toBe(true);
    expect(groups[0].outOfScope.has("a.txt")).toBe(false);
    expect(groups[0].files).toHaveLength(2);
  });
});
