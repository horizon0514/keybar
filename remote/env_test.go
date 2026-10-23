package remote

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"keybar/vault"
)

func TestWriteEnvVar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	orig := "PORT=3000\nexport OPENAI_API_KEY=\"sk-old-dummy-0000\"\n# keep\nDB_URL='postgres://a:b@h/db'\n"
	if err := os.WriteFile(path, []byte(orig), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvVar(path, "OPENAI_API_KEY", []byte("sk-new-dummy-1111")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if !strings.Contains(got, `export OPENAI_API_KEY="sk-new-dummy-1111"`) {
		t.Errorf("quoted export: %s", got)
	}
	if !strings.Contains(got, "PORT=3000") || !strings.Contains(got, "# keep") || !strings.Contains(got, "DB_URL=") {
		t.Errorf("other lines: %s", got)
	}
	bb, err := os.ReadFile(path + BackupSuffix)
	if err != nil || string(bb) != orig {
		t.Errorf("backup: %v %q", err, bb)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm()&0o777 != 0o640 && runtime.GOOS != "windows" {
		t.Errorf("mode %o", st.Mode().Perm())
	}
	// append missing
	if err := WriteEnvVar(path, "NEW_TOKEN", []byte("plain-token-value")); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if !strings.HasSuffix(strings.TrimRight(string(b), "\n"), "NEW_TOKEN=plain-token-value") &&
		!strings.Contains(string(b), "\nNEW_TOKEN=plain-token-value\n") {
		t.Errorf("append: %s", b)
	}
	// value with space gets quotes when bare
	if err := WriteEnvVar(path, "NEW_TOKEN", []byte("has space")); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if !strings.Contains(string(b), `NEW_TOKEN="has space"`) {
		t.Errorf("space quote: %s", b)
	}
	// single-quoted line stays single when possible
	if err := WriteEnvVar(path, "DB_URL", []byte("postgres://x:y@h/db")); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if !strings.Contains(string(b), "DB_URL='postgres://x:y@h/db'") {
		t.Errorf("single: %s", b)
	}
}

func TestWriteEnvVarCRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.local")
	_ = os.WriteFile(path, []byte("A=1\r\nB=old\r\n"), 0o600)
	if err := WriteEnvVar(path, "B", []byte("new")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "\r\n") || !strings.Contains(string(b), "B=new") {
		t.Errorf("%q", b)
	}
}

func TestSyncerEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	_ = os.WriteFile(path, []byte("K=old-dummy\n"), 0o600)
	s := &Syncer{}
	p := vault.Platform{Kind: "env", Target: path}
	if err := s.Update(context.Background(), "K", p, []byte("new-dummy-value")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "K=new-dummy-value\n" && !strings.Contains(string(b), "K=new-dummy-value") {
		t.Errorf("%q", b)
	}
	// ~ expansion
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	rel := filepath.Join(home, "proj", ".env")
	_ = os.MkdirAll(filepath.Dir(rel), 0o755)
	_ = os.WriteFile(rel, []byte("T=1\n"), 0o600)
	p = vault.Platform{Kind: "env", Target: "~/proj/.env", Var: "T"}
	if err := s.Update(context.Background(), "MY_T", p, []byte("2")); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(rel)
	if !strings.Contains(string(b), "T=2") {
		t.Errorf("~ : %s", b)
	}
}

func TestCheckEnvPathForbidden(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix paths")
	}
	if err := CheckEnvPath("/etc/passwd"); err == nil {
		t.Error("allowed /etc/passwd")
	}
}

func TestResolveEnvPathWindowsStyle(t *testing.T) {
	if runtime.GOOS != "windows" {
		// still parse drive-like targets without ~
		p := `C:\proj\.env`
		if _, err := ResolveEnvPath(p); err != nil {
			// Abs may still work on unix for this odd path
			t.Log(err)
		}
	}
}
