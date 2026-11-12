import { useEffect, useState } from "react";
import { UnauthorizedError, getResubmission } from "../api";
import { feedbackLabel } from "../control";
import { parseDiff } from "../diff";
import type { GateName, Resubmission as ResubData } from "../types";
import { DiffView } from "./DiffView";
import { useT } from "../i18n";

/** "Thay đổi so với lần nộp trước (lần n)" — chỉ hiện khi cổng đã nộp từ 2 lần (n ≥ 2). `reloadKey` đổi thì tải lại. */
export function Resubmission({ repoId, featureKey, gate, reloadKey }: {
  repoId: string; featureKey: string; gate: GateName; reloadKey: string;
}) {
  const { t } = useT();
  const [data, setData] = useState<ResubData | null>(null);
  const [open, setOpen] = useState<string | null>(null);
  useEffect(() => {
    let alive = true;
    getResubmission(repoId, featureKey, gate)
      .then((d) => { if (alive) setData(d); })
      .catch((e) => { if (alive && !(e instanceof UnauthorizedError)) setData(null); });
    return () => { alive = false; };
  }, [repoId, featureKey, gate, reloadKey]);
  useEffect(() => setOpen(null), [gate, repoId, featureKey]);

  if (!data || data.n < 2) return null;
  const files = data.files ?? [];
  const changed = files.filter((f) => f.status !== "SAME");
  const same = files.filter((f) => f.status === "SAME");
  const feedback = data.feedback ?? [];
  const openFile = changed.find((f) => f.path === open);
  return (
    <section className="rv-panel resub">
      <div className="rv-panel-head"><h3>{t("resub.title", { n: data.n })}</h3></div>
      {data.truncated && <p className="pill warn">{t("diff.truncated")}</p>}
      {feedback.length > 0 && (
        <ul className="resub-fb">
          {feedback.map((f) => (
            <li key={f.id}>
              <div>{f.comment || <span className="muted">{t("resub.noComment")}</span>}</div>
              <div className="muted small">
                {f.by} · <span className={`pill ${f.status === "RELATED_CHANGED" ? "ok" : f.status === "NO_CHANGE_SEEN" ? "warn" : ""}`}>{feedbackLabel(f.status)}</span>
                {f.matched.length > 0 && <> · {f.matched.map((m) => <code key={m}>{m}</code>)}</>}
              </div>
            </li>
          ))}
        </ul>
      )}
      <ul className="resub-files">
        {changed.map((f) => (
          <li key={f.path}>
            <button type="button" className="linklike" disabled={!f.patch} aria-expanded={open === f.path}
              onClick={() => setOpen(open === f.path ? null : f.path)}>
              <span className="mono path">{f.path}</span>{" "}
              {f.status === "ADDED" && <span className="pill info">{t("diff.new")}</span>}
              {f.status === "REMOVED" && <span className="pill bad">{t("resub.removed")}</span>}
              <span className="add">+{f.added}</span> <span className="del">−{f.removed}</span>
            </button>
          </li>
        ))}
      </ul>
      {changed.length === 0 && <p className="muted">{t("resub.noChange")}</p>}
      {openFile?.patch && (
        <DiffView groups={[{ stepId: openFile.path, title: "", files: parseDiff(openFile.patch), outOfScope: new Set(), truncated: false }]} />
      )}
      {same.length > 0 && <p className="muted small">{t("resub.unchanged", { files: same.map((f) => f.path).join(", ") })}</p>}
    </section>
  );
}
