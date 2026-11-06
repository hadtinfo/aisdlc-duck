package webclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Event là một bản tin gửi lên web (Event.java); Type ∈ COMMAND, FILE_EDIT, SESSION_START, SESSION_STOP,
// INSTRUCTION_DELIVERED, ACTION_RESULT, SCOPE_DENIED, RISK_CLASSIFIED (Detail {level, policyVersion, matched}),
// RISK_OVERRIDDEN (Detail {from, to, reason}). SessionID: session_id của input hook (rỗng ở lệnh CLI).
// Chuỗi rỗng và At bằng 0 ghi thành null như Java.
type Event struct {
	Type       string
	At         time.Time
	Feature    string
	Step       string
	Path       string
	OutOfScope bool
	Sender     string
	Detail     string
	SessionID  string
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// EventJSON là JSON của event như WebPublisher.eventJson: đủ trường theo thứ tự Java, sessionId chỉ khi có.
func EventJSON(e Event) []byte {
	var at *string
	if !e.At.IsZero() {
		s := javaInstant(e.At)
		at = &s
	}
	v := struct {
		Type       *string `json:"type"`
		At         *string `json:"at"`
		Feature    *string `json:"feature"`
		Step       *string `json:"step"`
		Path       *string `json:"path"`
		OutOfScope bool    `json:"outOfScope"`
		Sender     *string `json:"sender"`
		Detail     *string `json:"detail"`
		SessionID  *string `json:"sessionId,omitempty"`
	}{nullable(e.Type), at, nullable(e.Feature), nullable(e.Step), nullable(e.Path), e.OutOfScope,
		nullable(e.Sender), nullable(e.Detail), nullable(e.SessionID)}
	b, err := marshalNoEscape(v)
	if err != nil {
		panic(err) // chỉ có chuỗi và bool: không thể lỗi
	}
	return b
}

// batchJSON bọc một event đã ghi trong outbox thành lô v2 (event có sessionId tùy chọn). Event hỏng → lỗi.
func batchJSON(repoID, clientID string, eventJSON []byte) ([]byte, error) {
	if !json.Valid(eventJSON) {
		return nil, fmt.Errorf("event không phải JSON hợp lệ")
	}
	var ev json.RawMessage = bytes.TrimSpace(eventJSON)
	return marshalNoEscape(struct {
		Version  int               `json:"version"`
		RepoID   string            `json:"repoId"`
		ClientID string            `json:"clientId"`
		Events   []json.RawMessage `json:"events"`
	}{2, repoID, clientID, []json.RawMessage{ev}})
}

// marshalNoEscape như Jackson: không đổi <, >, & thành <..., không xuống dòng cuối.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// javaInstant định dạng như Instant.toString của Java (ISO_INSTANT): UTC, phần lẻ giây 0/3/6/9 chữ số.
func javaInstant(t time.Time) string {
	t = t.UTC()
	base := t.Format("2006-01-02T15:04:05")
	ns := t.Nanosecond()
	switch {
	case ns == 0:
		return base + "Z"
	case ns%1_000_000 == 0:
		return fmt.Sprintf("%s.%03dZ", base, ns/1_000_000)
	case ns%1_000 == 0:
		return fmt.Sprintf("%s.%06dZ", base, ns/1_000)
	default:
		return fmt.Sprintf("%s.%09dZ", base, ns)
	}
}

// Instant là thời điểm ghi ra JSON như Jackson ghi java.time.Instant; 0 → null.
type Instant time.Time

// MarshalJSON ghi chuỗi ISO_INSTANT hoặc null.
func (i Instant) MarshalJSON() ([]byte, error) {
	t := time.Time(i)
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(javaInstant(t))
}

// Time trả time.Time.
func (i Instant) Time() time.Time { return time.Time(i) }
