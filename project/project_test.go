package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFind(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	_ = os.MkdirAll(sub, 0o755)
	path := filepath.Join(root, FileName)
	if err := Write(path, "demo", map[string]string{"OPENAI_API_KEY": "OPENAI_API_KEY", "DATABASE_URL": "TIDB_PROD_URL"}); err != nil {
		t.Fatal(err)
	}
	p, ok := Find(sub)
	if !ok || p != path {
		t.Fatalf("find %q %v", p, ok)
	}
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.Project != "demo" || f.Env["DATABASE_URL"] != "TIDB_PROD_URL" || len(f.Keys()) != 2 {
		t.Errorf("%+v", f)
	}
}

func TestLoadRejectsValues(t *testing.T) {
	dir := t.TempDir()
	for _, body := range []string{
		"[env]\nOPENAI_API_KEY = \"sk-dummy123\"\n",
		"[env]\nDB = \"mysql://u:p@h/db\"\n",
		"[env]\n\"BAD NAME\" = \"X\"\n",
		"[env]\nX = \"\"\n",
	} {
		p := filepath.Join(dir, FileName)
		_ = os.WriteFile(p, []byte(body), 0o644)
		if _, err := Load(p); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
}

func TestDotenv(t *testing.T) {
	vars, err := ParseDotenv([]byte("# c\nexport A=1\nB=\"two words\\n\"\nC='x#y'\nD=val # comment\n\nE=\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "1", "B": "two words\n", "C": "x#y", "D": "val", "E": ""}
	if len(vars) != len(want) {
		t.Fatalf("%+v", vars)
	}
	for _, v := range vars {
		if want[v.Name] != v.Value {
			t.Errorf("%s = %q", v.Name, v.Value)
		}
	}
	if _, err := ParseDotenv([]byte("NOPE\n")); err == nil {
		t.Error("accepted a line without =")
	}
}

func TestFindDotenvsAndShred(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{".env", "app/.env.local", "app/.env.example", "node_modules/x/.env", "deep/1/2/3/4/5/6/.env"} {
		full := filepath.Join(root, p)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte("A=dummy\n"), 0o600)
	}
	got := FindDotenvs(root, 4)
	if len(got) != 2 {
		t.Errorf("found %v", got)
	}
	if err := Shred(filepath.Join(root, ".env")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".env")); !os.IsNotExist(err) {
		t.Error("not removed")
	}
}
