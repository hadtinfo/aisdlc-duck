package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

// DevConsole là nguồn xác nhận của dev. ok=false nghĩa là không có terminal thật (agent, pipe, CI).
type DevConsole interface {
	ReadLine(prompt string) (string, bool)
}

// SecretConsole là console đọc được chữ ẩn (token): không hiện chữ dev gõ. Console không có khả năng này thì lệnh
// cần token coi như không có terminal (exit 3).
type SecretConsole interface {
	ReadSecret(prompt string) (string, bool)
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

// ReadSecret đọc một dòng không hiện chữ (Console.readPassword của Java); chỉ khi có terminal thật.
func (s *systemConsole) ReadSecret(prompt string) (string, bool) {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return "", false
	}
	fmt.Fprint(os.Stdout, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return "", false
	}
	return strings.TrimRight(string(b), "\r\n"), true
}
