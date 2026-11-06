package webclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

// Giới hạn mặc định như WebClient.java: kết nối 1 giây; tổng thời gian một request tối đa 2 giây khi người gọi
// không đặt deadline (login đặt 5 giây qua context).
const (
	ConnectTimeout = time.Second
	DefaultTimeout = 2 * time.Second
	maxBody        = 8 << 20
)

// Client gọi web. HTTP nil thì dùng client mặc định (HTTP/1.1, kết nối 1 giây). Now nil thì time.Now (UTC).
type Client struct {
	BaseURL, Token string
	HTTP           *http.Client
	Now            func() time.Time
}

// HTTPError là phản hồi không phải 2xx. Error() tiếng Việt, không bao giờ chứa token.
type HTTPError struct {
	Status int
	Reason string
}

func (e *HTTPError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("Web trả HTTP %d", e.Status)
	}
	return fmt.Sprintf("Web trả HTTP %d: %s", e.Status, e.Reason)
}

// Response là phản hồi thô: status và body (có thể rỗng).
type Response struct {
	Status int
	Body   []byte
}

// OK: status 2xx.
func (r Response) OK() bool { return r.Status/100 == 2 }

// Err là *HTTPError cho status không phải 2xx (Reason lấy từ trường "error" của body JSON), nil nếu 2xx.
func (r Response) Err(token string) error {
	if r.OK() {
		return nil
	}
	var n struct {
		Error string `json:"error"`
	}
	reason := ""
	if json.Unmarshal(r.Body, &n) == nil {
		reason = oneLine(scrub(n.Error, token))
	}
	return &HTTPError{Status: r.Status, Reason: reason}
}

var defaultHTTP = &http.Client{Transport: &http.Transport{
	DialContext:       (&net.Dialer{Timeout: ConnectTimeout}).DialContext,
	ForceAttemptHTTP2: false,
	MaxIdleConns:      4,
	IdleConnTimeout:   10 * time.Second,
}, CheckRedirect: noRedirect}

// noRedirect: không theo redirect (Redirect.NEVER của Java); 3xx trả về như phản hồi thường rồi thành HTTPError.
// Theo redirect sẽ gửi lại Bearer sang địa chỉ khác và đổi POST thành GET.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func (c *Client) httpc() *http.Client {
	if c.HTTP != nil {
		cp := *c.HTTP // luôn không theo redirect, kể cả client do người gọi cấp
		cp.CheckRedirect = noRedirect
		return &cp
	}
	return defaultHTTP
}

// GetJSON: GET có Bearer, giải mã JSON vào out (bỏ qua nếu out nil). Lỗi là *HTTPError cho status không 2xx, còn
// lại là lỗi mạng/URL đã bỏ token.
func (c *Client) GetJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

// PostJSON: POST body (marshal JSON) có Bearer, giải mã phản hồi vào out (bỏ qua nếu nil).
func (c *Client) PostJSON(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("Không serialize được yêu cầu: %v", err)
	}
	return c.doJSON(ctx, http.MethodPost, path, b, out)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body []byte, out any) error {
	r, err := c.Do(ctx, method, path, body)
	if err != nil {
		return err
	}
	if err := r.Err(c.Token); err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(r.Body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(r.Body, out); err != nil {
		return fmt.Errorf("Phản hồi %s không hợp lệ: %v", path, err)
	}
	return nil
}

// Do gửi request và trả phản hồi thô, không coi status khác 2xx là lỗi (để nơi gọi đọc body của 401/409...). body nil
// thì không gửi body; có body thì Content-Type application/json. Không có deadline trong ctx thì giới hạn
// DefaultTimeout. Lỗi chỉ cho mạng/URL/quá giờ và không chứa token.
func (c *Client) Do(ctx context.Context, method, path string, body []byte) (Response, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return Response{}, errors.New("hết thời gian")
	}
	url := strings.TrimRight(c.BaseURL, "/") + path
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return Response{}, fmt.Errorf("URL không hợp lệ: %s", safeURL(url, c.Token))
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpc().Do(req)
	if err != nil {
		return Response{}, netError(c, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Response{}, netError(c, err)
	}
	return Response{Status: resp.StatusCode, Body: b}, nil
}

func netError(c *Client, err error) error {
	var ue *neturl.Error
	if errors.As(err, &ue) {
		err = ue.Err // url.Error chứa cả URL (có thể có userinfo)
	}
	msg := scrub(err.Error(), c.Token)
	if errors.Is(err, context.DeadlineExceeded) {
		msg = "hết thời gian"
	}
	return fmt.Errorf("Không kết nối được web %s: %s", safeURL(c.BaseURL, c.Token), msg)
}

// scrub bỏ token khỏi s (phòng server hoặc thư viện phản chiếu header vào lỗi).
func scrub(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "***")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// SafeURL bỏ userinfo (user:mật khẩu@) và token khỏi URL web trước khi in ra (web status, link duyệt).
func SafeURL(s, token string) string { return safeURL(s, token) }

// safeURL bỏ userinfo và token khỏi URL trước khi đưa vào thông báo lỗi.
func safeURL(s, token string) string {
	if u, err := neturl.Parse(s); err == nil {
		u.User = nil
		s = u.String()
	} else if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	return scrub(s, token)
}
