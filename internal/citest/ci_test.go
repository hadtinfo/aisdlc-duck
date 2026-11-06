package citest

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type pipeline = map[string]any

func load(t *testing.T) pipeline {
	t.Helper()
	b, err := os.ReadFile("../../.gitlab-ci.yml")
	if err != nil {
		t.Fatalf("đọc .gitlab-ci.yml: %v", err)
	}
	var p pipeline
	if err := yaml.Unmarshal(b, &p); err != nil {
		t.Fatalf("parse YAML: %v", err)
	}
	return p
}

// job trả về job đã áp dụng extends (gộp sâu như GitLab: map gộp, giá trị khác thì job con thắng).
func job(t *testing.T, p pipeline, name string) map[string]any {
	t.Helper()
	j, ok := p[name].(map[string]any)
	if !ok {
		t.Fatalf("thiếu job %q", name)
	}
	parent := str(j["extends"])
	if parent == "" {
		return j
	}
	base := job(t, p, parent)
	return merge(base, j)
}

func merge(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		am, aok := out[k].(map[string]any)
		bm, bok := v.(map[string]any)
		if aok && bok {
			out[k] = merge(am, bm)
		} else {
			out[k] = v
		}
	}
	return out
}

func str(v any) string { s, _ := v.(string); return s }

func script(j map[string]any) string {
	var sb strings.Builder
	l, _ := j["script"].([]any)
	for _, x := range l {
		sb.WriteString(str(x) + "\n")
	}
	return sb.String()
}

func TestStagesVaJob(t *testing.T) {
	p := load(t)
	var got []string
	for _, s := range p["stages"].([]any) {
		got = append(got, str(s))
	}
	want := []string{"check", "ui", "build", "docker", "e2e"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("stages = %v, want %v", got, want)
	}
	for name, stage := range map[string]string{"check": "check", "ui": "ui", "build": "build", "docker": "docker", "e2e": "e2e"} {
		if s := str(job(t, p, name)["stage"]); s != stage {
			t.Errorf("job %s stage = %q, want %q", name, s, stage)
		}
	}
}

func hasDind(j map[string]any) bool {
	svcs, _ := j["services"].([]any)
	for _, s := range svcs {
		if m, ok := s.(map[string]any); ok && strings.HasPrefix(str(m["name"]), "docker:") && strings.HasSuffix(str(m["name"]), "dind") {
			return true
		}
		if strings.HasPrefix(str(s), "docker:") && strings.HasSuffix(str(s), "dind") {
			return true
		}
	}
	return false
}

func TestJobDungTestcontainersCoDind(t *testing.T) {
	p := load(t)
	for _, name := range []string{"check", "e2e", "docker"} {
		j := job(t, p, name)
		if !hasDind(j) {
			t.Errorf("job %s thiếu service docker:dind", name)
		}
		vars, _ := j["variables"].(map[string]any)
		if str(vars["DOCKER_HOST"]) != "tcp://docker:2375" {
			t.Errorf("job %s: DOCKER_HOST = %q", name, str(vars["DOCKER_HOST"]))
		}
		if v, ok := vars["DOCKER_TLS_CERTDIR"]; !ok || str(v) != "" {
			t.Errorf("job %s: DOCKER_TLS_CERTDIR phải là chuỗi rỗng", name)
		}
		if name == "docker" {
			continue
		}
		if str(vars["TESTCONTAINERS_HOST_OVERRIDE"]) != "docker" {
			t.Errorf("job %s: TESTCONTAINERS_HOST_OVERRIDE = %q", name, str(vars["TESTCONTAINERS_HOST_OVERRIDE"]))
		}
		if _, set := vars["AISDLC_SKIP_DOCKER"]; set {
			t.Errorf("job %s không được đặt AISDLC_SKIP_DOCKER", name)
		}
	}
}

func TestLenhTungJob(t *testing.T) {
	p := load(t)
	if s := script(job(t, p, "check")); !strings.Contains(s, "make check") {
		t.Errorf("check thiếu make check: %s", s)
	}
	if s := script(job(t, p, "ui")); !strings.Contains(s, "make ui") || !strings.Contains(s, "make ui-test") {
		t.Errorf("ui thiếu make ui / make ui-test: %s", s)
	}
	b := job(t, p, "build")
	if s := script(b); !strings.Contains(s, "make plugin-dist") || !strings.Contains(s, "make build-web") {
		t.Errorf("build thiếu lệnh: %s", s)
	}
	if _, ok := b["artifacts"]; !ok {
		t.Error("build thiếu artifacts")
	}
	if s := script(job(t, p, "docker")); !strings.Contains(s, "Dockerfile.web") {
		t.Errorf("docker thiếu Dockerfile.web: %s", s)
	}
}

func TestDockerChiTrenTagHoacMain(t *testing.T) {
	rules, _ := job(t, load(t), "docker")["rules"].([]any)
	if len(rules) == 0 {
		t.Fatal("docker thiếu rules")
	}
	var all strings.Builder
	for _, r := range rules {
		all.WriteString(str(r.(map[string]any)["if"]) + "\n")
	}
	if !strings.Contains(all.String(), "$CI_COMMIT_TAG") || !strings.Contains(all.String(), "$CI_DEFAULT_BRANCH") {
		t.Errorf("rules docker phải gồm tag và nhánh mặc định: %s", all.String())
	}
}

func TestE2EChayTayHoacLichHaiLan(t *testing.T) {
	j := job(t, load(t), "e2e")
	rules, _ := j["rules"].([]any)
	var all strings.Builder
	for _, r := range rules {
		m := r.(map[string]any)
		all.WriteString(str(m["if"]) + "|" + str(m["when"]) + "\n")
	}
	if !strings.Contains(all.String(), "schedule") || !strings.Contains(all.String(), "manual") {
		t.Errorf("e2e phải chạy tay hoặc theo lịch: %s", all.String())
	}
	if n := strings.Count(script(j), "make e2e"); n != 2 {
		t.Errorf("e2e phải gọi make e2e đúng 2 lần, có %d", n)
	}
}

func TestCacheGoTheoGoSum(t *testing.T) {
	p := load(t)
	d, _ := p["default"].(map[string]any)
	c, _ := d["cache"].(map[string]any)
	k, _ := c["key"].(map[string]any)
	files, _ := k["files"].([]any)
	if len(files) != 1 || str(files[0]) != "go.sum" {
		t.Errorf("cache key phải theo go.sum: %v", c)
	}
}

func TestDindTatTLS(t *testing.T) {
	for _, name := range []string{"check", "e2e", "docker"} {
		svcs, _ := job(t, load(t), name)["services"].([]any)
		ok := false
		for _, s := range svcs {
			m, _ := s.(map[string]any)
			cmd, _ := m["command"].([]any)
			for _, c := range cmd {
				if str(c) == "--tls=false" {
					ok = true
				}
			}
		}
		if !ok {
			t.Errorf("job %s: service dind thiếu command --tls=false", name)
		}
	}
}

func TestE2EKhongCoGuardBoQua(t *testing.T) {
	s := script(job(t, load(t), "e2e"))
	if strings.Contains(s, "grep") || strings.Contains(s, "exit 0") {
		t.Errorf("e2e không được có guard bỏ qua: %s", s)
	}
}

func TestDockerKhongKhaiBaoLaiBienDind(t *testing.T) {
	raw := load(t)["docker"].(map[string]any)
	if v, ok := raw["variables"]; ok {
		t.Errorf("job docker không nên khai báo lại variables của .dind: %v", v)
	}
	if !strings.Contains(script(raw), "aisdlc") {
		t.Error("docker cần tag dự phòng khi không có CI_REGISTRY_IMAGE")
	}
}

func TestNeedsVaArtifactLaDauRaMakefile(t *testing.T) {
	p := load(t)
	for _, name := range []string{"docker", "e2e"} {
		if n, ok := job(t, p, name)["needs"].([]any); !ok || len(n) != 0 {
			t.Errorf("job %s phải có needs: []", name)
		}
	}
	b := job(t, p, "build")
	if n, _ := b["needs"].([]any); len(n) != 1 || str(n[0]) != "ui" {
		t.Errorf("build phải needs: [ui], có %v", b["needs"])
	}
	if strings.Contains(script(b), "make build-web\n") || strings.Contains(script(b), "npm") {
		t.Error("build không được chạy lại UI")
	}
	mk, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"build", "ui"} {
		arts, _ := job(t, p, name)["artifacts"].(map[string]any)
		paths, _ := arts["paths"].([]any)
		if len(paths) == 0 {
			t.Fatalf("job %s thiếu artifacts.paths", name)
		}
		for _, x := range paths {
			p := strings.TrimSuffix(str(x), "/")
			base := p[strings.LastIndex(p, "/")+1:]
			if !strings.Contains(string(mk), p) && !strings.Contains(string(mk), base) {
				t.Errorf("artifact %s không thấy trong Makefile", p)
			}
		}
	}
}
