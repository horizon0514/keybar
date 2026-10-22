package approval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGrants(t *testing.T) {
	g := &Grants{}
	now := time.Now()
	g.Add("mcp:a", []string{"K1", "K2"}, now.Add(time.Minute))
	switch {
	case !g.Covers("mcp:a", []string{"K1"}, now):
		t.Error("K1 not covered")
	case g.Covers("mcp:a", []string{"K1", "K3"}, now):
		t.Error("K3 covered")
	case g.Covers("mcp:b", []string{"K1"}, now):
		t.Error("another client covered")
	case g.Covers("mcp:a", []string{"K1"}, now.Add(2*time.Minute)):
		t.Error("expired grant covered")
	case g.Covers("mcp:a", nil, now):
		t.Error("nothing covered")
	}
	if len(g.List(now)) != 2 {
		t.Error(g.List(now))
	}
	g.Revoke("K1")
	if g.Covers("mcp:a", []string{"K1"}, now) {
		t.Error("revoked")
	}
}

func TestIPC(t *testing.T) {
	dir := t.TempDir()
	got := make(chan Request, 1)
	s, err := Listen(dir, func(r Request) Decision {
		got <- r
		return Decision{Allow: true, For: 15 * time.Minute}
	})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(filepath.Join(dir, ipcFile))
	if st.Mode().Perm()&0o077 != 0 {
		t.Errorf("ipc.json mode %v", st.Mode())
	}
	d, err := Client{Dir: dir}.Approve(Request{Client: "mcp:test", Action: "run", Keys: []string{"K"}})
	if err != nil || !d.Allow || d.For != 15*time.Minute {
		t.Fatalf("%+v %v", d, err)
	}
	if r := <-got; r.Client != "mcp:test" {
		t.Error(r)
	}
	// A wrong token is no app.
	b, _ := os.ReadFile(filepath.Join(dir, ipcFile))
	_ = os.WriteFile(filepath.Join(dir, ipcFile), []byte(strings.Replace(string(b), `"token":"`, `"token":"x`, 1)), 0o600)
	if _, err := (Client{Dir: dir}).Approve(Request{}); !errors.Is(err, ErrNoApp) {
		t.Errorf("bad token: %v", err)
	}
	_ = s.Close()
	if _, err := (Client{Dir: dir}).Approve(Request{}); !errors.Is(err, ErrNoApp) {
		t.Errorf("closed: %v", err)
	}
}

type fixed struct {
	d   Decision
	err error
	n   int
}

func (f *fixed) Approve(Request) (Decision, error) { f.n++; return f.d, f.err }

func TestGate(t *testing.T) {
	app := &fixed{err: ErrNoApp}
	dlg := &fixed{d: Decision{Allow: true, For: time.Minute}}
	g := &Gate{App: app, Dialog: dlg}
	r := Request{Client: "mcp:x", Keys: []string{"K"}}
	if d := g.Ask(r); !d.Allow || dlg.n != 1 {
		t.Fatalf("%+v", d)
	}
	if d := g.Ask(r); !d.Granted || dlg.n != 1 { // the grant answers
		t.Fatalf("second: %+v", d)
	}
	g2 := &Gate{App: &fixed{err: ErrNoApp}, NoDialog: true}
	if d := g2.Ask(r); d.Allow || d.Why != "no-approver" {
		t.Errorf("no approver: %+v", d)
	}
	g3 := &Gate{App: &fixed{err: ErrNoApp}, Dialog: &fixed{err: ErrNoDialog}}
	if d := g3.Ask(r); d.Allow {
		t.Error("allowed without a dialog")
	}
	g4 := &Gate{App: &fixed{d: Decision{Why: "denied"}}, Dialog: dlg}
	if d := g4.Ask(r); d.Allow || d.Code() != "denied" {
		t.Errorf("%+v", d)
	}
}

func TestDialogZenity(t *testing.T) {
	if os.Getenv("GOOS") != "" {
		t.Skip()
	}
	cases := []struct {
		out  string
		code int
		want string
	}{
		{"", 0, "allowed-once"},
		{"允许 15 分钟\n", 1, "allowed-15m"},
		{"", 1, "denied"},
		{"", 5, "timeout"},
	}
	for _, c := range cases {
		d := Dialog{run: func(_ context.Context, name string, args ...string) (string, int, error) {
			if name != "zenity" && name != "osascript" && name != "powershell" {
				t.Errorf("ran %s", name)
			}
			return c.out, c.code, nil
		}}
		got, err := d.Approve(Request{Client: "mcp:claude-code", Action: "run", Keys: []string{"K"}, Command: "npm test"})
		if err != nil {
			t.Fatal(err)
		}
		if goosIsLinux() && got.Code() != c.want {
			t.Errorf("%q %d: %s, want %s", c.out, c.code, got.Code(), c.want)
		}
	}
}

func TestSummary(t *testing.T) {
	r := Request{Client: "mcp:claude-code", Action: "reveal", Keys: []string{"A", "B"}}
	if s := r.Summary(); s != "Claude Code 请求读取 A、B 的明文值" {
		t.Error(s)
	}
}

func TestSummarySep(t *testing.T) {
	if got := (Request{Client: "cli", Action: "reveal", Keys: []string{"K"}}).Summary(); got != "命令行请求读取 K 的明文值" {
		t.Errorf("%q", got)
	}
	if got := (Request{Client: "mcp:cursor", Action: "run", Keys: []string{"K"}, Command: "make", Plain: true}).Summary(); !strings.Contains(got, " 请求用 K 运行（输出不脱敏）：make") {
		t.Errorf("%q", got)
	}
}
