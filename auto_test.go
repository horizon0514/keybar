package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"keybar/approval"
	"keybar/clip"
	"keybar/scan"
	"keybar/vault"

	"github.com/egoist/mygo/ui"
)

func actions(v *vault.Vault) []string {
	var out []string
	for _, ev := range vault.ReadLog(v.Dir, 0, nil) {
		out = append(out, ev.Action)
	}
	return out
}

func TestClipboardOffer(t *testing.T) {
	h := newHarness(t)
	h.settings.Clipboard = true
	f := &clip.Fake{Text: "whatever was there"}
	h.startAuto(f, true)
	h.clipTick() // learns what is there
	if h.auto.found != nil {
		t.Fatal("offered what was there before")
	}
	f.Set("just some words", false)
	h.clipTick()
	if h.auto.found != nil {
		t.Fatal("offered words")
	}
	f.Set(demoAnthropic, false)
	h.clipTick()
	c := h.auto.found
	if c == nil || c.provider != "Anthropic" || c.name != "ANTHROPIC_API_KEY" || c.masked != "sk-ant-… 96 位" {
		t.Fatalf("%v %v %v", c.provider, c.name, c.masked)
	}
	for _, n := range h.notified {
		if strings.Contains(n, demoAnthropic) {
			t.Error("value in a notification")
		}
	}
	tt := ui.NewTester(h.view, panelWidth, panelHeight)
	tt.Frame()
	if !tt.HasText("发现 Anthropic key · 保存为") {
		t.Error("no notice")
	}
	h.auto.found.name = "ANTHROPIC_KEY_WORK"
	h.saveClip()
	if v, _ := h.v.Value("ANTHROPIC_KEY_WORK"); string(v) != demoAnthropic {
		t.Error("not saved")
	}
	if h.auto.found != nil || !slices.Contains(actions(h.v), "clipboard") {
		t.Error("log / notice")
	}
	// copied again: KeyBar has it, nothing offered
	f.Set("x", false)
	h.clipTick()
	f.Set(demoAnthropic, false)
	h.clipTick()
	if h.auto.found != nil {
		t.Error("offered a key KeyBar has")
	}
	// a password manager's copy: not looked at
	f.Set(demoGitHub, true)
	h.clipTick()
	if h.auto.found != nil {
		t.Error("offered a concealed copy")
	}
	// off: not even read
	h.settings.Clipboard = false
	reads := f.Reads
	f.Set(demoHF, false)
	h.clipTick()
	if h.auto.found != nil || f.Reads != reads {
		t.Error("read while off")
	}
	// a name KeyBar has is offered as _2; dismissed, forgotten
	h.settings.Clipboard = true
	f.Set(demoOpenAI, false)
	h.clipTick()
	if h.auto.found == nil || h.auto.found.name != "OPENAI_API_KEY_2" {
		t.Fatal("not offered as _2")
	}
	val := h.auto.found.value
	h.dismissClip()
	if h.auto.found != nil || strings.Contains(string(val), "sk-") {
		t.Error("not forgotten")
	}
	// expires
	f.Set(demoHF, false)
	h.clipTick()
	h.clock = h.clock.Add(3 * time.Minute)
	h.clipTick()
	if h.auto.found != nil {
		t.Error("kept past two minutes")
	}
	// the settings file never holds a value
	b, _ := os.ReadFile(filepath.Join(h.v.Dir, "settings.json"))
	if strings.Contains(string(b), "sk-") {
		t.Error("settings")
	}
}

func TestEnvNoticeAndScan(t *testing.T) {
	h := newHarness(t)
	h.settings.WatchEnv = true
	home := h.agents.Home
	demoScanHome(home)
	h.startAuto(nil, false)
	h.auto.watcher.Settle = 0
	h.envTick() // the baseline
	h.run()
	p := filepath.Join(home, "code", "new-app", ".env")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte("PORT=1\nGROQ_API_KEY=gsk_Dm0Kx7Qp2Lw9Vb4Nr8Ty3Hs6Jc1Fg5Zu0Dm0Kx7Qp2Lw9Vb4Nr8Ty3Hs6J\nAPP_SECRET=Dm0Kx7Qp2Lw9Vb4Nr8Ty3Hs6\n"), 0o600)
	h.envTick()
	h.run()
	if len(h.auto.envNotes) != 0 {
		t.Fatal("told before settled")
	}
	h.envTick()
	h.run()
	if len(h.auto.envNotes) != 1 || h.auto.envNotes[0].n != 2 || h.auto.envNotes[0].path != p {
		t.Fatalf("%+v", h.auto.envNotes)
	}
	tt := ui.NewTester(h.view, panelWidth, panelHeight)
	tt.Frame()
	if !tt.HasText(filepath.FromSlash("new-app/.env") + " 有 2 个新 key") {
		t.Error("no notice")
	}
	// 导入: the file's checklist
	h.openScan(p)
	h.run()
	if h.page != pageScan || len(h.auto.scan.groups) != 1 || len(h.auto.envNotes) != 0 {
		t.Fatalf("%v %d", h.page, len(h.auto.scan.groups))
	}
	tt.Frame()
	if !tt.HasText("gsk_… 62 位") || tt.HasText("Dm0Kx7") {
		t.Error("values shown")
	}
	h.importScan()
	if v, _ := h.v.Value("GROQ_API_KEY"); len(v) == 0 {
		t.Error("not imported")
	}
	if _, ok := h.key("PORT"); ok {
		t.Error("PORT imported")
	}
	// ignore a file: never told again
	_ = os.WriteFile(p, []byte("OTHER_TOKEN=Dm0Kx7Qp2Lw9Vb4Nr8Ty3Hs6Jc\n"), 0o600)
	h.ignoreEnv(p)
	h.envTick()
	h.run()
	h.envTick()
	h.run()
	if len(h.auto.envNotes) != 0 || !slices.Contains(h.settings.IgnoredFiles, p) {
		t.Error("ignored file told")
	}
	// off
	h.settings.WatchEnv = false
	h.auto.watcher = nil
	h.envTick()
}

func TestScanMachine(t *testing.T) {
	h := newHarness(t)
	home := h.agents.Home
	demoScanHome(home)
	h.openScan("")
	h.run()
	s := &h.auto.scan
	if s.running || len(s.groups) != 3 {
		t.Fatalf("%d groups", len(s.groups))
	}
	var paths []string
	for _, g := range s.groups {
		paths = append(paths, h.shortHome(g.Plan.Path))
	}
	if strings.Join(paths, ",") != filepath.FromSlash("~/code/blog/.env.local,~/code/my-app/.env,~/.zshrc") {
		t.Error(paths)
	}
	tt := ui.NewTester(h.view, panelWidth, panelHeight)
	tt.Frame()
	for _, want := range []string{filepath.FromSlash("~/code/my-app/.env"), "OPENAI_API_KEY", "sk-proj-… 60 位", "不像密钥", "shell 配置 · export"} {
		if !tt.HasText(want) {
			t.Errorf("no %q", want)
		}
	}
	myApp := filepath.Join(home, "code", "my-app", ".env")
	rc := filepath.Join(home, ".zshrc")
	s.clean[myApp] = true
	s.mode[myApp] = scan.Names
	s.clean[rc] = true
	h.importScan()
	if h.page != pageList || !strings.Contains(h.toast, "从 3 个文件导入") {
		t.Error(h.toast)
	}
	for _, n := range []string{"ANTHROPIC_API_KEY", "HF_TOKEN", "DATABASE_URL", "STRIPE_SECRET_KEY"} {
		if _, ok := h.key(n); !ok {
			t.Error("missing", n)
		}
	}
	// OPENAI_API_KEY: KeyBar has another value: kept (blog's first, then my-app's same)
	if v, _ := h.v.Value("OPENAI_API_KEY"); string(v) != "sk-example-openai-0000" {
		t.Error("overwritten")
	}
	b, _ := os.ReadFile(myApp)
	// imported lines lose their value; kept ones (OPENAI: not imported) stay
	if !strings.Contains(string(b), "STRIPE_SECRET_KEY=\n") || !strings.Contains(string(b), "OPENAI_API_KEY="+demoOpenAI) || !strings.Contains(string(b), "PORT=3000") {
		t.Errorf("%s", b)
	}
	if _, err := os.Stat(myApp + scan.BackupSuffix); err != nil {
		t.Error("no backup")
	}
	b, _ = os.ReadFile(rc)
	// GITHUB_TOKEN: KeyBar has another value, kept, so its line stays
	if strings.Contains(string(b), demoHF) || !strings.Contains(string(b), "# moved to KeyBar: HF_TOKEN") || !strings.Contains(string(b), demoGitHub) {
		t.Errorf("%s", b)
	}
	// the blog file was not opted in: untouched
	b, _ = os.ReadFile(filepath.Join(home, "code", "blog", ".env.local"))
	if !strings.Contains(string(b), demoAnthropic) {
		t.Error("blog cleaned")
	}
	acts := actions(h.v)
	for _, a := range []string{"scan", "import", "clean"} {
		if !slices.Contains(acts, a) {
			t.Error("no", a, "in log")
		}
	}
	for _, ev := range vault.ReadLog(h.v.Dir, 0, nil) {
		if strings.Contains(ev.Detail, "Dm0Kx7") {
			t.Error("value in log")
		}
	}
}

func TestApprovalFill(t *testing.T) {
	h := newHarness(t)
	r := approval.Request{ID: "1", Client: "mcp:claude-code", Action: "run", Keys: []string{"DEEPSEEK_API_KEY"},
		Missing: []string{"ANTHROPIC_API_KEY", "STRIPE_SECRET_KEY", "SENTRY_DSN"}, Project: "/Users/me/code/my-app", Command: "npm test"}
	done := make(chan approval.Decision, 1)
	go func() { done <- h.request(r) }()
	<-h.posted
	h.mu.Lock()
	fn := h.queue[0]
	h.queue = nil
	h.mu.Unlock()
	fn()
	p := h.pending[0]
	tt := ui.NewTester(h.view, panelWidth, panelHeight)
	tt.Frame()
	if !tt.HasText("KeyBar 里没有 3") || !tt.HasText("ANTHROPIC_API_KEY") {
		t.Error("no paste rows")
	}
	*p.fill["ANTHROPIC_API_KEY"] = demoAnthropic
	p.never["SENTRY_DSN"] = true
	tt.Frame()
	if !tt.HasText("保存 1 个并允许一次") {
		t.Error("button")
	}
	h.decide(true, false)
	d := <-done
	if !d.Allow || strings.Join(d.Added, ",") != "ANTHROPIC_API_KEY" {
		t.Errorf("%+v", d)
	}
	if v, _ := h.v.Value("ANTHROPIC_API_KEY"); string(v) != demoAnthropic {
		t.Error("not saved")
	}
	if k, _ := h.key("ANTHROPIC_API_KEY"); !slices.Contains(k.Projects, "my-app") {
		t.Errorf("%+v", k)
	}
	if !slices.Contains(h.settings.NeverAskIn("/Users/me/code/my-app"), "SENTRY_DSN") {
		t.Error("never ask")
	}
	if *p.fill["ANTHROPIC_API_KEY"] != "" {
		t.Error("pasted value kept")
	}
	// denied: nothing saved
	r.ID, r.Missing = "2", []string{"STRIPE_SECRET_KEY"}
	go func() { done <- h.request(r) }()
	<-h.posted
	h.mu.Lock()
	fn = h.queue[0]
	h.queue = nil
	h.mu.Unlock()
	fn()
	*h.pending[0].fill["STRIPE_SECRET_KEY"] = demoStripe
	h.decide(false, false)
	<-done
	if _, ok := h.key("STRIPE_SECRET_KEY"); ok {
		t.Error("saved on deny")
	}
	// granted before, but names missing: still asks
	h.grants.Add("mcp:claude-code", []string{"DEEPSEEK_API_KEY"}, h.clock.Add(time.Hour))
	r.ID = "3"
	go func() { done <- h.request(r) }()
	select {
	case <-h.posted:
		h.mu.Lock()
		h.queue = nil
		h.mu.Unlock()
	case <-time.After(2 * time.Second):
		t.Fatal("granted without asking for the missing")
	}
}
