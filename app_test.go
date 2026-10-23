package main

import (
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"keybar/agents"
	"keybar/approval"
	"keybar/importer"
	"keybar/vault"

	"github.com/egoist/mygo/ui"
)

type harness struct {
	*app
	t        *testing.T
	mu       sync.Mutex
	queue    []func()
	posted   chan struct{}
	clock    time.Time
	notified []string
	copied   []string
	hidden   int
	shownN   int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	v := &vault.Vault{Dir: t.TempDir(), Secrets: &vault.Memory{}, By: "app"}
	seedDemo(v, demoNow)
	h := &harness{t: t, clock: demoNow, posted: make(chan struct{}, 100)}
	v.Now = func() time.Time { return h.clock }
	a := newApp(v)
	h.app = a
	a.now = func() time.Time { return h.clock }
	a.demo = true
	a.exe = "/Applications/KeyBar.app/Contents/MacOS/KeyBar"
	a.agents = demoAgents(a.exe)
	t.Cleanup(func() { os.RemoveAll(a.agents.Home) })
	a.settings.Shortcut = defaultShortcut
	a.post = func(fn func()) {
		h.mu.Lock()
		h.queue = append(h.queue, fn)
		h.mu.Unlock()
		h.posted <- struct{}{}
	}
	a.spawn = func(fn func()) { fn() }
	a.later = func(time.Duration, func()) {}
	a.notify = func(id, title, body string) { h.notified = append(h.notified, title+" | "+body) }
	a.copy = func(s string) { h.copied = append(h.copied, s) }
	a.hide = func() { h.hidden++ }
	a.show = func() { h.shownN++ }
	return h
}

// run runs what was posted.
func (h *harness) run() {
	for {
		h.mu.Lock()
		q := h.queue
		h.queue = nil
		h.mu.Unlock()
		if len(q) == 0 {
			return
		}
		for _, fn := range q {
			<-h.posted
			fn()
		}
	}
}

func (h *harness) tester(w int) *ui.Tester {
	h.shown()
	tt := ui.NewTester(h.view, w, panelHeight)
	tt.Frame()
	return tt
}

func TestListOrderAndSearch(t *testing.T) {
	h := newHarness(t)
	rows := h.rows()
	if rows[0].Name != "TIDB_PROD_URL" { // overdue first
		t.Errorf("first %s", rows[0].Name)
	}
	tt := h.tester(panelWidth)
	if !tt.HasText("RAILWAY_TOKEN") || !tt.HasText("1 超期") || !tt.HasText("1 快到期") || !tt.HasText("1 待同步") {
		t.Errorf("texts %q", tt.Texts())
	}
	tt.Type("vercel")
	tt.Frame()
	if rows := h.rows(); len(rows) != 2 { // VERCEL_TOKEN, and TIDB's platform
		t.Errorf("search: %d", len(rows))
	}
	tt.Key(0, ui.KeyEscape)
	if h.query != "" {
		t.Error("esc did not clear the search")
	}
	tt.Key(0, ui.KeyEscape)
	if h.hidden != 1 {
		t.Error("esc did not hide")
	}
}

func TestDetailAndRotate(t *testing.T) {
	h := newHarness(t)
	tt := h.tester(panelWidth)
	h.choose(indexOf(h.rows(), "RAILWAY_TOKEN"))
	// Enter now copies; open the detail explicitly.
	h.openDetail("RAILWAY_TOKEN")
	tt.Frame()
	if h.page != pageDetail || h.detail != "RAILWAY_TOKEN" {
		t.Fatalf("page %v %s", h.page, h.detail)
	}
	tt.Frame()
	if !tt.HasText("Railway · rz-api") || !tt.HasText("还是旧值") {
		t.Errorf("%q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyR)
	if h.page != pageForm || !h.form.rotate {
		t.Fatal("⌘R did not open the rotation")
	}
	h.form.value, h.form.value2 = "dummy-rotated-9999", "dummy-rotated-9999"
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyEnter)
	h.run()
	if h.form.err != "" {
		t.Fatal(h.form.err)
	}
	k, _ := h.key("RAILWAY_TOKEN")
	if v, _ := h.v.Value("RAILWAY_TOKEN"); string(v) != "dummy-rotated-9999" {
		t.Error("not stored")
	}
	if len(k.Pending()) != 3 || len(k.History) != 2 {
		t.Errorf("pending %d history %d", len(k.Pending()), len(k.History))
	}
	// The GitHub platform syncs at once: without a token key or gh it fails.
	if m := h.syncMsg["RAILWAY_TOKEN/p1"]; !strings.HasPrefix(m, "✗") {
		t.Errorf("sync msg %q", m)
	}
	h.togglePlatform("RAILWAY_TOKEN", k.Platforms[1], true)
	k, _ = h.key("RAILWAY_TOKEN")
	if len(k.Pending()) != 2 {
		t.Errorf("tick: %d", len(k.Pending()))
	}
	tt.Key(ui.Cmd, ui.KeyC)
	if len(h.copied) != 1 || h.copied[0] != "dummy-rotated-9999" {
		t.Errorf("copied %v", h.copied)
	}
	noValues(t, h.v.Dir, "dummy-rotated-9999")
}

func TestAdd(t *testing.T) {
	h := newHarness(t)
	tt := h.tester(panelWidth)
	tt.Key(ui.Cmd, ui.KeyN)
	if h.page != pageForm || h.form.rotate {
		t.Fatal("⌘N")
	}
	h.form = form{name: "NEW_KEY", value: "dummy-new-0000", desc: "test"}
	h.submit()
	if h.form.err != "" {
		t.Fatal(h.form.err)
	}
	k, ok := h.key("NEW_KEY")
	if !ok || k.Description != "test" || len(k.Platforms) != 0 || h.page != pageDetail {
		t.Errorf("%+v", k)
	}
	// metadata lives on the detail page
	h.meta.days, h.meta.projects, h.meta.tags = "30", "a, b", "ai"
	h.addPlat = "github:o/r"
	h.addPlatform("NEW_KEY")
	h.addPlat = "railway:api"
	h.addPlatform("NEW_KEY")
	h.saveMeta()
	k, _ = h.key("NEW_KEY")
	if k.RotateDays != 30 || len(k.Projects) != 2 || len(k.Platforms) != 2 {
		t.Errorf("meta %+v", k)
	}
	h.openAdd()
	h.form = form{name: "bad name", value: "x"}
	h.submit()
	if h.form.err == "" {
		t.Error("bad name accepted")
	}
}

func TestOpenDueRotate(t *testing.T) {
	h := newHarness(t)
	// TIDB_PROD_URL is overdue in the demo seed
	h.openDueRotate()
	if h.page != pageForm || !h.form.rotate || h.form.name != "TIDB_PROD_URL" {
		t.Fatalf("page=%v rotate=%v name=%q", h.page, h.form.rotate, h.form.name)
	}
	tt := h.tester(panelWidth)
	tt.Frame()
	if !tt.HasText("自动推送") && !tt.HasText("粘贴") {
		// form copy mentions paste / auto push
		if !tt.HasText("新值") {
			t.Errorf("%q", tt.Texts())
		}
	}
}

func TestSaveMeta(t *testing.T) {
	h := newHarness(t)
	h.openDetail("OPENAI_API_KEY")
	h.meta.desc = "updated desc"
	h.meta.days = "60"
	h.meta.tags = "ai, personal"
	h.saveMeta()
	if h.meta.err != "" {
		t.Fatal(h.meta.err)
	}
	k, _ := h.key("OPENAI_API_KEY")
	if k.Description != "updated desc" || k.RotateDays != 60 || len(k.Tags) != 2 {
		t.Errorf("%+v", k)
	}
}

func TestApprovalByKeys(t *testing.T) {
	h := newHarness(t)
	tt := h.tester(panelWidth)
	r := approval.Request{Client: "mcp:claude-code", Action: "run", Keys: []string{"OPENAI_API_KEY"}, Command: "npm test"}
	done := make(chan approval.Decision, 1)
	go func() { done <- h.request(r) }()
	<-h.posted
	h.posted <- struct{}{} // run() reads it back
	h.run()
	if len(h.pending) != 1 || h.shownN != 1 || len(h.notified) != 1 {
		t.Fatalf("pending %d shown %d", len(h.pending), h.shownN)
	}
	tt.Frame()
	if !tt.HasText("Claude Code 要用这些密钥运行一个命令") || !tt.HasText("npm test") {
		t.Errorf("%q", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter) // too soon: ignored
	if len(h.pending) != 1 {
		t.Fatal("answered by a key typed as it appeared")
	}
	h.clock = h.clock.Add(time.Second)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyEnter)
	d := <-done
	if !d.Allow || d.For != 15*time.Minute || len(h.pending) != 0 {
		t.Fatalf("%+v", d)
	}
	// The grant answers the next request without asking.
	if d := h.request(r); !d.Granted {
		t.Errorf("grant: %+v", d)
	}
	// Esc denies.
	r.Keys = []string{"TIDB_PROD_URL"}
	go func() { done <- h.request(r) }()
	<-h.posted
	h.posted <- struct{}{}
	h.run()
	h.clock = h.clock.Add(time.Second)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if d := <-done; d.Allow {
		t.Error("esc allowed")
	}
}

func TestReminder(t *testing.T) {
	h := newHarness(t)
	h.remind()
	if len(h.notified) != 1 || !strings.Contains(h.notified[0], "TIDB_PROD_URL") {
		t.Errorf("%v", h.notified)
	}
	h.remind()
	if len(h.notified) != 1 {
		t.Error("reminded twice a day")
	}
	if h.trayTitle() != "⚠︎2" {
		t.Errorf("title %q", h.trayTitle())
	}
}

func TestMCPConfig(t *testing.T) {
	h := newHarness(t)
	if c := h.mcpConfig("cursor"); !strings.Contains(c, `"args": ["mcp", "--client", "cursor"]`) || !strings.Contains(c, "MacOS/KeyBar") {
		t.Error(c)
	}
}

func TestIsCLI(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want bool
	}{
		{[]string{"/usr/local/bin/keys"}, true},
		{[]string{`C:\x\keys.exe`, "ls"}, true},
		{[]string{"/Applications/KeyBar.app/Contents/MacOS/KeyBar"}, false},
		{[]string{"KeyBar", "mcp"}, true},
		{[]string{"KeyBar", "-psn_0_123"}, false},
	} {
		if isCLI(c.argv) != c.want {
			t.Errorf("%v", c.argv)
		}
	}
}

func indexOf(ks []vault.Key, name string) int {
	for i, k := range ks {
		if k.Name == name {
			return i
		}
	}
	return -1
}

func noValues(t *testing.T, dir string, values ...string) {
	t.Helper()
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			b, _ := os.ReadFile(p)
			for _, v := range values {
				if strings.Contains(string(b), v) {
					t.Errorf("%s holds a value", p)
				}
			}
		}
		return nil
	})
}

// TestScreenshots renders the panel on the example data, light and dark,
// into shots/ (and docs/screenshots with KEYBAR_DOCS=1).
func TestScreenshots(t *testing.T) {
	if testing.Short() {
		t.Skip("screenshots")
	}
	setPlatform("darwin")
	defer setPlatform(runtime.GOOS)
	shot := func(name string, w int, setup func(h *harness, tt *ui.Tester)) {
		for _, dark := range []bool{false, true} {
			h := newHarness(t)
			h.shown()
			tt := ui.NewTester(h.view, w, panelHeight)
			tt.SetScale(2)
			tt.SetDark(dark)
			tt.Frame()
			setup(h, tt)
			h.run()
			tt.Advance(2 * time.Second)
			tt.Frame()
			suffix := map[bool]string{false: "light", true: "dark"}[dark]
			dir := "shots"
			if d := os.Getenv("KEYBAR_SHOTS"); d != "" {
				dir = d
			}
			save(t, filepath.Join(dir, name+"-"+suffix+".png"), tt)
			if os.Getenv("KEYBAR_DOCS") == "1" {
				save(t, filepath.Join("docs", "screenshots", name+"-"+suffix+".png"), tt)
			}
		}
	}
	shot("list", panelWidth, func(h *harness, tt *ui.Tester) { h.choose(1) })
	shot("detail", panelWidth, func(h *harness, tt *ui.Tester) { h.openDetail("RAILWAY_TOKEN") })
	shot("rotate", panelWidth, func(h *harness, tt *ui.Tester) {
		h.openRotate("RAILWAY_TOKEN")
		h.form.value, h.form.value2, h.form.note = "dummy-0000", "dummy-0000", "Railway 安全通告后轮换"
	})
	shot("add", panelWidth, func(h *harness, tt *ui.Tester) { h.openAdd() })
	shot("approval", panelWidth, func(h *harness, tt *ui.Tester) {
		h.pending = []*pending{{req: demoRequest(), at: h.clock.Add(-25 * time.Second), answer: make(chan approval.Decision, 1)}}
	})
	shot("log", panelWidth, func(h *harness, tt *ui.Tester) { h.openLog() })
	shot("settings-agents", panelWidth, func(h *harness, tt *ui.Tester) {
		h.grants.Add("mcp:claude-code", []string{"DEEPSEEK_API_KEY"}, h.clock.Add(10*time.Minute))
		h.openTab(tabAgents)
	})
	shot("settings-agents-unfolded", panelWidth, func(h *harness, tt *ui.Tester) {
		h.openTab(tabAgents)
		h.showMissing = true
	})
	shot("import", panelWidth, func(h *harness, tt *ui.Tester) {
		h.pickFile = func(func(string)) {}
		h.openImport()
		h.imp.path = "~/code/my-app/.env"
	})
	shot("import-conflicts", panelWidth, func(h *harness, tt *ui.Tester) {
		h.openImport()
		h.imp.path = demoEnvFile()
		h.readImport()
		h.imp.plan.Path = "/Users/me/code/my-app/.env"
		cs := h.imp.plan.Conflicts()
		cs[0].Choice = importer.Rename
		cs[1].Choice = importer.Overwrite
		h.imp.chosen = 0
	})
	shot("scan", panelWidth, func(h *harness, tt *ui.Tester) {
		demoScanHome(h.agents.Home)
		h.openScan("")
		h.run()
		h.auto.scan.clean[filepath.Join(h.agents.Home, "code", "blog", ".env.local")] = true
	})
	shot("scan-rc", panelWidth, func(h *harness, tt *ui.Tester) {
		demoScanHome(h.agents.Home)
		h.openScan("")
		h.run()
		g := h.auto.scan.groups
		h.auto.scan.groups = append(g[1:2:2], g[2])
		h.auto.scan.clean[filepath.Join(h.agents.Home, "code", "my-app", ".env")] = true
		h.auto.scan.mode[filepath.Join(h.agents.Home, "code", "my-app", ".env")] = 1
		h.auto.scan.clean[filepath.Join(h.agents.Home, ".zshrc")] = true
	})
	shot("clipboard-notice", panelWidth, func(h *harness, tt *ui.Tester) { h.offerClip(demoAnthropic) })
	shot("env-notice", panelWidth, func(h *harness, tt *ui.Tester) {
		h.auto.envNotes = []envNote{{path: filepath.Join(h.agents.Home, "code", "my-app", ".env"), n: 3}}
	})
	shot("approval-missing", panelWidth, func(h *harness, tt *ui.Tester) {
		r := approval.Request{ID: "demo", Client: "mcp:claude-code", Action: "run", Keys: []string{"OPENAI_API_KEY"},
			Missing: []string{"ANTHROPIC_API_KEY", "STRIPE_SECRET_KEY", "SENTRY_DSN"}, Project: "/Users/me/code/my-app",
			Command: "npm run test:e2e", Cwd: "~/code/my-app", Reason: "跑端到端测试"}
		p := &pending{req: r, at: h.clock.Add(-12 * time.Second), answer: make(chan approval.Decision, 1), fill: map[string]*string{}, never: map[string]bool{}}
		for _, n := range r.Missing {
			p.fill[n] = new(string)
		}
		*p.fill["ANTHROPIC_API_KEY"] = demoAnthropic
		p.never["SENTRY_DSN"] = true
		h.pending = []*pending{p}
	})
	shot("settings-auto", panelWidth, func(h *harness, tt *ui.Tester) {
		home := h.agents.Home
		h.settings.ScanRoots = []string{"~/code", "~/work"}
		h.settings.IgnoredFiles = []string{filepath.Join(home, "code", "old-site", ".env")}
		h.settings.AddNeverAsk(filepath.Join(home, "code", "my-app"), "SENTRY_DSN", "MAPBOX_TOKEN")
		h.openTab(tabAuto)
	})
	shot("settings-general", panelWidth, func(h *harness, tt *ui.Tester) { h.openTab(tabGeneral) })
	shot("shortcuts", panelWidth, func(h *harness, tt *ui.Tester) { h.cheat = true })
	shot("empty-search", panelWidth, func(h *harness, tt *ui.Tester) { h.query = "nothing-matches" })
}

func save(t *testing.T, path string, tt *ui.Tester) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

// TestContrast checks that every text color is WCAG AA (4.5:1) on every
// surface it sits on, light and dark.
func TestContrast(t *testing.T) {
	lum := func(hex string) float64 {
		var r, g, b int
		if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
			t.Fatal(hex, err)
		}
		ch := func(v int) float64 {
			c := float64(v) / 255
			if c <= 0.03928 {
				return c / 12.92
			}
			return math.Pow((c+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(r) + 0.7152*ch(g) + 0.0722*ch(b)
	}
	for dark, p := range colors {
		for _, fg := range []string{"text", "muted", "faint", "link", "gold", "green", "red", "danger"} {
			for _, bg := range []string{"bg", "card"} {
				l1, l2 := lum(p[fg]), lum(p[bg])
				if l1 < l2 {
					l1, l2 = l2, l1
				}
				if r := (l1 + 0.05) / (l2 + 0.05); r < 4.5 {
					t.Errorf("dark=%v: %s on %s is %.2f:1", dark, fg, bg, r)
				}
			}
		}
	}
}

func TestFilters(t *testing.T) {
	h := newHarness(t)
	tt := h.tester(panelWidth)
	// Filter tabs and shortcuts were removed from the main list; set the filter directly.
	h.setFilter(filterDue)
	if h.filter != filterDue {
		t.Fatalf("⌘2: filter %d", h.filter)
	}
	for _, k := range h.rows() {
		if st := h.inv.StatusOf(k, h.clock); st.State == vault.OK && st.Pending == 0 {
			t.Errorf("需处理 shows %s", k.Name)
		}
	}
	if n := len(h.rows()); n != 3 {
		t.Errorf("需处理: %d keys", n)
	}
	h.setFilter(filterDenied)
	for _, k := range h.rows() {
		if k.Agent != "deny" {
			t.Errorf("代理禁用 shows %s", k.Name)
		}
	}
	tt.Frame()
	h.setFilter(filterAll)
	if h.filter != filterAll || len(h.rows()) != len(h.inv.Keys) {
		t.Errorf("⌘1: filter %d, %d rows", h.filter, len(h.rows()))
	}
}

func TestConnectAgentsUI(t *testing.T) {
	h := newHarness(t)
	tt := h.tester(panelWidth)
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	tt.Key(ui.Cmd, ui.Key2)
	tt.Frame()
	if h.page != pageSettings || h.tab != tabAgents {
		t.Fatalf("page %v tab %v", h.page, h.tab)
	}
	states := map[string]agents.State{}
	for _, s := range h.agentState {
		states[s.ID] = s.State
	}
	if states["claude-code"] != agents.Connected || states["gemini"] != agents.Connected || states["cursor"] != agents.NotConnected ||
		states["opencode"] != agents.NotConnected || states["vscode"] != agents.Outdated || states["codex"] != agents.NotInstalled {
		t.Fatalf("%v", states)
	}
	missing := 0
	for _, s := range h.agentState {
		if s.State == agents.NotInstalled {
			missing++
		}
	}
	if !tt.HasText("连接 AI 代理") || !tt.HasText("已连接 · "+filepath.FromSlash("~/.claude.json")) || !tt.HasText("未连接 · "+filepath.FromSlash("~/.cursor/mcp.json")) ||
		!tt.HasText(fmt.Sprintf("未安装 %d", missing)) || tt.HasText("Codex") {
		t.Errorf("%q", tt.Texts())
	}
	// The agents not installed are folded; a click shows them.
	if err := tt.Click(fmt.Sprintf("未安装 %d", missing)); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Codex") || !tt.HasText("DeepSeek Harness") {
		t.Errorf("unfolded: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyEnter) // connect all: Cursor, opencode, VS Code
	tt.Frame()
	b, err := os.ReadFile(filepath.Join(h.agents.Home, ".cursor", "mcp.json"))
	if err != nil || !strings.Contains(string(b), `"`+h.exe+`"`) || !strings.Contains(string(b), `"cursor"`) {
		t.Errorf("cursor: %s %v", b, err)
	}
	b, _ = os.ReadFile(filepath.Join(h.agents.Home, ".config", "opencode", "opencode.jsonc"))
	if !strings.Contains(string(b), "// example") || !strings.Contains(string(b), `"type": "local"`) {
		t.Errorf("opencode: %s", b)
	}
	if !strings.Contains(h.toast, "3 个代理") {
		t.Errorf("toast %q", h.toast)
	}
	h.connectAgent(agents.All[0], false)
	if h.agentState[0].State != agents.NotConnected {
		t.Error("not disconnected")
	}
	b, _ = os.ReadFile(filepath.Join(h.agents.Home, ".claude.json"))
	if strings.Contains(string(b), "keybar") {
		t.Errorf("claude: %s", b)
	}
	if evs := vault.ReadLog(h.v.Dir, 0, func(e vault.Event) bool { return e.Action == "connect" || e.Action == "disconnect" }); len(evs) != 4 {
		t.Errorf("log %v", evs)
	}
}

func TestImportUI(t *testing.T) {
	h := newHarness(t)
	tt := h.tester(panelWidth)
	h.openImport()
	tt.Frame()
	if h.page != pageImport {
		t.Fatal("openImport")
	}
	path := demoEnvFile()
	defer os.RemoveAll(filepath.Dir(filepath.Dir(path)))
	h.imp.path = filepath.Dir(path) // a folder: its .env
	h.readImport()
	if h.imp.err != "" || h.imp.plan == nil {
		t.Fatal(h.imp.err)
	}
	tt.Frame()
	cs := h.imp.plan.Conflicts()
	if len(cs) != 2 || cs[0].Key != "OPENAI_API_KEY" || cs[1].Key != "RAILWAY_TOKEN" || cs[0].NewName != "OPENAI_API_KEY_MY_APP" {
		t.Fatalf("%+v", cs)
	}
	if !tt.HasText("同名但值不同 2") || !tt.HasText("出现 2 次，用第 7 行") || !tt.HasText("已有，值相同 · 跳过") || !tt.HasText("导入 1 个  "+hk("⌘↵")) {
		t.Errorf("%q", tt.Texts())
	}
	for _, v := range []string{"sk-example-openai-other", "example-railway-other", "sk-example-stripe-0002"} {
		if tt.HasText(v) {
			t.Errorf("shows a value %s", v)
		}
	}
	// Keyboard: → on the first makes it 覆盖, again 改名; ↓ then → on the
	// second: 覆盖.
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if cs[0].Choice != importer.Rename || cs[1].Choice != importer.Overwrite {
		t.Fatalf("choices %v %v", cs[0].Choice, cs[1].Choice)
	}
	if !tt.HasText("变量名不变，记在 keys.toml") || !tt.HasText("导入 3 个  "+hk("⌘↵")) {
		t.Errorf("%q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	h.run()
	if h.page != pageList || !strings.Contains(h.toast, "新增 1 个") || !strings.Contains(h.toast, "覆盖 1 个") || !strings.Contains(h.toast, "改名导入 1 个") {
		t.Errorf("page %v toast %q", h.page, h.toast)
	}
	val := func(n string) string { b, _ := h.v.Value(n); return string(b) }
	if val("OPENAI_API_KEY_MY_APP") != "sk-example-openai-other" || val("OPENAI_API_KEY") != "sk-example-openai-0000" ||
		val("RAILWAY_TOKEN") != "example-railway-other" || val("STRIPE_SECRET_KEY") != "sk-example-stripe-0002" {
		t.Error("values")
	}
	k, _ := h.key("RAILWAY_TOKEN")
	if len(k.History) != 2 || len(k.Pending()) != 3 {
		t.Errorf("overwrite is not a rotation: %+v", k.History)
	}
	if b, err := os.ReadFile(filepath.Join(filepath.Dir(path), "keys.toml")); err != nil || !strings.Contains(string(b), `OPENAI_API_KEY = "OPENAI_API_KEY_MY_APP"`) {
		t.Errorf("keys.toml %s %v", b, err)
	}
	noValues(t, h.v.Dir, "sk-example-openai-other", "example-railway-other")
}
