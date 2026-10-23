package importer

import (
	"os"
	"path/filepath"
	"strings"
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

func TestAttachEnvPlatforms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app", ".env")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte("OPENAI_API_KEY=dummy-attach-0001\nOTHER_TOKEN=dummy-attach-0002\n"), 0o600)
	v := &vault.Vault{Dir: t.TempDir(), Secrets: &vault.Memory{}, By: "test"}
	plan, err := Read(v, path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	r := plan.Apply(v)
	AttachEnvPlatforms(v, path, r)
	inv, _ := v.Inventory()
	k, ok := inv.Find("OPENAI_API_KEY")
	if !ok || len(k.Platforms) != 1 || k.Platforms[0].Kind != "env" || !k.Platforms[0].Auto() {
		t.Fatalf("%+v", k)
	}
	if !strings.Contains(k.Platforms[0].Target, ".env") {
		t.Error(k.Platforms[0].Target)
	}
	// rename: Var keeps file name
	_ = os.WriteFile(path, []byte("OPENAI_API_KEY=dummy-attach-9999\n"), 0o600)
	plan, _ = Read(v, path, "my_app", "")
	for _, it := range plan.Conflicts() {
		it.Choice = Rename
		it.NewName = "OPENAI_API_KEY_MY_APP"
	}
	r = plan.Apply(v)
	AttachEnvPlatforms(v, path, r)
	inv, _ = v.Inventory()
	k, ok = inv.Find("OPENAI_API_KEY_MY_APP")
	if !ok || len(k.Platforms) != 1 || k.Platforms[0].Var != "OPENAI_API_KEY" {
		t.Fatalf("rename plat %+v ok=%v", k, ok)
	}
	// second attach updates, does not duplicate
	AttachEnvPlatforms(v, path, r)
	inv, _ = v.Inventory()
	k, _ = inv.Find("OPENAI_API_KEY_MY_APP")
	if len(k.Platforms) != 1 {
		t.Fatalf("dup %d", len(k.Platforms))
	}
}
