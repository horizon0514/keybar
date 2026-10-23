package project

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNames(t *testing.T) {
	got := Names([]byte("# c\nexport A=1\nB = \"x=y\"\n  C=\nnot a line\n9BAD=1\nA=again\nD\n"))
	if strings.Join(got, ",") != "A,B,C" {
		t.Errorf("%v", got)
	}
}

func TestDetectStopsAtGitRoot(t *testing.T) {
	outer := t.TempDir()
	_ = os.WriteFile(filepath.Join(outer, ".env"), []byte("OUTSIDE=1\n"), 0o600)
	repo := filepath.Join(outer, "repo")
	sub := filepath.Join(repo, "apps", "web")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: elsewhere\n"), 0o644) // a worktree's .git file
	_ = os.WriteFile(filepath.Join(repo, ".env.example"), []byte("OPENAI_API_KEY=\nPORT=3000\n"), 0o644)
	_ = os.WriteFile(filepath.Join(sub, ".env.sample"), []byte("DATABASE_URL=postgres://example\n"), 0o644)
	_ = os.WriteFile(filepath.Join(sub, ".env"), []byte("STRIPE_KEY=placeholder\nOPENAI_API_KEY=placeholder\n"), 0o600)
	names, files := Detect(sub)
	if strings.Join(names, ",") != "DATABASE_URL,STRIPE_KEY,OPENAI_API_KEY,PORT" {
		t.Errorf("names %v", names)
	}
	if len(files) != 3 {
		t.Errorf("files %v", files)
	}
	// Outside a repository: the folder alone.
	if n, _ := Detect(outer); strings.Join(n, ",") != "OUTSIDE" {
		t.Errorf("outside %v", n)
	}
	held := []string{"OPENAI_API_KEY", "DATABASE_URL", "TIDB_PROD_URL"}
	has := func(n string) bool { return slices.Contains(held, n) }
	r, err := Resolve(sub, has)
	if err != nil {
		t.Fatal(err)
	}
	// Held names inject even if not *KEY*-shaped; missing PORT is not offered.
	if len(r.Env) != 2 || r.Env["OPENAI_API_KEY"] != "OPENAI_API_KEY" || r.Env["DATABASE_URL"] != "DATABASE_URL" || strings.Join(r.Missing, ",") != "STRIPE_KEY" || r.Toml != "" {
		t.Errorf("%+v", r)
	}
	// keys.toml maps a variable to another key, and wins.
	_ = os.WriteFile(filepath.Join(repo, FileName), []byte("[env]\nDATABASE_URL = \"TIDB_PROD_URL\"\nEXTRA = \"NOT_HELD\"\n"), 0o644)
	r, err = Resolve(sub, has)
	if err != nil {
		t.Fatal(err)
	}
	if r.Env["DATABASE_URL"] != "TIDB_PROD_URL" || r.Env["OPENAI_API_KEY"] != "OPENAI_API_KEY" || strings.Join(r.TomlMissing, ",") != "NOT_HELD" {
		t.Errorf("%+v", r)
	}
	if strings.Join(r.Keys(), ",") != "NOT_HELD,OPENAI_API_KEY,TIDB_PROD_URL" {
		t.Errorf("keys %v", r.Keys())
	}
}
