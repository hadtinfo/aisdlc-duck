/** Kiểu của chi tiết tính năng (GET /api/features/...) theo hợp đồng snapshot v1. */
import type { DiffFile } from "./diff";

export type GateName = "BRD" | "DESIGN" | "CASES" | "REVIEW";

export interface Finding { code: string; message: string }
export interface GateData {
  gate: GateName;
  state: string;
  currentHash: string;
  submission: { hash: string; at: string; findings: Finding[] } | null;
  approval: { hash: string; approver: string; at: string; rule?: string; label?: string } | null;
  /** Bản tự duyệt gần nhất bị thay thế khi mức rủi ro tăng (cổng cần dev duyệt lại). */
  superseded?: boolean;
  lastFeedback: { kind: "REJECTED" | "REOPENED"; by: string; at: string; comment: string } | null;
}
export interface StepRecord {
  endTree?: string | null;
  check?: {
    casesPassed?: number; casesNotPassed?: string[]; files?: number; lines?: number;
    overBudget?: boolean; outOfScope?: string[];
    budget?: { files: number; lines: number; valid?: boolean };
  } | null;
  changes?: { path: string; type: string; added: number; deleted: number }[];
  acceptedBy?: string | null;
}
export interface StepData {
  id: string; title: string; scope: string[]; budget: { files: number; lines: number };
  caseIds: string[]; status: string; record: StepRecord | null;
  diff: { text: string; truncated: boolean } | null;
}
export interface CaseData {
  id: string; step: string; kind: "happy" | "error" | "boundary"; title: string;
  request: string; expect: string; raw: string;
}
export interface EvalCheck { id: string; passed?: boolean; level?: string; detail: string }
export interface EvalReport {
  feature?: string; at?: string; mode?: string; passed: boolean; hard: EvalCheck[]; soft: EvalCheck[];
}
export interface ReviewCriterion { id: string; label: "DAT" | "CAN_SUA" | "NGHIEM_TRONG" | string; reason: string }
export interface ReviewIssue { step?: string; file?: string; line?: number; severity?: string; category?: string; message: string }
export interface AgentReviewData { summary?: string; criteria: ReviewCriterion[]; issues?: ReviewIssue[] }

/** Review card theo cổng (hợp đồng JSON spec §4 "reviewCards"); trường null bị bỏ. */
export type FlagLevel = "HIGH" | "WARN" | "INFO" | "NA";
export interface ReviewFlag { code: string; level: FlagLevel; message: string; hint?: string; refs: string[] }
export interface Assumption { text: string; source: string }
export interface SqlColumn { name: string; type?: string; constraints: string[]; references?: string }
/** `addedColumns` chỉ có ở bảng ALTERED (cột thêm mới, chưa gộp vào `columns`). */
export interface TablePart { name: string; status: "NEW" | "EXISTING" | "ALTERED" | "DROPPED"; columns: SqlColumn[]; addedColumns?: SqlColumn[] }
export interface StepPart { id: string; title: string; scope: string[]; budget: { files: number; lines: number };
  matchedCount: number; matchedFiles: string[]; plannedFiles?: string[]; emptyGlobs?: string[] }
export interface ApiRow { status: string; when: string; body: string }
/** Rủi ro trên review card: `risk` ở thẻ DESIGN/CASES (từ risk.yaml), `riskRequirements` ở CASES, `riskPreview` ở BRD. */
export interface RiskMatchData { rule: string; level: string; on: string }
export interface RiskPartData {
  level: string; policyVersion: number; matched: RiskMatchData[];
  override?: { from?: string; to?: string; by?: string; reason?: string; at?: string } | null;
  /** Đánh giá tác động của agent lúc phân loại (`impact` trong design.yaml/cases.yaml). */
  agent?: { level: string; labels: string[]; reason?: string | null } | null;
}
/** Khối "Xếp loại LLM" (subagent aisdlc-risk-classifier) ở thẻ DESIGN/CASES; `on` là "“câu trích” (file)". */
export interface RiskLLMData {
  level: string; summary: string;
  matched: RiskMatchData[]; notMatched: { rule: string; reason: string }[];
  override?: { from: string; reason: string } | null;
}
export interface RiskRequirementData { id: string; name: string; status: "OK" | "MISSING" | "LATER"; detail?: string | null }
export interface ReviewCardData {
  gate: GateName; flags: ReviewFlag[]; assumptions?: Assumption[]; assumptionsSection?: boolean;
  risk?: RiskPartData; riskLLM?: RiskLLMData; riskRequirements?: RiskRequirementData[]; riskPreview?: { level: string; matched: RiskMatchData[] };
  brd?: { rules: number; rulesWithId: number; acceptanceCriteria: number; outOfScope: string[]; answeredQuestions: string[] };
  design?: { steps: StepPart[]; apiLines: string[]; statusCodes: string[]; apiRows: ApiRow[];
    tables: TablePart[]; touchesExisting: { kind: string; table: string; text: string }[] };
  cases?: { rows: { id: string; step: string; kind: string; title: string; given: string; expect: string; rules?: string[] }[];
    coverage: Record<string, Record<string, number>>; ruleMatrix?: Record<string, Record<string, string[]>> };
}

export interface Snapshot {
  repo: { id: string; branch: string };
  sender: string;
  feature: { key: string; title: string; flow: "FULL" | "LITE"; owner: string | null; createdAt: string };
  gates: GateData[];
  steps: StepData[];
  cases: CaseData[];
  openQuestions: { gate: GateName; text: string }[];
  artifacts: Record<string, { content: string; truncated: boolean }>;
  eval: EvalReport | null;
  review: AgentReviewData | null;
  reviewCards?: Partial<Record<GateName, ReviewCardData>>;
  truncated: boolean;
}
export interface FeatureDetail {
  snapshot: Snapshot; column: string; labels: string[]; owner: string | null;
  syncedAt: string; clockSkew: boolean; lastSender: string | null;
  /** 2b-1: chế độ duyệt của repo; vắng (server cũ) coi như local. */
  approvalsMode?: "local" | "web";
  /** Bản duyệt nhập từ local đang chờ chủ tính năng xác nhận (chỉ chế độ web). */
  pendingImports?: PendingImport[];
  /** Đường dẫn artifact của bản nộp mới nhất mỗi cổng (chỉ chế độ web). */
  submittedPaths?: Partial<Record<GateName, string[]>>;
}
export interface PendingImport { gate: GateName; hash: string; originalApprover: string | null; originalAt: string | null }
export interface FeatureEvent {
  id?: number; type: "COMMAND" | "FILE_EDIT" | "SESSION_START" | "SESSION_STOP" | string; at: string;
  step: string | null; path: string | null; outOfScope: boolean; sender: string | null; detail: string | null;
}

export type { DiffFile };

/** 2b-2: điều khiển (GET /control) và diff giữa các lần nộp (GET /resubmission). */
export interface ControlInstruction {
  id: number; by: string; at: string; text: string; source: "web" | "feedback"; deliveredAt: string | null;
}
export interface ControlAction {
  id: number; kind: "ACCEPT_STEP" | "ALLOW_SCOPE"; step: string; path: string | null; by: string; at: string;
  expiresAt: string; status: "PENDING" | "APPLIED" | "STALE" | "EXPIRED"; resolvedAt: string | null; reason: string | null;
}
export interface ControlState {
  paused: boolean; pausedBy: string | null; pausedAt: string | null;
  instructions: ControlInstruction[]; actions: ControlAction[];
}
export interface ResubFile {
  path: string; status: "ADDED" | "REMOVED" | "CHANGED" | "SAME"; added: number; removed: number; patch: string | null;
}
export interface ResubFeedback {
  id: number; kind: string; by: string; at: string; comment: string;
  status: "RELATED_CHANGED" | "NO_CHANGE_SEEN" | "UNMATCHED"; matched: string[];
}
export interface Resubmission {
  n: number; previousAt?: string; latestAt?: string; truncated?: boolean; files?: ResubFile[]; feedback?: ResubFeedback[];
}
