import { Component, type ReactNode } from "react";

/** Bắt lỗi khi vẽ con; hiện `fallback` thay vì làm trắng cả trang. Đổi `key` để đặt lại. */
export class ErrorBoundary extends Component<{ fallback: ReactNode; children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidCatch(e: unknown) { console.error("review card render failed", e); }
  render() { return this.state.failed ? this.props.fallback : this.props.children; }
}
