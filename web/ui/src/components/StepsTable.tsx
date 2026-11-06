import type { StepData } from "../types";

/** Bảng bước: phạm vi và ngân sách (dùng ở màn thiết kế). */
export function StepsTable({ steps }: { steps: StepData[] }) {
  if (steps.length === 0) return null;
  return (
    <table className="grid">
      <thead><tr><th>Bước</th><th>Tiêu đề</th><th>Phạm vi</th><th>Ngân sách</th></tr></thead>
      <tbody>
        {steps.map((s) => (
          <tr key={s.id}>
            <td><strong>{s.id}</strong></td>
            <td>{s.title}</td>
            <td>{s.scope.map((g) => <div key={g}><code>{g}</code></div>)}</td>
            <td>{s.budget.files} file, {s.budget.lines} dòng</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
