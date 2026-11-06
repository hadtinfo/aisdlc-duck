/** Phân tích phần splat của route `/f/*`: `<repoId nhiều đoạn>/<key>[/live]`. */
export interface FeaturePath {
  repoId: string;
  key: string;
  live: boolean;
}

export function parseFeaturePath(splat: string): FeaturePath {
  const segs = splat.split("/").filter((s) => s !== "").map(decodeURIComponent);
  const live = segs.length > 0 && segs[segs.length - 1] === "live";
  if (live) segs.pop();
  const key = segs.pop() ?? "";
  return { repoId: segs.join("/"), key, live };
}

/** Mã hóa từng đoạn riêng rồi nối bằng '/', không bao giờ sinh %2F. */
export function encodePath(repoId: string, key: string): string {
  return [...repoId.split("/"), key].map(encodeURIComponent).join("/");
}

export function featureHref(repoId: string, key: string, live = false): string {
  return `/f/${encodePath(repoId, key)}${live ? "/live" : ""}`;
}

/** Mục chọn từ `?g=` (cổng hoặc bước); không có hoặc không thuộc `ids` thì null. */
export function selectionFromQuery(search: string, ids: string[]): string | null {
  const g = new URLSearchParams(search).get("g");
  return g && ids.includes(g) ? g : null;
}

/** Trang chính sách rủi ro của repo: `/risk/<repoId nhiều đoạn>`; mã hóa từng đoạn, không sinh %2F. */
export function riskHref(repoId: string): string {
  return `/risk/${repoId.split("/").map(encodeURIComponent).join("/")}`;
}

/** Phần splat của route `/risk/*` → repoId (bỏ `/` thừa, giải mã từng đoạn). */
export function parseRiskPath(splat: string): string {
  return splat.split("/").filter((s) => s !== "").map(decodeURIComponent).join("/");
}

export const RISK_TABS = ["rules", "levels", "try", "history"] as const;
export type RiskTab = (typeof RISK_TABS)[number];

/** Tab theo hash `#rules | #levels | #try | #history`; lạ hoặc trống thì `rules`. */
export function riskTab(hash: string): RiskTab {
  const h = hash.replace(/^#/, "");
  return (RISK_TABS as readonly string[]).includes(h) ? (h as RiskTab) : "rules";
}
