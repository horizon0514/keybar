// Package secret tells secrets from other values: by the provider's key
// format (sk-…, ghp_…), by the variable's name (…_API_KEY, …_TOKEN), and
// by the value's shape. It never keeps or prints a value; Mask shows one
// as its prefix and length.
package secret

import (
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Provider is a service whose keys have a recognizable format.
type Provider struct {
	Name string // OpenAI
	Var  string // the usual variable: OPENAI_API_KEY
	re   *regexp.Regexp
}

// Providers, the more specific first (sk-ant- before sk-).
var Providers = []Provider{
	{"Anthropic", "ANTHROPIC_API_KEY", regexp.MustCompile(`^sk-ant-[A-Za-z0-9_-]{32,}$`)},
	{"OpenRouter", "OPENROUTER_API_KEY", regexp.MustCompile(`^sk-or-(v1-)?[A-Za-z0-9]{32,}$`)},
	{"Stripe", "STRIPE_SECRET_KEY", regexp.MustCompile(`^(sk|rk)_(live|test)_[A-Za-z0-9]{20,}$`)},
	{"DeepSeek", "DEEPSEEK_API_KEY", regexp.MustCompile(`^sk-[0-9a-f]{32}$`)},
	{"OpenAI", "OPENAI_API_KEY", regexp.MustCompile(`^sk-(proj-|svcacct-|admin-|None-)?[A-Za-z0-9_-]{32,}$`)},
	{"GitHub", "GITHUB_TOKEN", regexp.MustCompile(`^(gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{50,})$`)},
	{"GitLab", "GITLAB_TOKEN", regexp.MustCompile(`^glpat-[A-Za-z0-9_-]{20,}$`)},
	{"Google", "GOOGLE_API_KEY", regexp.MustCompile(`^AIza[0-9A-Za-z_-]{35}$`)},
	{"AWS", "AWS_ACCESS_KEY_ID", regexp.MustCompile(`^(AKIA|ASIA)[0-9A-Z]{16}$`)},
	{"Slack", "SLACK_TOKEN", regexp.MustCompile(`^xox[abposr]-[0-9A-Za-z-]{10,}$|^xapp-[0-9A-Za-z-]{10,}$`)},
	{"Vercel", "VERCEL_TOKEN", regexp.MustCompile(`^vc[piarck]_[A-Za-z0-9]{20,64}$`)},
	{"Hugging Face", "HF_TOKEN", regexp.MustCompile(`^hf_[A-Za-z0-9]{30,}$`)},
	{"Groq", "GROQ_API_KEY", regexp.MustCompile(`^gsk_[A-Za-z0-9]{40,}$`)},
	{"xAI", "XAI_API_KEY", regexp.MustCompile(`^xai-[A-Za-z0-9]{40,}$`)},
	{"Perplexity", "PERPLEXITY_API_KEY", regexp.MustCompile(`^pplx-[A-Za-z0-9]{40,}$`)},
	{"Replicate", "REPLICATE_API_TOKEN", regexp.MustCompile(`^r8_[A-Za-z0-9]{30,}$`)},
	{"npm", "NPM_TOKEN", regexp.MustCompile(`^npm_[A-Za-z0-9]{36}$`)},
	{"Supabase", "SUPABASE_ACCESS_TOKEN", regexp.MustCompile(`^sbp_[a-f0-9]{40}$`)},
	{"SendGrid", "SENDGRID_API_KEY", regexp.MustCompile(`^SG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}$`)},
	{"Linear", "LINEAR_API_KEY", regexp.MustCompile(`^lin_api_[A-Za-z0-9]{32,}$`)},
	{"Notion", "NOTION_TOKEN", regexp.MustCompile(`^(ntn|secret)_[A-Za-z0-9]{40,}$`)},
	{"Fireworks", "FIREWORKS_API_KEY", regexp.MustCompile(`^fw_[A-Za-z0-9]{20,}$`)},
}

// Detect finds the provider of a value that is a key of a known format,
// and nothing else (no spaces, one line).
func Detect(value string) (Provider, bool) {
	v := strings.TrimSpace(value)
	if v == "" || len(v) > 300 || strings.ContainsAny(v, " \t\r\n") {
		return Provider{}, false
	}
	for _, p := range Providers {
		if p.re.MatchString(v) {
			return p, true
		}
	}
	return Provider{}, false
}

var (
	nameRE = regexp.MustCompile(`(?i)(^|_)(KEY|APIKEY|TOKEN|SECRET|SECRETS|PASSWORD|PASSWD|PWD|PASS|CREDENTIALS?|PRIVATE|AUTH|DSN|SALT|SIGNING|WEBHOOK|COOKIE|SESSION|CERT|PAT)($|_)|API_?KEY|ACCESS_?KEY|SECRET|TOKEN|PASSWORD`)
	urlRE  = regexp.MustCompile(`(?i)(_URL|_URI|_DSN|_CONN|_CONNECTION|_STRING)$|^DATABASE|^REDIS|^MONGO|^POSTGRES|^MYSQL|^AMQP`)
)

// plain are names that are configuration, not secrets: never asked for.
var plain = []string{
	"PORT", "HOST", "HOSTNAME", "NODE_ENV", "ENV", "APP_ENV", "RAILS_ENV", "RACK_ENV", "FLASK_ENV", "GO_ENV", "ENVIRONMENT", "STAGE",
	"DEBUG", "LOG_LEVEL", "LOGLEVEL", "LOG_FORMAT", "VERBOSE", "TZ", "LANG", "LC_ALL", "PATH", "HOME", "SHELL", "USER", "PWD",
	"NODE_OPTIONS", "PYTHONPATH", "PYTHONUNBUFFERED", "CI", "BASE_URL", "PUBLIC_URL", "APP_URL", "SITE_URL", "API_URL", "NEXT_PUBLIC_SITE_URL",
	"VITE_PORT", "BIND", "ADDR", "LISTEN", "TIMEOUT", "WORKERS", "CONCURRENCY", "REGION", "AWS_REGION", "AWS_DEFAULT_REGION",
	"NEXT_TELEMETRY_DISABLED", "DISABLE_TELEMETRY", "COLOR", "FORCE_COLOR", "NO_COLOR", "EDITOR", "PAGER", "TERM",
}

// Plain tells whether name is configuration, not a secret (PORT,
// NODE_ENV, DEBUG…): KeyBar does not ask for it.
func Plain(name string) bool {
	n := strings.ToUpper(name)
	if slices.Contains(plain, n) {
		return true
	}
	if strings.HasPrefix(n, "NEXT_PUBLIC_") || strings.HasPrefix(n, "EXPO_PUBLIC_") {
		return true // shipped to the browser: public by design
	}
	if SecretName(n) {
		return false
	}
	for _, suf := range []string{"_PORT", "_HOST", "_ENV", "_DEBUG", "_LOG_LEVEL", "_TIMEOUT", "_REGION", "_ENABLED", "_DISABLED", "_MODE", "_VERSION", "_PATH", "_DIR"} {
		if strings.HasSuffix(n, suf) {
			return true
		}
	}
	return false
}

// SecretName tells whether a variable's name says it holds a secret:
// …_API_KEY, …_TOKEN, …_SECRET, …PASSWORD.
func SecretName(name string) bool {
	n := strings.ToUpper(name)
	if strings.HasSuffix(n, "_KEY_ID") || strings.HasSuffix(n, "_PUBLIC_KEY") || strings.HasSuffix(n, "_PUBLISHABLE_KEY") {
		return n == "AWS_ACCESS_KEY_ID" // an id, but asked for with its secret
	}
	return nameRE.MatchString(n)
}

// CredentialURL tells whether a value is a URL with a password in it:
// postgres://user:pass@host/db.
func CredentialURL(value string) bool {
	v := strings.TrimSpace(value)
	if !strings.Contains(v, "://") {
		return false
	}
	u, err := url.Parse(v)
	if err != nil || u.User == nil {
		return false
	}
	p, ok := u.User.Password()
	return ok && p != ""
}

// Random tells whether a value looks generated: long, one word, mixed
// characters, high entropy.
func Random(value string) bool {
	v := strings.TrimSpace(value)
	if len(v) < 20 || len(v) > 500 || strings.ContainsAny(v, " \t\r\n") || strings.Contains(v, "://") || strings.Contains(v, "/") && !strings.ContainsAny(v, "+=") {
		return false
	}
	var lower, upper, digit int
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z':
			lower++
		case r >= 'A' && r <= 'Z':
			upper++
		case r >= '0' && r <= '9':
			digit++
		}
	}
	classes := 0
	for _, n := range []int{lower, upper, digit} {
		if n > 0 {
			classes++
		}
	}
	return classes >= 2 && digit > 0 && entropy(v) >= 3.5
}

func entropy(s string) float64 {
	counts := map[rune]float64{}
	for _, r := range s {
		counts[r]++
	}
	n := float64(utf8.RuneCountInString(s))
	var h float64
	for _, c := range counts {
		p := c / n
		h -= p * math.Log2(p)
	}
	return h
}

// Likely tells whether name=value is an obvious secret: a known provider
// shape, a URL with a password, or a *KEY*/*TOKEN*/*SECRET*/*PASSWORD*
// name with a real value. Random-looking values under ordinary names
// (PORT, HOST, FEATURE_X) are not — KeyBar is for a few API keys.
func Likely(name, value string) bool {
	v := strings.TrimSpace(value)
	if v == "" || Placeholder(v) {
		return false
	}
	if _, ok := Detect(v); ok {
		return true
	}
	if CredentialURL(v) {
		return true
	}
	if Plain(name) {
		return false
	}
	return SecretName(name) && len(v) >= 8 && !boolish(v)
}

func boolish(v string) bool {
	switch strings.ToLower(v) {
	case "true", "false", "yes", "no", "on", "off", "0", "1", "null", "none":
		return true
	}
	return false
}

// Placeholder tells whether a value is an example's: your-key-here,
// <token>, xxx, changeme.
func Placeholder(v string) bool {
	l := strings.ToLower(strings.TrimSpace(v))
	if strings.HasPrefix(l, "<") && strings.HasSuffix(l, ">") || strings.HasPrefix(l, "${") || strings.HasPrefix(l, "$(") {
		return true
	}
	for _, s := range []string{"your", "changeme", "change_me", "example", "placeholder", "replace", "todo", "xxxx", "****", "...", "<", "insert"} {
		if strings.Contains(l, s) {
			return true
		}
	}
	return strings.Trim(l, "x*-_. ") == ""
}

// Mask shows a value without it: its prefix (sk-, ghp_, postgres://…)
// and its length. Never more than the provider's prefix.
func Mask(value string) string {
	v := strings.TrimSpace(value)
	n := utf8.RuneCountInString(v)
	if n == 0 {
		return "（空）"
	}
	prefix := ""
	if i := strings.Index(v, "://"); i > 0 && i < 12 {
		prefix = v[:i+3]
	} else if m := prefixRE.FindString(v); m != "" && len(m) < n {
		prefix = m
	}
	if prefix == "" {
		return "•••• " + itoa(n) + " 位"
	}
	return prefix + "… " + itoa(n) + " 位"
}

var prefixRE = regexp.MustCompile(`^(sk-ant-|sk-or-|sk-proj-|sk-|sk_live_|sk_test_|rk_live_|gh[pousr]_|github_pat_|glpat-|AIza|AKIA|ASIA|xox[abposr]-|xapp-|vc[piarck]_|hf_|gsk_|xai-|pplx-|r8_|npm_|sbp_|SG\.|lin_api_|ntn_|secret_|fw_)`)

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// Fillable are the names worth asking a value for: the name itself looks
// like a secret (*KEY* / *TOKEN* / *SECRET* / *PASSWORD*…), and not in never.
// PORT、NODE_ENV、DATABASE_URL and the like are left out — use keys add.
func Fillable(names, never []string) []string {
	var out []string
	for _, n := range names {
		if !SecretName(n) || slices.Contains(never, n) || slices.Contains(out, n) {
			continue
		}
		out = append(out, n)
	}
	return out
}
