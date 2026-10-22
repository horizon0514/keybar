package main

import (
	"os"
	"path/filepath"
	"time"

	"keybar/agents"
	"keybar/approval"
	"keybar/vault"
)

// demoNow is the example data's today.
var demoNow = time.Date(2026, 10, 22, 9, 30, 0, 0, time.Local)

// demoVault is the example data, in memory and a temporary folder: never
// the user's keychain.
func demoVault() *vault.Vault {
	dir, _ := os.MkdirTemp("", "keybar-demo-")
	v := &vault.Vault{Dir: dir, Secrets: &vault.Memory{}, By: "app"}
	seedDemo(v, demoNow)
	return v
}

func seedDemo(v *vault.Vault, now time.Time) {
	day := 24 * time.Hour
	at := func(days int) time.Time { return now.Add(-time.Duration(days) * day) }
	add := func(k vault.Key, value string) {
		ps := k.Platforms
		k.Platforms = nil
		_ = v.Add(k, []byte(value))
		_ = v.Edit(k.Name, func(kk *vault.Key) error {
			for _, p := range ps {
				up := p.Updated
				kk.AddPlatform(p, now)
				kk.Platforms[len(kk.Platforms)-1].Updated = up
				kk.Platforms[len(kk.Platforms)-1].Old = up.Before(kk.LastRotated)
			}
			return nil
		})
	}
	add(vault.Key{Name: "RAILWAY_TOKEN", Description: "Railway 部署令牌", Tags: []string{"deploy"}, Projects: []string{"rz-system"},
		Created: at(200), LastRotated: at(2),
		History:   []vault.Rotation{{At: at(2), By: "cli", Note: "Railway 安全通告后轮换"}},
		Platforms: []vault.Platform{{Kind: "github", Target: "keleorange/rz-system", Updated: at(1)}, {Kind: "railway", Target: "rz-api", Updated: at(30)}, {Kind: "env", Target: "~/rz-system/.env.local", Updated: at(30)}}},
		"example-railway-0000")
	add(vault.Key{Name: "TIDB_PROD_URL", Description: "TiDB Cloud 生产库连接串（只读账号）", Tags: []string{"db", "prod"}, Projects: []string{"rz-system"},
		Created: at(140), LastRotated: at(130),
		Platforms: []vault.Platform{{Kind: "vercel", Target: "rz-web", Envs: []string{"production"}, Updated: at(130)}, {Kind: "tidb", Target: "cluster prod-1", Updated: at(130)}}},
		"mysql://example:example@localhost/example")
	add(vault.Key{Name: "DEEPSEEK_API_KEY", Description: "DeepSeek，DS Search / LB Bar 用", Tags: []string{"ai"}, Projects: []string{"ds-search", "lb-bar"},
		Created: at(80), LastRotated: at(80),
		Platforms: []vault.Platform{{Kind: "github", Target: "horizon0514/ds-search", Updated: at(80)}}},
		"sk-example-deepseek-0000")
	add(vault.Key{Name: "LONGPORT_ACCESS_TOKEN", Description: "长桥 OpenAPI 访问令牌", Tags: []string{"trading"}, Projects: []string{"lb-bar"},
		Created: at(30), LastRotated: at(30), Expires: time.Date(2027, 1, 19, 10, 16, 0, 0, time.Local), RotateDays: -1},
		"example-longport-0000")
	add(vault.Key{Name: "OPENAI_API_KEY", Description: "OpenAI 个人账号", Tags: []string{"ai"}, Projects: []string{"scripts"},
		Created: at(40), LastRotated: at(40)},
		"sk-example-openai-0000")
	add(vault.Key{Name: "VERCEL_TOKEN", Description: "Vercel API 令牌（KeyBar 自动同步用）", Tags: []string{"deploy"},
		Created: at(10), LastRotated: at(10), Agent: "deny"},
		"example-vercel-0000")
	add(vault.Key{Name: "GITHUB_TOKEN", Description: "GitHub 细粒度令牌（Actions secrets 读写）", Tags: []string{"deploy"},
		Created: at(20), LastRotated: at(20), Agent: "deny", Expires: now.Add(70 * day)},
		"example-github-0000")
	for _, e := range []vault.Event{
		{Time: now.Add(-3 * time.Hour), Client: "mcp:claude-code", Action: "run", Keys: []string{"OPENAI_API_KEY"}, Decision: "allowed-once", Detail: "python eval.py"},
		{Time: now.Add(-2 * time.Hour), Client: "cli", Action: "run", Keys: []string{"RAILWAY_TOKEN"}, Decision: "ok", Detail: "railway"},
		{Time: now.Add(-90 * time.Minute), Client: "mcp:cursor", Action: "reveal", Keys: []string{"TIDB_PROD_URL"}, Decision: "denied"},
		{Time: now.Add(-40 * time.Minute), Client: "mcp:claude-code", Action: "run", Keys: []string{"DEEPSEEK_API_KEY"}, Decision: "allowed-15m", Detail: "go test ./..."},
		{Time: now.Add(-20 * time.Minute), Client: "app", Action: "copy", Keys: []string{"OPENAI_API_KEY"}, Decision: "ok"},
	} {
		_ = v.Log(e)
	}
}

// demoAgents is a home folder with a few agents installed (Claude Code and
// Gemini CLI connected, Cursor and opencode not, VS Code pointing at an
// old path) and the rest not: the example's, never the user's settings.
func demoAgents(exe string) *agents.Env {
	home, _ := os.MkdirTemp("", "keybar-demo-home-")
	e := &agents.Env{Home: home, Exe: exe, GOOS: "darwin", LookPath: func(string) (string, error) { return "", os.ErrNotExist }}
	put := func(body string, parts ...string) {
		p := filepath.Join(append([]string{home}, parts...)...)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(body), 0o600)
	}
	_ = os.MkdirAll(filepath.Join(home, ".cursor"), 0o755)
	put("{}\n", ".claude.json")
	put("{\n  \"theme\": \"GitHub\"\n}\n", ".gemini", "settings.json")
	put("{\n  // example\n  \"$schema\": \"https://opencode.ai/config.json\",\n}\n", ".config", "opencode", "opencode.jsonc")
	put("{\n  \"servers\": {\n    \"keybar\": {\"type\": \"stdio\", \"command\": \"/Volumes/KeyBar/KeyBar.app/Contents/MacOS/KeyBar\", \"args\": [\"mcp\", \"--client\", \"vscode\"]}\n  }\n}\n",
		"Library", "Application Support", "Code", "User", "mcp.json")
	for _, id := range []string{"claude-code", "gemini"} {
		if a, ok := agents.Find(id); ok {
			_, _ = e.Connect(a)
		}
	}
	return e
}

// demoEnvFile is an example project's .env: one new key, one KeyBar has
// with the same value, two with other values, one repeated, one empty.
func demoEnvFile() string {
	dir, _ := os.MkdirTemp("", "keybar-demo-")
	dir = filepath.Join(dir, "my-app")
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, ".env")
	_ = os.WriteFile(p, []byte("# example values only\nOPENAI_API_KEY=sk-example-openai-other\nDEEPSEEK_API_KEY=sk-example-deepseek-0000\n"+
		"STRIPE_SECRET_KEY=sk-example-stripe-0001\nRAILWAY_TOKEN=example-railway-other\nSENTRY_DSN=\nSTRIPE_SECRET_KEY=sk-example-stripe-0002\n"), 0o600)
	return p
}

func demoRequest() approval.Request {
	return approval.Request{ID: "demo", Client: "mcp:claude-code", Action: "run", Keys: []string{"DEEPSEEK_API_KEY", "TIDB_PROD_URL"},
		Command: "go test ./... -run TestRealAPI", Cwd: "~/lb-bar", Reason: "跑一下真实 API 的集成测试"}
}

// Example keys for the scan, the clipboard and the run: shaped like the
// providers' so that they are recognized, made of filler, never real.
const (
	demoOpenAI    = "sk-proj-Dm0Kx7Qp2Lw9Vb4Nr8Ty3Hs6Jc1Fg5Zu0Dm0Kx7Qp2Lw9Vb4Nr8T"
	demoAnthropic = "sk-ant-api03-Dm0Kx7Qp2Lw9Vb4Nr8Ty3Hs6Jc1Fg5Zu0Dm0Kx7Qp2Lw9Vb4Nr8Ty3Hs6Jc1Fg5Zu0Dm0Kx7Qp2Lw9Vb4AA"
	demoStripe    = "sk_" + "test_Dm0Kx7Qp2Lw9Vb4Nr8Ty3Hs6"
	demoGitHub    = "ghp_Dm0Kx7Qp2Lw9Vb4Nr8Ty3Hs6Jc1Fg5Zu0Dm0K"
	demoHF        = "hf_Dm0Kx7Qp2Lw9Vb4Nr8Ty3Hs6Jc1Fg5Zu0Dm"
)

// demoScanHome puts example projects and a shell file in the demo's home
// folder, for 扫描本机.
func demoScanHome(home string) {
	put := func(body string, parts ...string) {
		p := filepath.Join(append([]string{home}, parts...)...)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(body), 0o600)
	}
	put("PORT=3000\nNODE_ENV=development\nOPENAI_API_KEY="+demoOpenAI+"\nDATABASE_URL=postgres://app:Dm0Kx7Qp2Lw9@localhost:5432/app\n"+
		"STRIPE_SECRET_KEY="+demoStripe+"\nDEEPSEEK_API_KEY=sk-example-deepseek-0000\n", "code", "my-app", ".env")
	put("OPENAI_API_KEY="+demoOpenAI+"\nANTHROPIC_API_KEY="+demoAnthropic+"\nNEXT_PUBLIC_SITE_URL=http://localhost:3000\n", "code", "blog", ".env.local")
	put("OPENAI_API_KEY=your-key-here\n", "code", "blog", ".env.example")
	put("export PATH=\"$HOME/bin:$PATH\"\nexport GITHUB_TOKEN="+demoGitHub+"\nexport HF_TOKEN="+demoHF+"\nexport EDITOR=vim\n", ".zshrc")
}
