import type { StepData } from "../types";
import { useT } from "../i18n";

/** Bảng bước: phạm vi và ngân sách (dùng ở màn thiết kế). */
export function StepsTable({ steps }: { steps: StepData[] }) {
  const { t } = useT();
  if (steps.length === 0) return null;
  return (
    <table className="grid">
      <thead><tr><th>{t("steps.step")}</th><th>{t("common.title")}</th><th>{t("steps.scope")}</th><th>{t("steps.budget")}</th></tr></thead>
      <tbody>
        {steps.map((s) => (
          <tr key={s.id}>
            <td><strong>{s.id}</strong></td>
            <td>{s.title}</td>
            <td>{s.scope.map((g) => <div key={g}><code>{g}</code></div>)}</td>
            <td>{t("steps.budgetValue", { files: s.budget.files, lines: s.budget.lines })}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
