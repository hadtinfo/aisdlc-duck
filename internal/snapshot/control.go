package snapshot

import (
	"encoding/json"
	"fmt"
	"time"
)

// FeatureControl là trạng thái điều khiển của một tính năng do server web ký (nằm trong SignedEnvelope).
type FeatureControl struct {
	Repo         string        `json:"repo"`
	Feature      string        `json:"feature"`
	IssuedAt     time.Time     `json:"issuedAt"`
	Pause        *Pause        `json:"pause"` // nil: không tạm dừng
	Instructions []Instruction `json:"instructions"`
	Actions      []Action      `json:"actions"`
}

// Pause là yêu cầu tạm dừng.
type Pause struct {
	By string    `json:"by"`
	At time.Time `json:"at"`
}

// Instruction là chỉ dẫn của dev cho agent.
type Instruction struct {
	ID   int64     `json:"id"`
	By   string    `json:"by"`
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

// Action có Kind: ACCEPT_STEP | ALLOW_SCOPE. Step/Path/EndTree bỏ khi rỗng (Java: null).
type Action struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	Step      string     `json:"step,omitempty"`
	Path      string     `json:"path,omitempty"`
	EndTree   string     `json:"endTree,omitempty"`
	By        string     `json:"by"`
	At        time.Time  `json:"at"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// ToJSON serialize; danh sách nil thành [].
func (c FeatureControl) ToJSON() ([]byte, error) {
	if c.Instructions == nil {
		c.Instructions = []Instruction{}
	}
	if c.Actions == nil {
		c.Actions = []Action{}
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("Không serialize được trạng thái điều khiển: %v", err)
	}
	return b, nil
}

// FeatureControlFromJSON parse JSON; lỗi khi hỏng hoặc rỗng.
func FeatureControlFromJSON(b []byte) (FeatureControl, error) {
	var c *FeatureControl
	if err := json.Unmarshal(b, &c); err != nil {
		return FeatureControl{}, fmt.Errorf("Trạng thái điều khiển hỏng: %v", err)
	}
	if c == nil {
		return FeatureControl{}, fmt.Errorf("Trạng thái điều khiển rỗng")
	}
	return *c, nil
}
