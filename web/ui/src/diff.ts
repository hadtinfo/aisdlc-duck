/** Phân tích unified diff (dạng `git diff`) thành cấu trúc để hiển thị. */
export interface DiffLine { kind: "+" | "-" | " "; text: string }
export interface DiffHunk { header: string; lines: DiffLine[] }
export interface DiffFile { path: string; added: number; deleted: number; isNew: boolean; hunks: DiffHunk[] }

const stripPrefix = (p: string) => p.replace(/^[ab]\//, "");

export function parseDiff(text: string): DiffFile[] {
  const files: DiffFile[] = [];
  const cur: { file?: DiffFile; hunk?: DiffHunk } = {};
  let oldPath = "";
  const start = (path: string): DiffFile => {
    const f: DiffFile = { path, added: 0, deleted: 0, isNew: false, hunks: [] };
    files.push(f);
    cur.file = f;
    cur.hunk = undefined;
    return f;
  };
  for (const line of text.split("\n")) {
    if (line.startsWith("diff --git ")) {
      const m = /^diff --git a\/(.*) b\/(.*)$/.exec(line);
      start(m ? m[2] : line.slice(11));
      oldPath = "";
      continue;
    }
    // Trong hunk, dòng bắt đầu bằng '---'/'+++' vẫn là nội dung; chỉ hiểu là header khi chưa vào hunk.
    if (!cur.hunk && line.startsWith("--- ")) {
      oldPath = line.slice(4);
      const f = !cur.file || cur.file.hunks.length > 0 ? start(stripPrefix(oldPath)) : cur.file;
      if (oldPath === "/dev/null") f.isNew = true;
      continue;
    }
    if (!cur.hunk && line.startsWith("+++ ")) {
      const p = line.slice(4);
      const f = cur.file ?? start(stripPrefix(p));
      f.path = stripPrefix(p === "/dev/null" ? oldPath : p);
      continue;
    }
    if (line.startsWith("new file mode") && cur.file) { cur.file.isNew = true; continue; }
    if (line.startsWith("@@") && cur.file) {
      cur.hunk = { header: line, lines: [] };
      cur.file.hunks.push(cur.hunk);
      continue;
    }
    const { file, hunk } = cur;
    if (!hunk || !file) continue;
    const c = line[0];
    if (c === "+") { file.added++; hunk.lines.push({ kind: "+", text: line.slice(1) }); }
    else if (c === "-") { file.deleted++; hunk.lines.push({ kind: "-", text: line.slice(1) }); }
    else if (c === " ") hunk.lines.push({ kind: " ", text: line.slice(1) });
  }
  return files;
}

export interface StepLike {
  id: string;
  title: string;
  record: { check?: { outOfScope?: string[] | null } | null } | null;
  diff: { text: string; truncated: boolean } | null;
}
export interface StepDiffGroup {
  stepId: string; title: string; files: DiffFile[]; outOfScope: Set<string>; truncated: boolean;
}

/** Nhóm diff theo bước; bước không có diff bị bỏ. */
export function groupByStep(steps: StepLike[]): StepDiffGroup[] {
  return steps.filter((s) => s.diff).map((s) => ({
    stepId: s.id,
    title: s.title,
    files: parseDiff(s.diff!.text),
    outOfScope: new Set(s.record?.check?.outOfScope ?? []),
    truncated: s.diff!.truncated,
  }));
}
