package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"keybar/agents"
	"keybar/approval"
	"keybar/remote"
	"keybar/vault"
)

const (
	dummy1 = "dummy-value-one-1111"
	dummy2 = "dummy-value-two-2222"
)

type h struct {
	t        *testing.T
	env      *Env
	out, err bytes.Buffer
	tty      bool
	pw       []string // the passwords typed, in order
	asked    []approval.Request
	allow    bool
	ran      *exec.Cmd
	wd       string
	ghArgs   []string
	ghStdin  []byte
}

func newH(t *testing.T) *h {
	x := &h{t: t, wd: t.TempDir()}
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	v := &vault.Vault{Dir: t.TempDir(), Secrets: &vault.Memory{}, Now: func() time.Time { return now }}
	x.env = &Env{
		Vault: v, Stdout: &x.out, Stderr: &x.err, Version: "test",
		Terminal: func() bool { return x.tty },
		Password: func(string) ([]byte, error) {
			p := x.pw[0]
			x.pw = x.pw[1:]
			return []byte(p), nil
		},
		Gate: gateFunc(func(r approval.Request) approval.Decision {
			x.asked = append(x.asked, r)
			if x.allow {
				return approval.Decision{Allow: true}
			}
			return approval.Decision{Why: "denied"}
		}),
		Getwd: func() (string, error) { return x.wd, nil },
		Exec:  func(c *exec.Cmd) (int, error) { x.ran = c; return 0, nil },
		Syncer: &remote.Syncer{Token: v.Value, GH: func(_ context.Context, a []string, in []byte) error {
			x.ghArgs, x.ghStdin = a, in
			return nil
		}},
	}
	return x
}

type gateFunc func(approval.Request) approval.Decision

func (f gateFunc) Ask(r approval.Request) approval.Decision { return f(r) }

func (x *h) run(stdin string, args ...string) int {
	x.out.Reset()
	x.err.Reset()
	x.env.Stdin = strings.NewReader(stdin)
	return Main(x.env, args)
}

func (x *h) noValues() {
	x.t.Helper()
	_ = filepath.Walk(x.env.Vault.Dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			b, _ := os.ReadFile(p)
			if bytes.Contains(b, []byte(dummy1)) || bytes.Contains(b, []byte(dummy2)) {
				x.t.Errorf("%s holds a value", p)
			}
		}
		return nil
	})
}

func TestAddFromStdinAndTerminal(t *testing.T) {
	x := newH(t)
	if c := x.run(dummy1+"\n", "add", "OPENAI_API_KEY", "--desc", "OpenAI", "--platform", "github:o/r", "--tag", "ai"); c != 0 {
		t.Fatalf("%d %s", c, x.err.String())
	}
	if v, _ := x.env.Vault.Value("OPENAI_API_KEY"); string(v) != dummy1 {
		t.Errorf("value %q", v)
	}
	x.tty, x.pw = true, []string{dummy2, "different"}
	if c := x.run("", "add", "B"); c == 0 {
		t.Error("mismatched confirm accepted")
	}
	x.pw = []string{dummy2, dummy2}
	if c := x.run("", "add", "B"); c != 0 {
		t.Fatal(x.err.String())
	}
	x.tty = false
	if c := x.run("", "add", "C"); c == 0 || !strings.Contains(x.err.String(), "没有读到值") {
		t.Errorf("empty stdin: %d %s", c, x.err.String())
	}
	if c := x.run("x", "add", "OPENAI_API_KEY"); c == 0 {
		t.Error("added twice")
	}
	x.run("", "ls")
	if !strings.Contains(x.out.String(), "OPENAI_API_KEY") || strings.Contains(x.out.String(), dummy1) {
		t.Errorf("ls: %s", x.out.String())
	}
	x.run("", "ls", "--json")
	if strings.Contains(x.out.String(), dummy1) || !strings.Contains(x.out.String(), `"status": "ok"`) {
		t.Errorf("ls --json: %s", x.out.String())
	}
	x.run("", "show", "OPENAI_API_KEY")
	if !strings.Contains(x.out.String(), "[x] p1  GitHub Actions · o/r") {
		t.Errorf("show: %s", x.out.String())
	}
	x.noValues()
}

func TestGet(t *testing.T) {
	x := newH(t)
	x.run(dummy1, "add", "K")
	if c := x.run("", "get", "K"); c != 2 || strings.Contains(x.out.String(), dummy1) {
		t.Errorf("without --reveal: %d %q", c, x.out.String())
	}
	// Without a terminal (an agent's shell): it asks.
	if c := x.run("", "get", "K", "--reveal"); c == 0 || strings.Contains(x.out.String(), dummy1) || len(x.asked) != 1 {
		t.Errorf("denied: %d %q %d", c, x.out.String(), len(x.asked))
	}
	x.allow = true
	if c := x.run("", "get", "K", "--reveal"); c != 0 || x.out.String() != dummy1 {
		t.Errorf("allowed: %d %q", c, x.out.String())
	}
	x.tty = true
	if c := x.run("", "get", "K", "--reveal"); c != 0 || len(x.asked) != 2 {
		t.Errorf("terminal asked: %d", len(x.asked))
	}
	evs := vault.ReadLog(x.env.Vault.Dir, 0, nil)
	var ds []string
	for _, e := range evs {
		if e.Action == "reveal" {
			ds = append(ds, e.Decision)
		}
	}
	if strings.Join(ds, ",") != "denied,allowed-once,ok" {
		t.Errorf("log %v", ds)
	}
	x.noValues()
}

func TestRunInjects(t *testing.T) {
	x := newH(t)
	x.run(dummy1, "add", "OPENAI_API_KEY")
	x.run(dummy2, "add", "TIDB_URL")
	x.tty = true
	if c := x.run("", "run", "-e", "OPENAI_API_KEY", "-e", "DB=TIDB_URL", "--", "node", "app.js", "--flag"); c != 0 {
		t.Fatal(x.err.String())
	}
	env := x.ran.Env
	if !slices.Contains(env, "OPENAI_API_KEY="+dummy1) || !slices.Contains(env, "DB="+dummy2) {
		t.Error("not injected")
	}
	if strings.Join(x.ran.Args, " ") != "node app.js --flag" {
		t.Errorf("args %v", x.ran.Args)
	}
	// keys.toml, found from the working directory's parent.
	sub := filepath.Join(x.wd, "src")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.WriteFile(filepath.Join(x.wd, "keys.toml"), []byte("[env]\nDATABASE_URL = \"TIDB_URL\"\n"), 0o644)
	x.wd = sub
	if c := x.run("", "run", "--", "make"); c != 0 {
		t.Fatal(x.err.String())
	}
	if !slices.Contains(x.ran.Env, "DATABASE_URL="+dummy2) || slices.Contains(x.ran.Env, "OPENAI_API_KEY="+dummy1) {
		t.Error("keys.toml: wrong keys injected")
	}
	if c := x.run("", "run", "-e", "NOPE", "--", "x"); c == 0 {
		t.Error("missing key ran")
	}
	x.tty = false
	if c := x.run("", "run", "--", "make"); c == 0 || len(x.asked) != 1 || x.asked[0].Action != "run" {
		t.Errorf("non-interactive run without approval: %d %v", c, x.asked)
	}
	if !x.asked[0].Plain {
		t.Error("run without --redact: the request should say the output is not redacted")
	}
	x.run("", "run", "--redact", "--", "make")
	if len(x.asked) != 2 || x.asked[1].Plain {
		t.Errorf("run --redact: %+v", x.asked)
	}
	x.noValues()
}

func TestRunRedact(t *testing.T) {
	x := newH(t)
	x.run(dummy1, "add", "K")
	x.tty = true
	var out bytes.Buffer
	x.env.Exec = func(c *exec.Cmd) (int, error) {
		_, _ = io.WriteString(c.Stdout, "token is "+dummy1[:7])
		_, _ = io.WriteString(c.Stdout, dummy1[7:]+" done")
		return 0, nil
	}
	x.env.Stdout = &out
	if c := x.run("", "run", "--redact", "-e", "K", "--", "x"); c != 0 {
		t.Fatal(x.err.String())
	}
	if out.String() != "token is [REDACTED:K] done" {
		t.Errorf("%q", out.String())
	}
}

func TestImport(t *testing.T) {
	x := newH(t)
	path := filepath.Join(x.wd, ".env")
	_ = os.WriteFile(path, []byte("OPENAI_API_KEY="+dummy1+"\nDB_URL=\""+dummy2+"\"\nEMPTY=\n"), 0o600)
	if c := x.run("", "import", path, "--shred", "--project", "demo"); c != 0 {
		t.Fatal(x.err.String())
	}
	if v, _ := x.env.Vault.Value("DB_URL"); string(v) != dummy2 {
		t.Errorf("DB_URL %q", v)
	}
	// Names are the variables': no keys.toml needed.
	if _, err := os.Stat(filepath.Join(x.wd, "keys.toml")); err == nil {
		t.Error("wrote keys.toml with nothing renamed")
	}
	// EMPTY was skipped: the file stays.
	if _, err := os.Stat(path); err != nil {
		t.Error("removed with a variable not imported")
	}
	_ = os.WriteFile(path, []byte("OPENAI_API_KEY="+dummy1+"\n"), 0o600)
	if c := x.run("", "import", path, "--shred"); c != 0 {
		t.Fatal(x.err.String())
	}
	if !strings.Contains(x.out.String(), "已有相同值 1 个") {
		t.Errorf("same value not counted: %s", x.out.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error(".env not shredded")
	}
	x.noValues()
}

// importConflicts sets up KeyBar with A, B, C and a project's .env that
// has A (same value), B and C (other values), D (new) and D twice.
func importConflicts(t *testing.T) (*h, string) {
	x := newH(t)
	x.run("value-a-0000", "add", "A")
	x.run(dummy1, "add", "B", "--platform", "railway:api")
	x.run("value-c-0000", "add", "C")
	dir := filepath.Join(x.wd, "My App")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, ".env")
	_ = os.WriteFile(path, []byte("A=value-a-0000\nB="+dummy2+"\nD=first-d-0000\nC=other-c-1111\nD=last-d-2222\n"), 0o600)
	return x, path
}

func (x *h) value(name string) string {
	v, _ := x.env.Vault.Value(name)
	return string(v)
}

func (x *h) printedNoValues() {
	x.t.Helper()
	for _, v := range []string{dummy1, dummy2, "value-a-0000", "other-c-1111", "first-d-0000", "last-d-2222", "value-c-0000"} {
		if strings.Contains(x.out.String(), v) || strings.Contains(x.err.String(), v) {
			x.t.Errorf("printed a value %q", v)
		}
	}
}

func TestImportConflictsNonInteractive(t *testing.T) {
	x, path := importConflicts(t)
	// Without a terminal: conflicts are kept, and said so.
	if c := x.run("", "import", path); c != 0 {
		t.Fatal(x.err.String())
	}
	out := x.out.String()
	if !strings.Contains(out, "新增 1 个，已有相同值 1 个，保留现有 2 个") || !strings.Contains(out, "--on-conflict overwrite") {
		t.Errorf("summary: %s", out)
	}
	if !strings.Contains(x.err.String(), "D 在文件里出现了 2 次（第 3、5 行），用最后一次（第 5 行）") {
		t.Errorf("repeat warning: %s", x.err.String())
	}
	if x.value("D") != "last-d-2222" || x.value("B") != dummy1 || x.value("C") != "value-c-0000" {
		t.Error("wrong values kept")
	}
	if _, err := os.Stat(path); err != nil {
		t.Error(".env removed though values were not imported")
	}
	x.printedNoValues()

	// overwrite: B and C replaced, as rotations.
	if c := x.run("", "import", path, "--on-conflict=overwrite"); c != 0 {
		t.Fatal(x.err.String())
	}
	if x.value("B") != dummy2 || x.value("C") != "other-c-1111" {
		t.Error("not overwritten")
	}
	inv, _ := x.env.Vault.Inventory()
	b, _ := inv.Find("B")
	if len(b.History) != 1 || !strings.Contains(b.History[0].Note, "导入") || len(b.Pending()) != 1 {
		t.Errorf("not a rotation: %+v", b)
	}
	if !strings.Contains(x.out.String(), "覆盖 2 个") || !strings.Contains(x.out.String(), "keys show B") {
		t.Errorf("%s", x.out.String())
	}
	x.printedNoValues()
	x.noValues()
}

func TestImportRename(t *testing.T) {
	x, path := importConflicts(t)
	if c := x.run("", "import", path, "--on-conflict", "rename"); c != 0 {
		t.Fatal(x.err.String())
	}
	// NAME_PROJECT, the folder's name sanitized.
	if x.value("B_MY_APP") != dummy2 || x.value("C_MY_APP") != "other-c-1111" || x.value("B") != dummy1 {
		t.Error("not renamed")
	}
	toml, err := os.ReadFile(filepath.Join(filepath.Dir(path), "keys.toml"))
	if err != nil || !strings.Contains(string(toml), `B = "B_MY_APP"`) || strings.Contains(string(toml), `A = `) {
		t.Errorf("mapping: %s %v", toml, err)
	}
	// keys run in the project gives B the renamed key, A its own.
	x.tty = true
	x.wd = filepath.Dir(path)
	if c := x.run("", "run", "--", "env"); c != 0 {
		t.Fatal(x.err.String())
	}
	if !slices.Contains(x.ran.Env, "B="+dummy2) || !slices.Contains(x.ran.Env, "A=value-a-0000") {
		t.Errorf("run after rename: %v", x.err.String())
	}
	// Importing again: the renamed ones are the same now, and the
	// suggestion moves on to _2 for a third value.
	x.tty = false
	_ = os.WriteFile(path, []byte("B=third-b-3333\n"), 0o600)
	x.run("", "import", path, "--on-conflict", "rename")
	if x.value("B_MY_APP_2") != "third-b-3333" {
		t.Errorf("second rename: %s", x.out.String())
	}
	x.printedNoValues()
	x.noValues()
}

func TestImportInteractive(t *testing.T) {
	x, path := importConflicts(t)
	x.run("value-z-0000", "add", "Z")
	x.tty = true
	// B: rename, first to C (the file's), then Z (KeyBar's, another
	// value), then CUSTOM_B;
	// C: overwrite; then no to shredding.
	if c := x.run("r\nC\nZ\nCUSTOM_B\no\nn\n", "import", path); c != 0 {
		t.Fatal(x.err.String())
	}
	if x.value("CUSTOM_B") != dummy2 || x.value("C") != "other-c-1111" || x.value("B") != dummy1 {
		t.Errorf("choices not applied: %s %s", x.out.String(), x.err.String())
	}
	if !strings.Contains(x.err.String(), "改名导入为 B_MY_APP") || !strings.Contains(x.err.String(), "C 是这个文件里另一个变量要用的名字") || !strings.Contains(x.err.String(), "KeyBar 里已有 Z（值不同）") {
		t.Errorf("prompts: %s", x.err.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("shredded after no")
	}
	// The input ends: the rest are kept.
	_ = os.WriteFile(path, []byte("A=changed-a-4444\n"), 0o600)
	if c := x.run("", "import", path); c != 0 || x.value("A") != "value-a-0000" {
		t.Errorf("EOF: %d %s", c, x.err.String())
	}
	x.tty = false
	if c := x.run("", "import", path, "--on-conflict", "ask"); c != 2 {
		t.Error("ask without a terminal")
	}
	if c := x.run("", "import", path, "--on-conflict", "replace"); c != 2 {
		t.Error("bad --on-conflict accepted")
	}
	x.printedNoValues()
}

func TestRunAutoDetect(t *testing.T) {
	x := newH(t)
	x.run(dummy1, "add", "OPENAI_API_KEY")
	x.run(dummy2, "add", "UNRELATED")
	_ = os.MkdirAll(filepath.Join(x.wd, ".git"), 0o755)
	sub := filepath.Join(x.wd, "pkg", "api")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.WriteFile(filepath.Join(x.wd, ".env.example"), []byte("# names only\nOPENAI_API_KEY=your-key-here\nPORT=3000\n"), 0o644)
	x.wd = sub
	x.tty = true
	if c := x.run("", "run", "--", "node", "server.js"); c != 0 {
		t.Fatal(x.err.String())
	}
	if !slices.Contains(x.ran.Env, "OPENAI_API_KEY="+dummy1) || slices.Contains(x.ran.Env, "UNRELATED="+dummy2) {
		t.Error("wrong keys injected")
	}
	if slices.Contains(x.ran.Env, "OPENAI_API_KEY=your-key-here") || slices.Contains(x.ran.Env, "PORT=3000") {
		t.Error("used the example's values")
	}
	if e := x.err.String(); !strings.Contains(e, "注入 OPENAI_API_KEY") || !strings.Contains(e, "KeyBar 里没有 PORT，跳过") {
		t.Errorf("note: %q", e)
	}
	// Without a terminal it asks, for exactly those keys.
	x.tty = false
	x.run("", "run", "--", "node", "server.js")
	if len(x.asked) != 1 || strings.Join(x.asked[0].Keys, ",") != "OPENAI_API_KEY" {
		t.Errorf("asked %+v", x.asked)
	}
	// Nothing to inject: it runs, unasked, and says so.
	x.wd = t.TempDir()
	if c := x.run("", "run", "--", "make"); c != 0 || len(x.asked) != 1 || !strings.Contains(x.err.String(), "没找到要注入的密钥") {
		t.Errorf("empty: %d %s", c, x.err.String())
	}
	x.noValues()
}

func TestConnect(t *testing.T) {
	x := newH(t)
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".cursor"), 0o755)
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	x.env.Agents = &agents.Env{Home: home, Exe: "/opt/keybar/keybar", LookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	if c := x.run("", "connect"); c != 0 {
		t.Fatalf("%d %s %s", c, x.out.String(), x.err.String())
	}
	out := x.out.String()
	if !strings.Contains(out, "● Cursor") || !strings.Contains(out, "● Codex") || !strings.Contains(out, "  Claude Code  未安装") {
		t.Errorf("%s", out)
	}
	b, _ := os.ReadFile(filepath.Join(home, ".cursor", "mcp.json"))
	if !strings.Contains(string(b), `"/opt/keybar/keybar"`) {
		t.Errorf("cursor: %s", b)
	}
	x.run("", "connect", "--status")
	if !strings.Contains(x.out.String(), "● Codex        已连接") {
		t.Errorf("status: %s", x.out.String())
	}
	if c := x.run("", "connect", "claude"); c != 1 {
		t.Error("connected a missing agent")
	}
	if c := x.run("", "disconnect", "cursor"); c != 0 {
		t.Fatal(x.err.String())
	}
	b, _ = os.ReadFile(filepath.Join(home, ".cursor", "mcp.json"))
	if strings.Contains(string(b), "keybar") {
		t.Errorf("not disconnected: %s", b)
	}
	if c := x.run("", "connect", "vim"); c != 2 {
		t.Error("unknown agent")
	}
}

func TestRotateSyncChecklist(t *testing.T) {
	x := newH(t)
	x.run(dummy1, "add", "RAILWAY_TOKEN", "--platform", "github:o/r", "--platform", "railway:api", "--platform", "env:~/p/.env")
	if c := x.run(dummy1, "rotate", "RAILWAY_TOKEN"); c == 0 {
		t.Error("rotated to the same value")
	}
	if c := x.run(dummy2, "rotate", "RAILWAY_TOKEN", "--note", "incident"); c != 0 {
		t.Fatal(x.err.String())
	}
	out := x.out.String()
	if !strings.Contains(out, "✓ GitHub Actions · o/r 已自动更新") || !strings.Contains(out, "[ ] p2  Railway · api") || !strings.Contains(out, "[ ] p3  .env · ~/p/.env") {
		t.Errorf("%s", out)
	}
	if string(x.ghStdin) != dummy2 || slices.Contains(x.ghArgs, dummy2) {
		t.Error("gh not given the value on stdin")
	}
	x.run("", "done", "RAILWAY_TOKEN", "p2")
	if !strings.Contains(x.out.String(), "还要手动更新（1）") {
		t.Errorf("%s", x.out.String())
	}
	x.run("", "done", "RAILWAY_TOKEN", "all")
	if !strings.Contains(x.out.String(), "所有平台都已更新") {
		t.Errorf("%s", x.out.String())
	}
	x.run("", "show", "RAILWAY_TOKEN")
	if !strings.Contains(x.out.String(), "incident") {
		t.Errorf("history: %s", x.out.String())
	}
	x.run("", "audit")
	if !strings.Contains(x.out.String(), "轮换") || !strings.Contains(x.out.String(), "同步") {
		t.Errorf("audit: %s", x.out.String())
	}
	x.noValues()
}

func TestEditRmDoctorConfig(t *testing.T) {
	x := newH(t)
	x.run(dummy1, "add", "K", "--tag", "a")
	if c := x.run("", "edit", "K", "--desc", "new", "--untag", "a", "--platform", "vercel:app?env=production", "--agent", "deny", "--expires", "2026-10-25"); c != 0 {
		t.Fatal(x.err.String())
	}
	inv, _ := x.env.Vault.Inventory()
	k, _ := inv.Find("K")
	if k.Description != "new" || len(k.Tags) != 0 || len(k.Platforms) != 1 || k.Agent != "deny" || k.Expires.IsZero() {
		t.Errorf("%+v", k)
	}
	_ = os.WriteFile(filepath.Join(x.wd, ".env.local"), []byte("A=b\n"), 0o600)
	if c := x.run("", "doctor"); c != 1 || !strings.Contains(x.out.String(), ".env.local") || !strings.Contains(x.out.String(), "天内到期") {
		t.Errorf("doctor %d: %s", c, x.out.String())
	}
	if c := x.run("", "export"); c == 0 {
		t.Error("export ran")
	}
	if c := x.run("", "config", "cli-approval", "off"); c == 0 {
		t.Error("an agent's shell turned approval off")
	}
	x.tty = true
	if c := x.run("", "config", "cli-approval", "off"); c != 0 || vault.LoadSettings(x.env.Vault.Dir).CLIApproval {
		t.Error("not turned off from a terminal")
	}
	x.tty = false
	if c := x.run("", "rm", "K"); c == 0 {
		t.Error("removed without -y")
	}
	if c := x.run("", "rm", "K", "-y"); c != 0 {
		t.Fatal(x.err.String())
	}
	if c := x.run("", "nope"); c != 2 {
		t.Error("unknown command")
	}
}
