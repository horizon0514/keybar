package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"keybar/approval"
	"keybar/scan"
	"keybar/vault"
)

// Dummy, key-shaped: never a real key.
const (
	dummyOpenAI = "sk-proj-Zq8LmW2pT7vXc4Rn9KjH5sYd3FgB6tQeZq8LmW2p"
	dummyGH     = "ghp_Zq8LmW2pT7vXc4Rn9KjH5sYd3FgB6tQeZq8L"
)

func TestScanCLI(t *testing.T) {
	x := newH(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := filepath.Join(home, "code", "my-app")
	_ = os.MkdirAll(app, 0o755)
	env := filepath.Join(app, ".env")
	_ = os.WriteFile(env, []byte("PORT=3000\nOPENAI_API_KEY="+dummyOpenAI+"\nDB_PASSWORD=hunter2-dummy\n"), 0o600)
	_ = os.WriteFile(filepath.Join(app, ".env.example"), []byte("OPENAI_API_KEY=your-key\n"), 0o644)
	rc := filepath.Join(home, ".zshrc")
	_ = os.WriteFile(rc, []byte("export PATH=/usr/bin:$PATH\nexport GITHUB_TOKEN="+dummyGH+"\n"), 0o644)

	if c := x.run("", "scan"); c != 0 {
		t.Fatalf("%d %s", c, x.err.String())
	}
	out := x.out.String()
	for _, want := range []string{filepath.FromSlash("~/code/my-app/.env"), "✓ OPENAI_API_KEY", "sk-proj-… 48 位", "· PORT", filepath.FromSlash("~/.zshrc"), "✓ GITHUB_TOKEN", "ghp_… 40 位", "打勾的 3 个（2 个文件）"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, dummyOpenAI) || strings.Contains(out, "hunter2") || strings.Contains(out, dummyGH) || strings.Contains(out, ".env.example") {
		t.Errorf("printed a value or an example:\n%s", out)
	}
	if inv, _ := x.env.Vault.Inventory(); len(inv.Keys) != 0 {
		t.Error("imported without --import")
	}

	// import the .env only, taking the values out (names kept)
	if c := x.run("", "scan", "--file", env, "--import", "--clean", "names"); c != 0 {
		t.Fatalf("%d %s %s", c, x.out.String(), x.err.String())
	}
	if v, _ := x.env.Vault.Value("OPENAI_API_KEY"); string(v) != dummyOpenAI {
		t.Error("not imported")
	}
	if inv, _ := x.env.Vault.Inventory(); slices.ContainsFunc(inv.Keys, func(k vault.Key) bool { return k.Name == "PORT" || k.Name == "GITHUB_TOKEN" }) {
		t.Error("imported unchecked or another file")
	}
	b, _ := os.ReadFile(env)
	if string(b) != "PORT=3000\nOPENAI_API_KEY=\nDB_PASSWORD=\n" {
		t.Errorf("cleaned: %q", b)
	}
	if bb, _ := os.ReadFile(env + scan.BackupSuffix); !strings.Contains(string(bb), dummyOpenAI) {
		t.Error("no backup")
	}
	if !strings.Contains(x.out.String(), "备份在 "+filepath.FromSlash("~/code/my-app/.env.keybar-backup")) {
		t.Error(x.out.String())
	}
	// again: the rc file, the .env now has nothing; the backup is not scanned
	if c := x.run("", "scan", "--import", "--clean", "comment"); c != 0 {
		t.Fatal(x.err.String())
	}
	b, _ = os.ReadFile(rc)
	if strings.Contains(string(b), dummyGH) || !strings.Contains(string(b), "# moved to KeyBar: GITHUB_TOKEN") || !strings.Contains(string(b), "export PATH=") {
		t.Errorf("rc: %q", b)
	}
	if strings.Contains(x.out.String(), "keybar-backup (") {
		t.Error("scanned a backup")
	}
	// the log has scan, import and clean, never a value
	var acts []string
	for _, ev := range vault.ReadLog(x.env.Vault.Dir, 0, nil) {
		acts = append(acts, ev.Action)
		if strings.Contains(ev.Detail, dummyOpenAI) {
			t.Error("value in log")
		}
	}
	for _, a := range []string{"scan", "import", "clean"} {
		if !slices.Contains(acts, a) {
			t.Errorf("no %s in %v", a, acts)
		}
	}
	if c := x.run("", "scan", "--clean", "names"); c != 2 {
		t.Error("--clean without --import")
	}
}

func TestRunFillTTY(t *testing.T) {
	x := newH(t)
	_ = os.MkdirAll(filepath.Join(x.wd, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(x.wd, ".env.example"), []byte("OPENAI_API_KEY=\nSTRIPE_SECRET_KEY=\nSENTRY_DSN=\nPORT=3000\nNODE_ENV=dev\n"), 0o644)
	x.tty = true
	x.pw = []string{dummyOpenAI, "", "-"} // save, skip, never ask
	if c := x.run("", "run", "--", "node", "app.js"); c != 0 {
		t.Fatal(x.err.String())
	}
	if len(x.pw) != 0 {
		t.Error("not asked for each", x.pw)
	}
	if !slices.Contains(x.ran.Env, "OPENAI_API_KEY="+dummyOpenAI) {
		t.Error("added key not injected")
	}
	if v, _ := x.env.Vault.Value("OPENAI_API_KEY"); string(v) != dummyOpenAI {
		t.Error("not saved")
	}
	e := x.err.String()
	if !strings.Contains(e, "已存入 OPENAI_API_KEY（sk-proj-… 48 位）") || strings.Contains(e, dummyOpenAI) || !strings.Contains(e, "不再询问 STRIPE_SECRET_KEY") {
		t.Errorf("%s", e)
	}
	// next time: SENTRY asked again, STRIPE not, PORT/NODE_ENV never
	x.pw = []string{""}
	x.run("", "run", "--", "node", "app.js")
	if len(x.pw) != 0 {
		t.Error("asks wrong")
	}
	if s := vault.LoadSettings(x.env.Vault.Dir); !slices.Contains(s.NeverAskIn(x.wd), "STRIPE_SECRET_KEY") {
		t.Errorf("%+v", s.NeverAsk)
	}
	x.noValues()
}

func TestRunFillAgent(t *testing.T) {
	x := newH(t)
	x.run(dummy1, "add", "OPENAI_API_KEY")
	_ = os.MkdirAll(filepath.Join(x.wd, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(x.wd, ".env.example"), []byte("OPENAI_API_KEY=\nGITHUB_TOKEN=\nPORT=1\n"), 0o644)
	// the app: the user pastes GITHUB_TOKEN into the approval
	x.env.Gate = gateFunc(func(r approval.Request) approval.Decision {
		x.asked = append(x.asked, r)
		_ = x.env.Vault.Add(vault.Key{Name: "GITHUB_TOKEN"}, []byte(dummy2))
		return approval.Decision{Allow: true, Added: []string{"GITHUB_TOKEN"}}
	})
	if c := x.run("", "run", "--", "make"); c != 0 {
		t.Fatal(x.err.String())
	}
	r := x.asked[0]
	if strings.Join(r.Keys, ",") != "OPENAI_API_KEY" || strings.Join(r.Missing, ",") != "GITHUB_TOKEN" || r.Project != x.wd {
		t.Errorf("%+v", r)
	}
	if !slices.Contains(x.ran.Env, "GITHUB_TOKEN="+dummy2) || !slices.Contains(x.ran.Env, "OPENAI_API_KEY="+dummy1) {
		t.Error("not injected")
	}
	// only missing names, nothing in KeyBar: still offered; declined, it runs bare
	x.wd = t.TempDir()
	_ = os.WriteFile(filepath.Join(x.wd, ".env.example"), []byte("ANTHROPIC_API_KEY=\n"), 0o644)
	x.asked = nil
	x.env.Gate = gateFunc(func(r approval.Request) approval.Decision {
		x.asked = append(x.asked, r)
		return approval.Decision{Why: "denied"}
	})
	if c := x.run("", "run", "--", "make"); c != 0 || len(x.asked) != 1 || len(x.asked[0].Keys) != 0 {
		t.Errorf("%d %+v %s", c, x.asked, x.err.String())
	}
}
