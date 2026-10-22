package runner

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestEnv(t *testing.T) {
	env := Env([]string{"PATH=/bin", "TOKEN=old", "OTHER=1"}, map[string][]byte{"TOKEN": []byte("dummy-new")})
	if !slices.Contains(env, "TOKEN=dummy-new") || slices.Contains(env, "TOKEN=old") || !slices.Contains(env, "OTHER=1") {
		t.Errorf("%v", env)
	}
}

func TestCaptureRedacts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	const v = "dummy-secret-7777-xyz"
	r := Capture(context.Background(), Shell(`echo "out:$MY_KEY"; echo "err:$MY_KEY" >&2; printf %s "$MY_KEY" | base64; exit 3`),
		map[string][]byte{"MY_KEY": []byte(v)}, map[string][]byte{"SVC_KEY": []byte(v)}, "", 10*time.Second, nil)
	if r.Exit != 3 || r.Err != nil {
		t.Fatalf("%+v", r)
	}
	if strings.Contains(r.Stdout+r.Stderr, v) || strings.Contains(r.Stdout, "ZHVtbXktc2VjcmV0LTc3NzctenL6") {
		t.Errorf("leaked: %q %q", r.Stdout, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "out:[REDACTED:SVC_KEY]") || !strings.Contains(r.Stderr, "err:[REDACTED:SVC_KEY]") {
		t.Errorf("%q %q", r.Stdout, r.Stderr)
	}
	if strings.Count(r.Stdout, "[REDACTED:SVC_KEY]") != 2 {
		t.Errorf("base64 not redacted: %q", r.Stdout)
	}
}

func TestCaptureOnlyInjected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	t.Setenv("KEYBAR_TEST_PARENT", "parent")
	r := Capture(context.Background(), Shell(`echo "[$A][$KEYBAR_TEST_PARENT]"`), map[string][]byte{"A": []byte("dummy-a")}, nil, "", 10*time.Second, nil)
	if strings.TrimSpace(r.Stdout) != "[[REDACTED:A]][parent]" {
		t.Errorf("%q", r.Stdout)
	}
}

func TestCaptureTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	r := Capture(context.Background(), []string{"sleep", "5"}, nil, nil, "", 200*time.Millisecond, nil)
	if !r.TimedOut {
		t.Errorf("%+v", r)
	}
	if r := Capture(context.Background(), []string{"/no/such/program"}, nil, nil, "", time.Second, nil); r.Err == nil {
		t.Error("started a missing program")
	}
}

func TestDisplay(t *testing.T) {
	got := Display([]string{"sh", "-c", "echo $X; it's", "", "a=b"})
	want := `sh -c 'echo $X; it'\''s' '' a=b`
	if got != want {
		t.Fatalf("Display = %q, want %q", got, want)
	}
}
