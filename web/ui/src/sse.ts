import { UnauthorizedError, probeAuth } from "./api";

/** Tên sự kiện SSE của backend (card: bảng; snapshot/event: trang tính năng). */
const EVENT_NAMES = ["card", "snapshot", "event"];
const MAX_BACKOFF_MS = 30_000;

/**
 * Đăng ký SSE. Lỗi thì đóng rồi nối lại sau 1 → 2 → 4 … tối đa 30 giây.
 * Mỗi lần nối lại thành công gọi onReset để trang tải lại toàn bộ (có thể đã lỡ sự kiện).
 * EventSource không thấy mã trạng thái, nên mỗi lần lỗi gọi probe: 401 (phiên hết hạn) thì dừng
 * hẳn (probe tự chuyển về /login); lỗi khác thì vẫn nối lại.
 * Trả về hàm hủy đăng ký.
 */
export function subscribe(
  url: string,
  onMessage: (type: string, data: string) => void,
  onReset: () => void,
  probe: () => Promise<void> = probeAuth,
): () => void {
  let es: EventSource | null = null;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let stopped = false;
  let backoff = 1000;
  let failed = false;

  const open = () => {
    if (stopped) return;
    const cur = new EventSource(url);
    es = cur;
    cur.onopen = () => {
      backoff = 1000;
      if (failed) { failed = false; onReset(); }
    };
    for (const name of EVENT_NAMES) {
      cur.addEventListener(name, (e) => onMessage(name, (e as MessageEvent).data));
    }
    cur.onerror = () => {
      cur.close();
      if (stopped) return;
      failed = true;
      const wait = backoff;
      backoff = Math.min(backoff * 2, MAX_BACKOFF_MS);
      timer = setTimeout(open, wait);
      probe().catch((e) => { if (e instanceof UnauthorizedError) stop(); });
    };
  };
  function stop() {
    stopped = true;
    if (timer) clearTimeout(timer);
    es?.close();
  }
  open();
  return stop;
}
