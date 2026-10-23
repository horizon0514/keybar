package secret

import (
	"strings"
	"testing"
)

// Dummy shapes only: built from repeated filler, never a real key.
func fill(n int) string { return strings.Repeat("Ab3d", n/4+1)[:n] }

func TestDetect(t *testing.T) {
	cases := map[string]string{
		"sk-ant-api03-" + fill(80):             "ANTHROPIC_API_KEY",
		"sk-or-v1-" + strings.Repeat("a1", 32): "OPENROUTER_API_KEY",
		"sk-proj-" + fill(60):                  "OPENAI_API_KEY",
		"sk-" + fill(48):                       "OPENAI_API_KEY",
		"sk-" + strings.Repeat("0a", 16):       "DEEPSEEK_API_KEY",
		"ghp_" + fill(36):                      "GITHUB_TOKEN",
		"github_pat_" + fill(82):               "GITHUB_TOKEN",
		"gho_" + fill(36):                      "GITHUB_TOKEN",
		"AIza" + fill(35):                      "GOOGLE_API_KEY",
		"AKIA" + strings.Repeat("ABCD2345", 2): "AWS_ACCESS_KEY_ID",
		"sk_" + "live_" + fill(24):             "STRIPE_SECRET_KEY",
		"rk_" + "live_" + fill(24):             "STRIPE_SECRET_KEY",
		"xoxb-" + "1234567890-" + fill(24):     "SLACK_TOKEN",
		"vcp_" + fill(40):                      "VERCEL_TOKEN",
		"hf_" + fill(34):                       "HF_TOKEN",
		"gsk_" + fill(52):                      "GROQ_API_KEY",
		"xai-" + fill(60):                      "XAI_API_KEY",
	}
	for v, want := range cases {
		p, ok := Detect(v)
		if !ok {
			t.Errorf("%s: not detected", Mask(v))
			continue
		}
		if p.Var != want {
			t.Errorf("%s: %s (%s), want %s", Mask(v), p.Var, p.Name, want)
		}
	}
	for _, v := range []string{"", "hello world", "sk-short", "https://example.com", "ghp_short", fill(400), "sk-" + fill(40) + " x"} {
		if p, ok := Detect(v); ok {
			t.Errorf("%q detected as %s", v, p.Name)
		}
	}
}

func TestLikely(t *testing.T) {
	yes := [][2]string{
		{"OPENAI_API_KEY", "sk-proj-" + fill(40)},
		{"MY_SERVICE_TOKEN", "dummy-token-value"},
		{"DB_PASSWORD", "hunter2-dummy"},
		{"DATABASE_URL", "postgres://app:dummy-pass@localhost:5432/app"},
		{"SENTRY_DSN", "https://abc123:secret@o1.ingest.sentry.io/1"},
	}
	for _, c := range yes {
		if !Likely(c[0], c[1]) {
			t.Errorf("%s=%s: not likely", c[0], Mask(c[1]))
		}
	}
	no := [][2]string{
		{"PORT", "3000"}, {"NODE_ENV", "production"}, {"DEBUG", "true"}, {"API_KEY", "your-api-key-here"},
		{"SECRET_KEY", "changeme"}, {"TOKEN", "<token>"}, {"API_KEY", "${API_KEY}"}, {"DATABASE_URL", "postgres://localhost/app"},
		{"APP_URL", "https://example.com/some/long/path/abc123"}, {"NEXT_PUBLIC_API_KEY", "pk-" + fill(30)},
		{"STRIPE_PUBLISHABLE_KEY", "pk_test_" + fill(24)}, {"LOG_LEVEL", "info"}, {"API_KEY", ""}, {"ENABLE_KEY_ROTATION", "false"},
		{"AWS_REGION", "us-east-1"}, {"HOST", "0.0.0.0"},
		{"WHATEVER", "Zq8LmW2pT7vXc4Rn9KjH5sYd3FgB6tQe"}, // random alone is not enough
		{"FEATURE_FLAG", "Zq8LmW2pT7vXc4Rn9KjH5sYd3FgB6tQe"},
	}
	for _, c := range no {
		if Likely(c[0], c[1]) {
			t.Errorf("%s=%s: likely", c[0], Mask(c[1]))
		}
	}
	if !SecretName("AWS_ACCESS_KEY_ID") || SecretName("STRIPE_PUBLIC_KEY") {
		t.Error("key id / public key")
	}
}

func TestMask(t *testing.T) {
	v := "sk-proj-" + fill(40)
	m := Mask(v)
	if !strings.HasPrefix(m, "sk-proj-…") || !strings.Contains(m, "48 位") || strings.Contains(m, fill(6)) {
		t.Error(m)
	}
	if m := Mask("hunter2-dummy"); m != "•••• 13 位" {
		t.Error(m)
	}
	if m := Mask("postgres://app:dummy-pass@localhost/app"); strings.Contains(m, "dummy") || !strings.HasPrefix(m, "postgres://…") {
		t.Error(m)
	}
	if Mask("") != "（空）" {
		t.Error("empty")
	}
}

func TestFillable(t *testing.T) {
	got := Fillable([]string{"PORT", "OPENAI_API_KEY", "NODE_ENV", "DATABASE_URL", "OPENAI_API_KEY", "SKIPPED", "MY_TOKEN"}, []string{"SKIPPED"})
	if strings.Join(got, ",") != "OPENAI_API_KEY,MY_TOKEN" {
		t.Error(got) // DATABASE_URL: name is not *KEY*/*TOKEN*/*SECRET*…
	}
}
