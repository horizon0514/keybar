package redact

import (
	"bytes"
	"encoding/base64"
	"math/rand"
	"strings"
	"testing"
)

func TestBytes(t *testing.T) {
	const v = "sk-dummy-0123456789abcdef"
	r := New(map[string][]byte{"OPENAI_API_KEY": []byte(v + "\n"), "SHORT": []byte("abc")})
	in := "key=" + v + " b64=" + base64.StdEncoding.EncodeToString([]byte(v)) + " url=" + "sk-dummy-0123456789abcdef" + " abc stays"
	out := r.String(in)
	if strings.Contains(out, v) || strings.Contains(out, base64.StdEncoding.EncodeToString([]byte(v))) {
		t.Errorf("leaked: %s", out)
	}
	if strings.Count(out, "[REDACTED:OPENAI_API_KEY]") != 3 || !strings.Contains(out, "abc stays") {
		t.Errorf("out %s", out)
	}
}

func TestOverlapping(t *testing.T) {
	r := New(map[string][]byte{"A": []byte("dummy-secret"), "B": []byte("dummy-secret-longer")})
	if out := r.String("x dummy-secret-longer y dummy-secret z"); out != "x [REDACTED:B] y [REDACTED:A] z" {
		t.Error(out)
	}
}

// TestWriter writes in chunks of every size: no value gets through split
// across writes.
func TestWriter(t *testing.T) {
	const v = "dummy-token-ZZZZ-9999"
	r := New(map[string][]byte{"T": []byte(v)})
	text := strings.Repeat("log line with "+v+" inside, and d-u-m-m-y noise dummy-tok\n", 40)
	want := r.String(text)
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 200; trial++ {
		var out bytes.Buffer
		w := r.Writer(&out)
		rest := []byte(text)
		for len(rest) > 0 {
			n := min(len(rest), 1+rng.Intn(30))
			if _, err := w.Write(rest[:n]); err != nil {
				t.Fatal(err)
			}
			rest = rest[n:]
		}
		_ = w.Close()
		if out.String() != want {
			t.Fatalf("trial %d: streamed output differs", trial)
		}
		if strings.Contains(out.String(), v) {
			t.Fatal("leaked")
		}
	}
}

func TestNone(t *testing.T) {
	r := New(nil)
	if r.String("hello") != "hello" {
		t.Error("changed")
	}
}
