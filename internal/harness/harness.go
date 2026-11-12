package harness

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"aisdlc/internal/harness/stub"
	"aisdlc/internal/i18n"
	"aisdlc/internal/lang"
	"aisdlc/internal/model"
	"aisdlc/internal/run"
	"aisdlc/internal/spec"
)

const (
	exitHarness    = 2
	cleanupTimeout = 30 * time.Second
	healthInterval = 200 * time.Millisecond
	serviceLogTail = 50
	harnessRowID   = "HARNESS"
)

func buildFailed() string { return i18n.T("build lỗi") }

func noDatabase() string { return i18n.T("chưa cấu hình database (service.env.databaseUrl)") }

// Infra khởi động hạ tầng của một lần chạy; test tiêm lỗi qua đây. stop phải dọn được cả khi ctx của lần chạy đã hủy.
type Infra interface {
	Postgres(ctx context.Context, image, runID string) (url string, stop func(context.Context) error, err error)
	Kafka(ctx context.Context, image string, topics []string, runID string) (brokers string, stop func(context.Context) error, err error)
}

// ContainerInfra là Infra mặc định: container testcontainers-go gắn nhãn aisdlc.run=<runID>.
type ContainerInfra struct{}

// Postgres khởi động Postgres và trả URL.
func (ContainerInfra) Postgres(ctx context.Context, image, runID string) (string, func(context.Context) error, error) {
	pg, stop, err := StartPostgres(ctx, image, runID)
	if err != nil {
		return "", nil, err
	}
	return pg.URL, stop, nil
}

// Kafka khởi động Kafka và trả địa chỉ broker.
func (ContainerInfra) Kafka(ctx context.Context, image string, topics []string, runID string) (string, func(context.Context) error, error) {
	return StartKafka(ctx, image, topics, runID)
}

// Harness chạy bộ case black-box trên dịch vụ đích.
type Harness struct {
	Root    string
	Config  model.RepoConfig
	Adapter lang.Adapter
	Runtime lang.Runtime
	Infra   Infra // nil → testcontainers
	Now     func() time.Time
}

// Run chạy bộ case của một tính năng theo spec §6. error chỉ khi không ghi được out; lỗi hạ tầng hay dịch vụ được
// ghi thành case ERROR.
func (h *Harness) Run(ctx context.Context, o Options) (RunResult, error) {
	s, err := h.open(ctx, o.Out)
	if err != nil {
		return RunResult{}, err
	}
	defer s.close()
	r, err := s.run(o, nil)
	return r.res, err
}

// Verify: build, unit test, chạy mọi case (mode cases) rồi tính coverage. Build hỏng thì không chạy unit test và case.
func (h *Harness) Verify(ctx context.Context, key model.FeatureKey, out string) (VerifyResult, error) {
	s, err := h.open(ctx, out)
	if err != nil {
		return VerifyResult{}, err
	}
	defer s.close()
	var vr VerifyResult
	var errs []string

	s.logf("build %s", h.Config.Service.Main)
	b, berr := h.Runtime.Build(s.ctx, h.Root, h.Config.Service, s.out)
	vr.Build = b
	built := berr == nil && b.OK
	if built {
		s.logf("unit test %v", h.Config.UnitTest.Packages)
		u, uerr := h.Runtime.UnitTests(s.ctx, h.Root, h.Config.UnitTest, s.out)
		vr.Unit = u
		s.raw(u.Output)
		if uerr != nil && s.ctx.Err() == nil {
			errs = append(errs, i18n.Tf("không chạy được unit test: %s", uerr.Error()))
			s.logf("%s", errs[len(errs)-1])
		}
	}
	r, err := s.run(Options{Feature: key, Mode: ModeCases, Out: out}, &prebuilt{res: b, err: berr})
	if err != nil {
		return VerifyResult{}, err
	}
	vr.Run = r.res
	// build hỏng (không phải lỗi hạ tầng) đã thể hiện qua Build.OK=false, không tính là lỗi harness
	if r.harnessErr != "" && (berr != nil || b.OK) {
		errs = append(errs, r.harnessErr)
	}
	vr.HarnessErr = strings.Join(errs, "; ")
	vr.TimedOut = r.res.ExitCode == ExitTimeout

	switch {
	case vr.TimedOut:
		vr.Coverage = lang.Coverage{NA: s.stopMessage()}
	case !built:
		vr.Coverage = lang.Coverage{NA: buildFailed()}
	case r.started && !r.graceful:
		// spec §3: không tắt êm thì coverage N/A, không trả số chỉ từ unit test
		vr.Coverage = lang.Coverage{NA: i18n.Tf("%s: không có coverage lúc chạy case", r.killed)}
	default:
		vr.Coverage = s.coverage(vr.Unit.Profile, s.coverDir())
	}
	if vr.Coverage.NA != "" {
		s.logf("coverage N/A: %s", vr.Coverage.NA)
	}
	return vr, nil
}

// session là một lần chạy: thư mục out, harness.log và ctx có hạn timeoutMinutes.
type session struct {
	h      *Harness
	ctx    context.Context
	cancel context.CancelFunc
	out    string
	log    *os.File
}

// open xóa rồi tạo lại out, mở out/harness.log và đặt hạn timeoutMinutes.
func (h *Harness) open(ctx context.Context, out string) (*session, error) {
	abs, err := filepath.Abs(out)
	if err != nil {
		return nil, err
	}
	if err := os.RemoveAll(abs); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	logf, err := os.Create(filepath.Join(abs, "harness.log"))
	if err != nil {
		return nil, err
	}
	s := &session{h: h, out: abs, log: logf}
	if m := h.Config.TimeoutMinutes; m > 0 {
		s.ctx, s.cancel = context.WithTimeout(ctx, time.Duration(math.Round(m*60_000))*time.Millisecond)
	} else {
		s.ctx, s.cancel = context.WithCancel(ctx)
	}
	return s, nil
}

// stopMessage: thông điệp khi s.ctx đã dừng — hủy (Ctrl-C, ctx của lệnh bị hủy) khác quá timeoutMinutes.
func (s *session) stopMessage() string {
	if errors.Is(s.ctx.Err(), context.Canceled) {
		return Canceled()
	}
	return TimeoutMessage(s.h.Config.TimeoutMinutes)
}

func (s *session) close() {
	s.cancel()
	_ = s.log.Close()
}

func (s *session) now() time.Time {
	if s.h.Now != nil {
		return s.h.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *session) logf(format string, args ...any) {
	fmt.Fprintf(s.log, "[%s] %s\n", s.now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

func (s *session) raw(text string) {
	if text == "" {
		return
	}
	_, _ = io.WriteString(s.log, text)
	if !strings.HasSuffix(text, "\n") {
		_, _ = io.WriteString(s.log, "\n")
	}
}

func (s *session) coverDir() string { return filepath.Join(s.out, "cover") }

func (s *session) coverage(unitProfile, coverDir string) lang.Coverage {
	c, err := s.h.Runtime.Coverage(s.ctx, s.h.Root, s.out, unitProfile, coverDir)
	if err != nil {
		return lang.Coverage{NA: i18n.Tf("không tính được coverage: %s", err.Error())}
	}
	return c
}

func (s *session) infra() Infra {
	if s.h.Infra != nil {
		return s.h.Infra
	}
	return ContainerInfra{}
}

// prebuilt là bản build Verify đã làm, để run không build lại.
type prebuilt struct {
	res lang.BuildResult
	err error
}

// runOutcome là kết quả nội bộ của run.
type runOutcome struct {
	res        RunResult
	harnessErr string
	started    bool   // dịch vụ đã được khởi động
	graceful   bool   // dịch vụ tự thoát khi nhận SIGTERM
	killed     string // thông điệp khi dịch vụ không tắt êm
}

// run thực hiện các bước 2–10 của spec §6 trong s.out rồi ghi results.json (và schema.json nếu chụp được).
// Container, stub và tiến trình được dọn bằng ctx riêng (WithoutCancel, hạn 30s) để vẫn dọn được khi s.ctx đã hủy.
func (s *session) run(o Options, pb *prebuilt) (runOutcome, error) {
	h := s.h
	var oc runOutcome
	var cleanups []func(context.Context)
	defer func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), cleanupTimeout)
		defer cancel()
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i](cctx)
		}
	}()

	mode, merr := ParseMode(o.Mode)
	if merr != nil {
		mode = o.Mode
	}
	runID := fmt.Sprintf("aisdlc-%s-%08x", s.now().Format("20060102t150405"), randUint32())
	s.logf(i18n.T("feature %s, mode %s, bước %v, run %s"), o.Feature, mode, o.Steps, runID)
	paths := model.FeaturePaths{RepoRoot: h.Root, Key: o.Feature}
	svc := h.Config.Service

	var cs spec.CasesSpec
	var selected []spec.CaseSpec
	results := map[int]run.CaseResult{}
	var schema *run.SchemaSnapshot

	// finish điền case chưa có kết quả bằng fill, áp timeout, tính ExitCode và ghi file.
	finish := func(fill string) (runOutcome, error) {
		cases := make([]run.CaseResult, 0, len(selected))
		for i, c := range selected {
			r, ok := results[i]
			if !ok {
				r = errResult(c, fill)
			}
			cases = append(cases, r)
		}
		timedOut := s.ctx.Err() != nil
		switch {
		case timedOut:
			msg := s.stopMessage()
			for i := range cases {
				cases[i].Outcome, cases[i].Message = run.CaseOutcomeError, msg
			}
			s.logf("%s", msg)
			oc.res.ExitCode = ExitTimeout
			oc.harnessErr = "" // lỗi do ctx hết hạn/bị hủy không phải lỗi hạ tầng; TimedOut mang thông tin này
		case oc.harnessErr != "":
			oc.res.ExitCode = exitHarness
		default:
			for _, c := range cases {
				if c.Outcome != run.CaseOutcomePassed {
					oc.res.ExitCode = 1
				}
			}
		}
		oc.res.Cases = cases
		for _, c := range cases {
			s.logf("%s [%s] %s %s", c.ID, c.Step, c.Outcome, c.Message)
		}
		// results.json trước: luôn được ghi kể cả khi ghi schema lỗi
		if err := WriteResults(s.out, string(o.Feature), mode, cases); err != nil {
			return oc, err
		}
		if schema != nil && !timedOut {
			return oc, schema.Write(filepath.Join(s.out, "schema.json"))
		}
		return oc, nil
	}
	fail := func(msg string) (runOutcome, error) {
		oc.harnessErr = msg
		s.logf("%s", msg)
		return finish(msg)
	}
	// failRow: chưa có danh sách case (mode sai, cases.yaml hỏng) thì một dòng HARNESS như AisdlcCaseSuite.
	failRow := func(msg string) (runOutcome, error) {
		selected = []spec.CaseSpec{{ID: harnessRowID}}
		return fail(msg)
	}

	// 2. cases.yaml
	if merr != nil {
		return failRow(merr.Error())
	}
	check := mode == ModeMigrationCheck
	if !check {
		var err error
		if cs, err = spec.ReadCases(paths.Cases()); err != nil {
			return failRow(i18n.Tf("không đọc được cases.yaml: %s", err.Error()))
		}
		selected = Select(cs.Cases, o.Steps)
		s.logf(i18n.T("%d case được chọn"), len(selected))
	}

	// 3. build
	var binary string
	if !check {
		if pb == nil {
			s.logf("build %s", svc.Main)
			b, err := h.Runtime.Build(s.ctx, h.Root, svc, s.out)
			pb = &prebuilt{b, err}
		}
		if pb.err != nil {
			return fail(i18n.Tf("không build được: %s", pb.err.Error()))
		}
		if !pb.res.OK {
			s.raw(pb.res.Output)
			return fail(buildFailed())
		}
		binary = pb.res.Binary
	}

	// 4. hạ tầng
	var dbURL, brokers string
	if svc.Env.DatabaseURL != "" || check {
		s.logf(i18n.T("khởi động Postgres %s"), svc.Postgres.Image)
		url, stop, err := s.infra().Postgres(s.ctx, svc.Postgres.Image, runID)
		if err != nil {
			return fail(i18n.Tf("không khởi động được Postgres: %s", err.Error()))
		}
		cleanups = append(cleanups, func(ctx context.Context) {
			if err := stop(ctx); err != nil {
				s.logf(i18n.T("dừng Postgres: %v"), err)
			}
		})
		dbURL = url
	}
	if svc.Env.KafkaBrokers != "" && !check {
		s.logf(i18n.T("khởi động Kafka %s"), svc.Kafka.Image)
		b, stop, err := s.infra().Kafka(s.ctx, svc.Kafka.Image, svc.Kafka.Topics, runID)
		if err != nil {
			return fail(i18n.Tf("không khởi động được Kafka: %s", err.Error()))
		}
		cleanups = append(cleanups, func(ctx context.Context) {
			if err := stop(ctx); err != nil {
				s.logf(i18n.T("dừng Kafka: %v"), err)
			}
		})
		brokers = b
	}

	// 5. migration (chính, rồi nháp với cases-with-drafts và migration-check)
	if dbURL != "" {
		mainDir := filepath.Join(h.Root, filepath.FromSlash(h.Config.Migrations.Dir))
		dirs := []string{mainDir}
		if WithDrafts(mode) {
			drafts := filepath.Join(s.out, "drafts")
			ok, err := PrepareDrafts(paths.DbDrafts(), mainDir, drafts, h.Adapter)
			if err != nil {
				return fail(err.Error())
			}
			if ok {
				dirs = append(dirs, drafts)
			}
		}
		s.logf("migration %v", dirs)
		if err := Migrate(s.ctx, dbURL, dirs...); err != nil {
			return fail(err.Error())
		}
	}

	// 6. migration-check: chỉ dump schema
	if check {
		snap, err := DumpSchema(s.ctx, dbURL)
		if err != nil {
			return fail(i18n.Tf("không dump được schema: %s", err.Error()))
		}
		schema = &snap
		return finish(NotRunText())
	}

	// 7. stub và env
	st, err := stub.Start()
	if err != nil {
		return fail(i18n.Tf("không khởi động được stub server: %s", err.Error()))
	}
	cleanups = append(cleanups, func(context.Context) { st.Close() })
	port, err := freePort()
	if err != nil {
		return fail(i18n.Tf("không lấy được cổng trống: %s", err.Error()))
	}
	env := map[string]string{}
	for k, v := range svc.ExtraEnv {
		env[k] = v
	}
	env[svc.Env.Port] = strconv.Itoa(port)
	if svc.Env.DatabaseURL != "" {
		env[svc.Env.DatabaseURL] = dbURL
	}
	if svc.Env.KafkaBrokers != "" {
		env[svc.Env.KafkaBrokers] = brokers
	}
	for _, name := range svc.Env.ExternalBaseURLs {
		env[name] = st.URL()
	}

	// 8. khởi động dịch vụ, chờ healthPath
	serviceLog := filepath.Join(s.out, "service.log")
	s.logf(i18n.T("khởi động dịch vụ ở cổng %d"), port)
	proc, err := h.Runtime.Start(s.ctx, binary, env, s.coverDir(), serviceLog)
	if err != nil {
		s.tail(serviceLog)
		return fail(err.Error())
	}
	oc.started = true
	untrack := track(proc)
	stopped := false
	stopService := func() {
		if stopped {
			return
		}
		stopped = true
		oc.graceful = proc.Stop(time.Duration(svc.ShutdownTimeoutSeconds) * time.Second)
		untrack()
		if !oc.graceful {
			oc.killed = i18n.Tf("dịch vụ không tắt êm khi nhận SIGTERM sau %d giây (đã SIGKILL)", svc.ShutdownTimeoutSeconds)
			s.logf("%s", oc.killed)
		}
	}
	cleanups = append(cleanups, func(context.Context) { stopService() })
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	if msg := waitHealthy(s.ctx, proc, base+svc.HealthPath, time.Duration(svc.StartTimeoutSeconds)*time.Second); msg != "" {
		stopService()
		s.logf("%s", msg)
		s.tail(serviceLog)
		oc.harnessErr = msg
		return finish(NotRunText())
	}
	s.logf("%s", i18n.T("dịch vụ sẵn sàng"))

	// 9. chạy case
	if invariants, order, err := Checks(paths); err != nil {
		for i, c := range selected {
			results[i] = errResult(c, i18n.Tf("không đọc được bất biến (design.yaml/cases.yaml/risk.yaml): %s", err.Error()))
		}
	} else {
		var db DB = noDB{}
		if dbURL != "" {
			pg, err := OpenPgDB(s.ctx, dbURL)
			if err != nil {
				stopService()
				return fail(i18n.Tf("không kết nối được Postgres: %s", err.Error()))
			}
			defer pg.Close()
			db = pg
		}
		var probe MessageProbe = NoMessageProbe{}
		if brokers != "" {
			kp := NewKafkaProbe(brokers, messageTopics(selected), 0)
			defer kp.Close()
			probe = kp
		}
		ex := &Executor{HTTP: &HTTPCaller{BaseURL: base}, DB: db, Stubs: st, Messages: probe,
			ResetSQL: cs.ResetSQL, AllStubs: cs.Stubs, Checks: invariants, StepOrder: order}
		for i, c := range selected {
			if s.ctx.Err() != nil {
				break
			}
			results[i] = ex.Execute(s.ctx, c)
		}
	}

	// 10. dump schema, tắt dịch vụ, ghi kết quả. Không có database: schema rỗng, như migration-check khi không có
	// migration nào, để SCHEMA_MATCH so được.
	if s.ctx.Err() == nil {
		if dbURL == "" {
			schema = &run.SchemaSnapshot{}
		} else if snap, err := DumpSchema(s.ctx, dbURL); err != nil {
			s.logf(i18n.T("không dump được schema: %v"), err)
		} else {
			schema = &snap
		}
	}
	stopService()
	if oc.graceful && proc.ExitErr() != nil && s.ctx.Err() == nil {
		s.logf(i18n.T("dịch vụ thoát lỗi: %v"), proc.ExitErr())
		s.tail(serviceLog)
	}
	return finish(NotRunText())
}

// tail chép 50 dòng cuối log dịch vụ vào harness.log.
func (s *session) tail(serviceLog string) {
	b, err := os.ReadFile(serviceLog)
	if err != nil {
		s.logf(i18n.T("không đọc được log dịch vụ: %v"), err)
		return
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > serviceLogTail {
		lines = lines[len(lines)-serviceLogTail:]
	}
	s.logf(i18n.T("%d dòng cuối log dịch vụ:"), serviceLogTail)
	s.raw(strings.Join(lines, "\n"))
}

// waitHealthy poll healthPath mỗi 200ms; "" khi nhận 2xx, ngược lại là lý do (tiếng Việt).
func waitHealthy(ctx context.Context, proc lang.Process, url string, timeout time.Duration) string {
	client := &http.Client{Timeout: time.Second}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(healthInterval)
	defer tick.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return i18n.Tf("healthPath không hợp lệ: %s", err.Error())
		}
		if resp, err := client.Do(req); err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return ""
			}
		}
		select {
		case <-proc.Done():
			msg := i18n.T("dịch vụ thoát trước khi sẵn sàng")
			if err := proc.ExitErr(); err != nil {
				msg += ": " + err.Error()
			}
			return msg
		case <-deadline.C:
			return i18n.Tf("dịch vụ không sẵn sàng sau %d giây (%s)", int(timeout.Seconds()), url)
		case <-ctx.Done():
			return i18n.T("dịch vụ chưa sẵn sàng khi harness bị dừng")
		case <-tick.C:
		}
	}
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// messageTopics là các topic mà case được chọn kiểm, đã sắp xếp.
func messageTopics(cases []spec.CaseSpec) []string {
	set := map[string]bool{}
	for _, c := range cases {
		if c.Then != nil {
			for _, m := range c.Then.Messages {
				set[m.Topic] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// noDB dùng khi dịch vụ không khai service.env.databaseUrl: case cần DB thành ERROR.
type noDB struct{}

func (noDB) Exec(context.Context, string) error { return errors.New(noDatabase()) }
func (noDB) Check(context.Context, spec.DbExpect) (string, error) {
	return "", errors.New(noDatabase())
}
func (noDB) Violations(context.Context, string, int) ([]map[string]any, error) {
	return nil, errors.New(noDatabase())
}

// randUint32 là số ngẫu nhiên từ crypto/rand cho ID của lần chạy (không đoán trước được, không cần seed).
func randUint32() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint32(b[:])
}
