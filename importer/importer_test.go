package importer

import (
	"os"
	"path/filepath"
	"testing"

	"keybar/vault"
)

func TestSanitizeSuggest(t *testing.T) {
	for in, want := range map[string]string{"my-app": "MY_APP", "My App 2": "MY_APP_2", "--x--": "X", "项目": "", "rz.system": "RZ_SYSTEM"} {
		if got := Sanitize(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
	v := &vault.Vault{Dir: t.TempDir(), Secrets: &vault.Memory{}}
	_ = v.Add(vault.Key{Name: "K_APP"}, []byte("dummy-1"))
	inv, _ := v.Inventory()
	if s := Suggest(inv, "K", "app", nil); s != "K_APP_2" {
		t.Error(s)
	}
	if s := Suggest(inv, "K", "app", []string{"K_APP_2"}); s != "K_APP_3" {
		t.Error(s)
	}
	if s := Suggest(inv, "K", "项目", nil); s != "K_IMPORTED" {
		t.Error(s)
	}
}

func TestPlanStates(t *testing.T) {
	v := &vault.Vault{Dir: t.TempDir(), Secrets: &vault.Memory{}}
	_ = v.Add(vault.Key{Name: "SAME"}, []byte("dummy-same"))
	_ = v.Add(vault.Key{Name: "DIFF"}, []byte("dummy-old"))
	dir := filepath.Join(t.TempDir(), "proj")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, ".env")
	_ = os.WriteFile(path, []byte("SAME=dummy-same\nDIFF=dummy-new\nNEW=dummy-n\nE=\nNEW=dummy-n2\n"), 0o600)
	p, err := Read(v, path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]State{"SAME": Same, "DIFF": Conflict, "NEW": New, "E": Empty}
	for _, it := range p.Items {
		if it.State != want[it.Var] {
			t.Errorf("%s: %v", it.Var, it.State)
		}
	}
	if len(p.Items) != 4 || len(p.Repeated()) != 1 || p.Repeated()[0].Line != 5 || p.Conflicts()[0].NewName != "DIFF_PROJ" {
		t.Errorf("%+v", p.Items)
	}
	p.Conflicts()[0].Choice = Rename
	r := p.Apply(v)
	if r.Added != 1 || r.Same != 1 || r.Renamed != 1 || r.Empty != 1 || r.Mapped["DIFF"] != "DIFF_PROJ" || r.Complete() {
		t.Errorf("%+v", r)
	}
	if b, _ := v.Value("NEW"); string(b) != "dummy-n2" {
		t.Error("not the last value")
	}
	if b, _ := v.Value("DIFF"); string(b) != "dummy-old" {
		t.Error("changed the existing key")
	}
}
