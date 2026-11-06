package cli

import (
	"io"

	"aisdlc/internal/gate"
	"aisdlc/internal/model"
	"aisdlc/internal/review"
	"aisdlc/internal/risk"
	"aisdlc/internal/snapshot"
	"aisdlc/internal/spec"
	"aisdlc/internal/webclient"
)

// Phần gửi web của lệnh và hook (WebPublisher của Java nối vào Context): lệnh có thay đổi trạng thái gọi
// AfterCommand (snapshot + event COMMAND, gửi ngay tối đa 2 giây); hook chỉ ghi outbox rồi mở `aisdlc web flush` nền.
// Web tắt (chưa cấu hình) thì mọi thứ ở đây không làm gì. Lỗi chỉ ra stderr, không đổi stdout hay mã thoát.

// WithFlushSpawner thay cách mở tiến trình gửi nền `aisdlc web flush` cho repo root (test ghi lại thay vì mở);
// feature là tính năng của phiên gọi ("" nếu không có).
func (c *Context) WithFlushSpawner(f func(root string, feature model.FeatureKey) error) *Context {
	c.spawnFlush = f
	return c
}

// SpawnFlush mở `aisdlc web flush` tách rời cho repo root, kèm `--feature` là tính năng của phiên (flushFeature, do
// hook đặt) để tiến trình nền làm mới cả tính năng đó, không chỉ .aisdlc/active.
func (c *Context) SpawnFlush(root string) error {
	if c.spawnFlush != nil {
		return c.spawnFlush(root, c.flushFeature)
	}
	return webclient.SpawnFlush(root, string(c.flushFeature))
}

// Publisher là bộ gửi web của repo root (một bản mỗi repo mỗi lệnh; cấu hình web đọc lười một lần). Lỗi ra stderr.
func (c *Context) Publisher(root string) *webclient.Publisher {
	if p, ok := c.publishers[root]; ok {
		return p
	}
	p := c.newPublisher(root, c.err, true)
	if c.publishers == nil {
		c.publishers = map[string]*webclient.Publisher{}
	}
	c.publishers[root] = p
	return p
}

// newPublisher dựng Publisher cho root; warn: in cảnh báo cấu hình (web.json hỏng) ra errw.
func (c *Context) newPublisher(root string, errw io.Writer, warn bool) *webclient.Publisher {
	var (
		done   bool
		client *webclient.Client
	)
	clientOf := func() (*webclient.Client, bool) {
		if !done {
			done = true
			home, _ := c.Home()
			cfg, ok, warns := webclient.LoadConfig(home, c.envValue)
			if warn {
				for _, w := range warns {
					c.Eprintln(w)
				}
			}
			if ok {
				client = &webclient.Client{BaseURL: cfg.URL, Token: cfg.Token, Now: c.now}
			}
		}
		return client, client != nil
	}
	p := webclient.NewPublisher(root, clientOf, errw)
	p.Now = c.now
	p.Spawn = func() error { return c.SpawnFlush(root) }
	p.RepoID = func() string { return snapshot.RepoID(c.std, c.Git(root), root) }
	return p
}

// AfterCommand ghi snapshot mới của tính năng và event COMMAND rồi gửi ngay (WebPublisher.afterCommand).
func (c *Context) AfterCommand(f *Feature, command string) {
	p := c.Publisher(f.Root)
	if !p.Enabled() {
		return
	}
	p.AfterCommandFunc(f.Key.String(), func() ([]byte, error) { return c.featureSnapshot(p, f) },
		func() webclient.Event { return c.commandEvent(f, command) })
}

// AfterCommandKey như AfterCommand cho tính năng vừa tạo: đọc tính năng cũng trong guard, nên lỗi (vd. tính năng
// không có) chỉ thành một dòng "aisdlc web: ..." ở stderr, không đổi mã thoát.
func (c *Context) AfterCommandKey(root string, key model.FeatureKey, command string) {
	p := c.Publisher(root)
	if !p.Enabled() {
		return
	}
	var f *Feature
	build := func() ([]byte, error) {
		var err error
		if f, err = c.FeatureAt(root, key); err != nil {
			return nil, err
		}
		return c.featureSnapshot(p, f)
	}
	// event dựng trong guard, sau snapshot: lỗi dựng (kể cả đọc git) chỉ ra stderr
	p.AfterCommandFunc(key.String(), build, func() webclient.Event {
		return webclient.Event{Type: "COMMAND", At: c.now(), Feature: key.String(), Detail: command,
			Sender: webclient.Sender(c.Git(root))}
	})
}

func (c *Context) commandEvent(f *Feature, command string) webclient.Event {
	return webclient.Event{Type: "COMMAND", At: c.now(), Feature: f.Key.String(), Sender: webclient.Sender(f.Git()),
		Detail: command}
}

// featureSnapshot dựng snapshot của tính năng (WebPublisher.snapshot): thẻ BRD kèm mức rủi ro tạm.
func (c *Context) featureSnapshot(p *webclient.Publisher, f *Feature) ([]byte, error) {
	clientID, err := p.ClientID()
	if err != nil {
		return nil, err
	}
	records, err := f.Steps().All()
	if err != nil {
		return nil, err
	}
	var cases *spec.CasesSpec
	if cs, ok := f.Cases(); ok {
		cases = &cs
	}
	git := f.Git()
	branch, _, _ := git.CurrentBranch(c.std)
	return webclient.BuildSnapshot(webclient.SnapshotInput{
		Paths: f.Paths, Config: f.Config, Meta: f.Meta, Flow: f.Flow(), Gates: f.Gates, Store: f.Store,
		Specs: f.Specs(), Records: records, Cases: cases, Git: git, Adapter: f.Adapter,
		RepoID: f.Web.RepoID(), Branch: branch, ClientID: clientID, CapturedAt: c.now(),
		Sender: webclient.Sender(git), WebMode: f.Mode == gate.ModeWeb,
		CardHook: func(_ gate.Gate, card review.ReviewCard) review.ReviewCard { return withBrdPreview(c, f, card) },
	})
}

// riskClassified đặt event RISK_CLASSIFIED (detail {level, policyVersion, matched:[rule]}) vào outbox; lệnh gửi ở
// AfterCommand. Không bao giờ làm hỏng lệnh.
func riskClassified(ctx *Context, f *Feature, r risk.RiskFile) {
	matched := make([]string, 0, len(r.Matched))
	for _, m := range r.Matched {
		matched = append(matched, m.Rule)
	}
	riskEvent(ctx, f, "RISK_CLASSIFIED", struct {
		Level         string   `json:"level"`
		PolicyVersion int      `json:"policyVersion"`
		Matched       []string `json:"matched"`
	}{r.Level, r.Policy.Version, matched})
}

// riskOverridden đặt event RISK_OVERRIDDEN (detail {from, to, reason}) vào outbox; reason rỗng ghi null.
func riskOverridden(ctx *Context, f *Feature, from, to, reason string) {
	var why *string
	if reason != "" {
		why = &reason
	}
	riskEvent(ctx, f, "RISK_OVERRIDDEN", struct {
		From   string  `json:"from"`
		To     string  `json:"to"`
		Reason *string `json:"reason"`
	}{from, to, why})
}

func riskEvent(ctx *Context, f *Feature, typ string, detail any) {
	defer func() { _ = recover() }() // event chỉ để hiển thị trên web
	p := ctx.Publisher(f.Root)
	if !p.Enabled() {
		return
	}
	d, err := jsonMarshal(detail)
	if err != nil {
		return
	}
	p.Queue(webclient.Event{Type: typ, At: ctx.now(), Feature: f.Key.String(), Sender: webclient.Sender(f.Git()),
		Detail: string(d)})
}
