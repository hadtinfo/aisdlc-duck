package webtest

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Trickle gửi request có thân nhỏ giọt (1 byte mỗi 50 ms, không bao giờ xong) và trả status cùng thời gian chờ phản hồi.
// cl rỗng thì gửi chunked.
func Trickle(t *testing.T, base, method, path, cl string, hdr map[string]string) (int, time.Duration) {
	t.Helper()
	host := strings.TrimPrefix(base, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	h := fmt.Sprintf("%s %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\n", method, path, host)
	if cl != "" {
		h += "Content-Length: " + cl + "\r\n"
	} else {
		h += "Transfer-Encoding: chunked\r\n"
	}
	for k, v := range hdr {
		h += k + ": " + v + "\r\n"
	}
	if _, err := conn.Write([]byte(h + "\r\n")); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		chunk := "x"
		if cl == "" {
			chunk = "20\r\n" + strings.Repeat("x", 32) + "\r\n" // chunked: vượt giới hạn nhỏ ngay
		}
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
				if _, err := conn.Write([]byte(chunk)); err != nil {
					return
				}
			}
		}
	}()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("không nhận được phản hồi sau %v: %v", time.Since(start), err)
	}
	resp.Body.Close()
	return resp.StatusCode, time.Since(start)
}
