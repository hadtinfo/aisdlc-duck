package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

// DevConsole là nguồn xác nhận của dev. ok=false nghĩa là không có terminal thật (agent, pipe, CI).
type DevConsole interface {
	ReadLine(prompt string) (string, bool)
}

// SystemConsole đọc từ terminal của tiến trình: chỉ khi cả stdin và stdout là terminal (như System.console() của
// Java 22+). Không có cờ hay biến môi trường nào bỏ qua.
func SystemConsole() DevConsole { return &systemConsole{} }

type systemConsole struct {
	once   sync.Once
	reader *bufio.Reader
}

func (s *systemConsole) ReadLine(prompt string) (string, bool) {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return "", false
	}
	s.once.Do(func() { s.reader = bufio.NewReader(os.Stdin) })
	fmt.Fprint(os.Stdout, prompt)
	line, err := s.reader.ReadString('\n')
	if err != nil && line == "" {
		return "", false // EOF (Ctrl-D): như Console.readLine trả null
	}
	return strings.TrimRight(line, "\r\n"), true
}
