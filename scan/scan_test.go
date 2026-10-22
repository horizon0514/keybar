package scan

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"keybar/importer"
	"keybar/vault"
)

const dummyKey = "sk-proj-Zq8LmW2pT7vXc4Rn9KjH5sYd3FgB6tQeZq8LmW2p" // dummy

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestEnvFile(t *testing.T) {
	for n, want := range map[string]bool{".env": true, ".env.local": true, ".env.production": true, ".env.example": false,
		".env.sample": false, ".env.template": false, ".env.local.example": false, ".env.vault": false, ".envrc": false,
		"env": false, ".env.keybar-backup": false, ".env.development.local": true} {
		if EnvFile(n) != want {
			t.Errorf("%s: %v", n, !want)
		}
	}
}

func TestEnvFiles(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, "code/app/.env"), "A=1\n")
	write(t, filepath.Join(home, "code/app/.env.example"), "A=1\n")
	write(t, filepath.Join(home, "code/app/node_modules/x/.env"), "A=1\n")
	write(t, filepath.Join(home, "code/app/.git/.env"), "A=1\n")
	write(t, filepath.Join(home, "code/a/b/c/d/e/f/.env"), "A=1\n") // too deep
	write(t, filepath.Join(home, "code/mono/web/.env.local"), "A=1\n")
	write(t, filepath.Join(home, "side/.env"), "A=1\n")         // home, one down
	write(t, filepath.Join(home, "side/deep/.env"), "A=1\n")    // home, two down: not
	write(t, filepath.Join(home, "Library/x/.env"), "A=1\n")    // skipped
	write(t, filepath.Join(home, "code/ignored/.env"), "A=1\n") // ignored
	got := EnvFiles(Options{Home: home, Ignored: []string{filepath.Join(home, "code/ignored/.env")}})
	var rel []string
	for _, p := range got {
		r, _ := filepath.Rel(home, p)
		rel = append(rel, filepath.ToSlash(r))
	}
	want := []string{"code/app/.env", "code/mono/web/.env.local", "side/.env"}
	if !slices.Equal(rel, want) {
		t.Errorf("%v", rel)
	}
	// roots of one's own, ~ allowed
	got = EnvFiles(Options{Home: home, Roots: []string{"~/side"}})
	if len(got) != 2 {
		t.Errorf("%v", got)
	}
}

func TestParseRC(t *testing.T) {
	src := `# comment
export OPENAI_API_KEY="` + dummyKey + `"
export GITHUB_TOKEN='ghp_dummydummydummydummydummydummydumm'
export PATH="$HOME/bin:$PATH"
export FROM_CMD=$(security find-generic-password -w)
export BARE=plainvalue123 # trailing
alias ll='ls -l'
  export   SPACED=abc ; echo hi
EDITOR=vim
`
	vs := ParseRC([]byte(src), false)
	var names []string
	for _, v := range vs {
		names = append(names, v.Name)
	}
	if strings.Join(names, ",") != "OPENAI_API_KEY,GITHUB_TOKEN,BARE,SPACED" {
		t.Fatal(names)
	}
	if vs[0].Value != dummyKey || vs[0].Line != 2 || vs[2].Value != "plainvalue123" {
		t.Error("values")
	}
	fish := "set -gx HF_TOKEN hf_dummydummydummydummydummydummy12\nset -l LOCAL x\nset -Ux GROQ_API_KEY 'gsk_dummy'\n"
	fv := ParseRC([]byte(fish), true)
	if len(fv) != 2 || fv[0].Name != "HF_TOKEN" || fv[1].Value != "gsk_dummy" {
		t.Errorf("%+v", fv)
	}
}

func TestRemovePlaintextEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	orig := "PORT=3000\nOPENAI_API_KEY=" + dummyKey + "\nDB_PASSWORD=hunter2-dummy\n"
	write(t, p, orig)
	f, _ := ReadEnv(p)
	for mode, want := range map[Mode]string{
		Comment: "PORT=3000\n# OPENAI_API_KEY moved to KeyBar（keys run 注入）\n# DB_PASSWORD moved to KeyBar（keys run 注入）\n",
		Names:   "PORT=3000\nOPENAI_API_KEY=\nDB_PASSWORD=\n",
		Delete:  "PORT=3000\n",
	} {
		write(t, p, orig)
		backup, err := RemovePlaintext(p, DotEnv, LinesOf(f, []string{"OPENAI_API_KEY", "DB_PASSWORD"}), mode)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(p)
		if string(b) != want {
			t.Errorf("mode %d: %q", mode, b)
		}
		bb, _ := os.ReadFile(backup)
		if string(bb) != orig || backup != p+BackupSuffix {
			t.Error("backup")
		}
		st, _ := os.Stat(p)
		bs, _ := os.Stat(backup)
		if runtime.GOOS != "windows" && (st.Mode().Perm() != 0o640 || bs.Mode().Perm() != 0o600) { // no Unix modes there
			t.Errorf("modes %v %v", st.Mode(), bs.Mode())
		}
	}
	// changed since the scan: nothing touched
	write(t, p, "PORT=1\n")
	if _, err := RemovePlaintext(p, DotEnv, map[int]string{2: "X"}, Comment); err == nil {
		t.Error("changed file")
	}
}

func TestRemovePlaintextRC(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, ".zshrc")
	orig := "export PATH=/usr/bin\r\nexport OPENAI_API_KEY=\"" + dummyKey + "\"\r\nalias g=git\r\n"
	write(t, p, orig)
	f, ok := ReadRC(p)
	if !ok || f.Kind != ShellRC || f.Project != "zsh" {
		t.Fatalf("%+v", f)
	}
	backup, err := RemovePlaintext(p, ShellRC, LinesOf(f, []string{"OPENAI_API_KEY"}), Delete) // rc: always comment
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	want := "export PATH=/usr/bin\r\n# moved to KeyBar: OPENAI_API_KEY（值在钥匙串里，用 keys run 注入）\r\nalias g=git\r\n"
	if string(b) != want {
		t.Errorf("%q", b)
	}
	if strings.Contains(string(b), dummyKey) {
		t.Error("value left")
	}
	if bb, _ := os.ReadFile(backup); string(bb) != orig {
		t.Error("backup")
	}
}

func TestPlans(t *testing.T) {
	home := t.TempDir()
	v := &vault.Vault{Dir: t.TempDir(), Secrets: &vault.Memory{}}
	_ = v.Add(vault.Key{Name: "STRIPE_SECRET_KEY"}, []byte("sk_test_dummyOLDdummyOLDdummyOLD12"))
	write(t, filepath.Join(home, "code/a/.env"), "PORT=3000\nOPENAI_API_KEY="+dummyKey+"\nSTRIPE_SECRET_KEY=sk_test_dummyNEWdummyNEWdummyNEW12\n")
	write(t, filepath.Join(home, "code/b/.env"), "OPENAI_API_KEY="+dummyKey+"\nDB_PASSWORD=other-dummy-pw\n")
	write(t, filepath.Join(home, "code/c/.env"), "OPENAI_API_KEY=sk-proj-Other8LmW2pT7vXc4Rn9KjH5sYd3FgB6tQeZq8LmW\n")
	write(t, filepath.Join(home, ".zshrc"), "export HF_TOKEN=hf_dummydummydummydummydummydummy12\n")
	gs, err := Plans(v, Run(Options{Home: home}))
	if err != nil || len(gs) != 4 {
		t.Fatal(err, len(gs))
	}
	item := func(g int, name string) *importer.Item {
		for _, it := range gs[g].Plan.Items {
			if it.Var == name {
				return it
			}
		}
		t.Fatalf("%d %s", g, name)
		return nil
	}
	if it := item(0, "PORT"); !it.Skip || it.Likely {
		t.Error("PORT checked")
	}
	if it := item(0, "OPENAI_API_KEY"); it.Skip || it.State != importer.New || it.Masked != "sk-proj-… 48 位" {
		t.Errorf("%+v", it)
	}
	if it := item(0, "STRIPE_SECRET_KEY"); it.State != importer.Conflict || it.Choice != importer.Keep {
		t.Errorf("stripe %+v", it)
	}
	if it := item(1, "OPENAI_API_KEY"); it.State != importer.Same || it.Elsewhere == "" {
		t.Errorf("same elsewhere %+v", it)
	}
	if it := item(2, "OPENAI_API_KEY"); it.State != importer.Conflict || it.Choice != importer.Rename || it.NewName != "OPENAI_API_KEY_C" {
		t.Errorf("other elsewhere %+v", it)
	}
	if gs[3].Found.Kind != ShellRC || item(3, "HF_TOKEN").Skip {
		t.Error("rc")
	}
	if f, k := Checked(gs); f != 4 || k != 6 {
		t.Error(f, k)
	}
	// import in order: Recheck before each Apply
	for _, g := range gs {
		if err := g.Plan.Recheck(v); err != nil {
			t.Fatal(err)
		}
		g.Plan.Apply(v)
	}
	inv, _ := v.Inventory()
	for _, n := range []string{"OPENAI_API_KEY", "OPENAI_API_KEY_C", "DB_PASSWORD", "HF_TOKEN"} {
		if _, ok := inv.Find(n); !ok {
			t.Error("missing", n)
		}
	}
	if _, ok := inv.Find("PORT"); ok {
		t.Error("PORT imported")
	}
	if b, _ := v.Value("STRIPE_SECRET_KEY"); string(b) != "sk_test_dummyOLDdummyOLDdummyOLD12" {
		t.Error("kept")
	}
	if Unmanaged(gs[0].Plan) != 2 {
		t.Error(Unmanaged(gs[0].Plan))
	}
}

func TestWatcherSettles(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, "code/old/.env"), "A=1\n")
	now := time.Unix(1000, 0)
	w := NewWatcher(Options{Home: home})
	w.Now = func() time.Time { return now }
	if got := w.Tick(); got != nil {
		t.Fatal("baseline told", got)
	}
	p := filepath.Join(home, "code/new/.env")
	write(t, p, "TOKEN=dummy-token-1\n")
	if got := w.Tick(); len(got) != 0 {
		t.Fatal("told before settled", got)
	}
	// saved again within the settle time: waits more
	now = now.Add(2 * time.Second)
	write(t, p, "TOKEN=dummy-token-1\nSECRET=dummy-2\n")
	_ = os.Chtimes(p, now, now)
	if got := w.Tick(); len(got) != 0 {
		t.Fatal("told while changing", got)
	}
	now = now.Add(2 * time.Second)
	if got := w.Tick(); len(got) != 0 {
		t.Fatal("too soon", got)
	}
	now = now.Add(2 * time.Second)
	if got := w.Tick(); len(got) != 1 || got[0] != p {
		t.Fatal("not told", got)
	}
	now = now.Add(10 * time.Second)
	if got := w.Tick(); len(got) != 0 {
		t.Fatal("told twice", got)
	}
	// an old file modified, then ignored
	old := filepath.Join(home, "code/old/.env")
	_ = os.Chtimes(old, now, now)
	w.Ignore(old)
	w.Tick()
	now = now.Add(5 * time.Second)
	if got := w.Tick(); len(got) != 0 {
		t.Fatal("ignored told", got)
	}
}
