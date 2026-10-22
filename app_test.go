package main

import (
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"keybar/approval"
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
	if !tt.HasText("RAILWAY_TOKEN") || !tt.HasText("1 个已超期 · 1 个快到期 · 1 个轮换后还有平台没更新") {
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
	tt.Key(0, ui.KeyEnter)
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
	h.form = form{name: "NEW_KEY", value: "dummy-new-0000", desc: "test", platforms: "github:o/r\nrailway:api", days: "30", projects: "a, b"}
	h.submit()
	if h.form.err != "" {
		t.Fatal(h.form.err)
	}
	k, ok := h.key("NEW_KEY")
	if !ok || len(k.Platforms) != 2 || k.RotateDays != 30 || len(k.Projects) != 2 || h.page != pageDetail {
		t.Errorf("%+v", k)
	}
	h.openAdd()
	h.form = form{name: "bad name", value: "x"}
	h.submit()
	if h.form.err == "" {
		t.Error("bad name accepted")
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
			save(t, filepath.Join("shots", name+"-"+suffix+".png"), tt)
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
	shot("settings-general", panelWidth, func(h *harness, tt *ui.Tester) { h.openTab(tabGeneral) })
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
