package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

func newTest(t *testing.T) (*Vault, *Memory, *time.Time) {
	t.Helper()
	now := t0
	m := &Memory{}
	return &Vault{Dir: t.TempDir(), Secrets: m, Now: func() time.Time { return now }, By: "test"}, m, &now
}

// noValues fails if any file of dir holds one of the values.
func noValues(t *testing.T, dir string, values ...string) {
	t.Helper()
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, v := range values {
			if strings.Contains(string(b), v) {
				t.Errorf("%s holds a value", p)
			}
		}
		return nil
	})
}

func TestAddRotateChecklist(t *testing.T) {
	v, m, now := newTest(t)
	const val, val2 = "dummy-value-aaaa-1111", "dummy-value-bbbb-2222"
	k := Key{Name: "RAILWAY_TOKEN", Description: "deploy", Platforms: []Platform{{Kind: "github", Target: "o/r"}, {Kind: "railway", Target: "api"}}}
	if err := v.Add(k, []byte(val)); err != nil {
		t.Fatal(err)
	}
	if err := v.Add(k, []byte(val)); err == nil {
		t.Error("added twice")
	}
	if got, _ := v.Value("RAILWAY_TOKEN"); string(got) != val {
		t.Errorf("value %q", got)
	}
	inv, _ := v.Inventory()
	kk, _ := inv.Find("RAILWAY_TOKEN")
	if len(kk.Pending()) != 0 || kk.Platforms[0].ID != "p1" || kk.Platforms[1].ID != "p2" {
		t.Errorf("platforms %+v", kk.Platforms)
	}
	*now = now.Add(time.Hour)
	if _, err := v.Rotate("RAILWAY_TOKEN", []byte(val), ""); err == nil {
		t.Error("rotated to the same value")
	}
	nk, err := v.Rotate("RAILWAY_TOKEN", []byte(val2), "incident")
	if err != nil {
		t.Fatal(err)
	}
	if len(nk.Pending()) != 2 || len(nk.History) != 1 || nk.History[0].Note != "incident" {
		t.Errorf("after rotate %+v", nk)
	}
	if err := v.MarkUpdated("RAILWAY_TOKEN", "railway"); err != nil { // by kind
		t.Fatal(err)
	}
	inv, _ = v.Inventory()
	kk, _ = inv.Find("RAILWAY_TOKEN")
	if p := kk.Pending(); len(p) != 1 || p[0].Kind != "github" {
		t.Errorf("pending %+v", p)
	}
	_ = v.MarkUpdated("RAILWAY_TOKEN", "all")
	inv, _ = v.Inventory()
	kk, _ = inv.Find("RAILWAY_TOKEN")
	if len(kk.Pending()) != 0 {
		t.Error("all not done")
	}
	noValues(t, v.Dir, val, val2)
	if err := v.Remove("RAILWAY_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if m.Len() != 0 {
		t.Error("value left in the store")
	}
	if _, err := v.Value("RAILWAY_TOKEN"); !errors.Is(err, ErrNotFound) {
		t.Errorf("value after rm: %v", err)
	}
}

func TestChecks(t *testing.T) {
	v, m, _ := newTest(t)
	for _, n := range []string{"", "1ABC", "A B", "A=B", strings.Repeat("A", 200)} {
		if err := v.Add(Key{Name: n}, []byte("x-value")); err == nil {
			t.Errorf("name %q accepted", n)
		}
	}
	if err := v.Add(Key{Name: "A"}, []byte("  \n")); err == nil {
		t.Error("empty value accepted")
	}
	if err := v.Add(Key{Name: "A"}, make([]byte, MaxValueSize+1)); err == nil {
		t.Error("big value accepted")
	}
	m.Fail = errors.ErrUnsupported
	if err := v.Add(Key{Name: "A"}, []byte("dummy")); err == nil || !strings.Contains(err.Error(), "Secret Service") {
		t.Errorf("no store: %v", err)
	}
	if inv, _ := v.Inventory(); len(inv.Keys) != 0 {
		t.Error("a key without a value was kept")
	}
	if err := v.Check(); err == nil {
		t.Error("check passed without a store")
	}
}

func TestStatus(t *testing.T) {
	inv := &Inventory{}
	now := t0
	day := 24 * time.Hour
	cases := []struct {
		k    Key
		want State
	}{
		{Key{LastRotated: now.Add(-10 * day)}, OK},
		{Key{LastRotated: now.Add(-80 * day)}, DueSoon},
		{Key{LastRotated: now.Add(-100 * day)}, Overdue},
		{Key{LastRotated: now.Add(-100 * day), RotateDays: -1}, OK},
		{Key{LastRotated: now.Add(-10 * day), RotateDays: 7}, Overdue},
		{Key{LastRotated: now, Expires: now.Add(5 * day)}, DueSoon},
		{Key{LastRotated: now, Expires: now.Add(-day)}, Overdue},
		{Key{Created: now.Add(-100 * day)}, Overdue},
	}
	for i, c := range cases {
		if st := inv.StatusOf(c.k, now); st.State != c.want {
			t.Errorf("%d: %v (%s), want %v", i, st.State, st.Why, c.want)
		}
	}
	inv.RotateDays = 365
	if st := inv.StatusOf(Key{LastRotated: now.Add(-100 * day)}, now); st.State != OK {
		t.Errorf("default 365: %v", st.State)
	}
}

func TestParsePlatform(t *testing.T) {
	p, err := ParsePlatform("vercel:my-app:DB_URL?env=production,preview&team=t_1&token=VT")
	if err != nil || p.Kind != "vercel" || p.Target != "my-app" || p.Var != "DB_URL" || len(p.Envs) != 2 || p.Team != "t_1" || p.TokenKey != "VT" {
		t.Errorf("%+v %v", p, err)
	}
	if p, _ := ParsePlatform(`env:C:\proj\.env`); p.Target != `C:\proj\.env` {
		t.Errorf("env path %+v", p)
	}
	for _, s := range []string{"", "github:norepo", "vercel:x?bad=1"} {
		if _, err := ParsePlatform(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestConcurrentUpdates(t *testing.T) {
	v, _, _ := newTest(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := "K" + string(rune('A'+i))
			if err := v.Add(Key{Name: name}, []byte("dummy-"+name)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	inv, _ := v.Inventory()
	if len(inv.Keys) != 20 {
		t.Errorf("%d keys, want 20: a write was lost", len(inv.Keys))
	}
}

func TestAudit(t *testing.T) {
	v, _, now := newTest(t)
	for i := 0; i < 5; i++ {
		*now = now.Add(time.Minute)
		_ = v.Log(Event{Client: "mcp:test", Action: "run", Keys: []string{"A", "B"}[i%2 : i%2+1], Decision: "allowed-once"})
	}
	if evs := ReadLog(v.Dir, 0, nil); len(evs) != 5 {
		t.Errorf("%d events", len(evs))
	}
	if evs := ReadLog(v.Dir, 2, ForKey("A")); len(evs) != 2 || evs[1].Keys[0] != "A" {
		t.Errorf("%+v", evs)
	}
	if DecisionZh("allowed-15m") != "允许 15 分钟" {
		t.Error(DecisionZh("allowed-15m"))
	}
}

func TestSettings(t *testing.T) {
	dir := t.TempDir()
	s := LoadSettings(dir)
	if !s.Remind || !s.CLIApproval || s.GrantMinutes != 15 {
		t.Errorf("defaults %+v", s)
	}
	s.GrantMinutes = 60
	_ = SaveSettings(dir, s)
	if LoadSettings(dir).GrantMinutes != 60 {
		t.Error("not saved")
	}
	st, _ := os.Stat(filepath.Join(dir, "settings.json"))
	if st.Mode().Perm()&0o077 != 0 {
		t.Errorf("mode %v", st.Mode())
	}
}
