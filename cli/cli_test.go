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
	b, err := os.ReadFile(filepath.Join(x.wd, "keys.toml"))
	if err != nil || !strings.Contains(string(b), `DB_URL = "DB_URL"`) || strings.Contains(string(b), dummy1) {
		t.Errorf("keys.toml %s %v", b, err)
	}
	// EMPTY was skipped: the file stays.
	if _, err := os.Stat(path); err != nil {
		t.Error("removed with a variable not imported")
	}
	_ = os.WriteFile(path, []byte("OPENAI_API_KEY="+dummy1+"\n"), 0o600)
	if c := x.run("", "import", path, "--shred"); c != 0 {
		t.Fatal(x.err.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error(".env not shredded")
	}
	x.noValues()
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
