package agents

import "path/filepath"

// The agents KeyBar can connect, in the order shown. Each was checked
// against its own documentation (October 2026); to add one, add an entry:
// where its settings are, their format, and KeyBar's entry in them.

// Entries: the common {"command", "args"}, with "type" where the agent
// wants it.
func plain(exe string, args []string) []field {
	return []field{{"command", exe}, {"args", args}}
}

func typed(t string) func(string, []string) []field {
	return func(exe string, args []string) []field {
		return []field{{"type", t}, {"command", exe}, {"args", args}}
	}
}

func home(parts ...string) func(*Env) string {
	return func(e *Env) string { return e.path(parts...) }
}

func homes(parts ...[]string) func(*Env) []string {
	return func(e *Env) []string {
		var out []string
		for _, p := range parts {
			out = append(out, e.path(p...))
		}
		return out
	}
}

// unix: only on macOS and Linux (no documented Windows location).
func unix(f func(*Env) string) func(*Env) string {
	return func(e *Env) string {
		if e.goos() == "windows" {
			return ""
		}
		return f(e)
	}
}

var mcpServers = []string{"mcpServers"}

var All = []Agent{
	{
		ID: "claude-code", Name: "Claude Code", Aliases: []string{"claude"},
		Key: mcpServers, Entry: typed("stdio"),
		file: home(".claude.json"), noDirMark: true, marks: homes([]string{".claude.json"}, []string{".claude"}),
		Commands: []string{"claude"},
		// Claude Code rewrites ~/.claude.json while it runs: its own
		// command is the safe way to change it.
		CLI: &CLI{Name: "claude",
			Add: func(exe string, args []string) []string {
				return append([]string{"mcp", "add", ServerName, "--scope", "user", "--", exe}, args...)
			},
			Remove: []string{"mcp", "remove", ServerName, "--scope", "user"}},
	},
	{
		ID: "cursor", Name: "Cursor",
		Key: mcpServers, Entry: plain,
		file: home(".cursor", "mcp.json"), Apps: []string{"Cursor.app"},
	},
	{
		ID: "codex", Name: "Codex", Format: CodexTOML,
		file: func(e *Env) string {
			if e.CodexHome != "" {
				return filepath.Join(e.CodexHome, "config.toml")
			}
			return e.path(".codex", "config.toml")
		},
		Commands: []string{"codex"},
	},
	{
		// https://geminicli.com/docs/tools/mcp-server/
		ID: "gemini", Name: "Gemini CLI", Aliases: []string{"gemini-cli"},
		Key: mcpServers, Entry: plain,
		file: home(".gemini", "settings.json"), Commands: []string{"gemini"},
	},
	{
		// https://qwenlm.github.io/qwen-code-docs/en/users/features/mcp/
		ID: "qwen", Name: "Qwen Code", Aliases: []string{"qwen-code"},
		Key: mcpServers, Entry: plain,
		file: home(".qwen", "settings.json"), Commands: []string{"qwen"},
	},
	{
		// https://opencode.ai/docs/mcp-servers/: "mcp", a local server's
		// command an array; opencode.json or opencode.jsonc.
		ID: "opencode", Name: "opencode",
		Key: []string{"mcp"},
		Entry: func(exe string, args []string) []field {
			return []field{{"type", "local"}, {"command", append([]string{exe}, args...)}, {"enabled", true}}
		},
		file: func(e *Env) string {
			if p := e.xdg("opencode", "opencode.jsonc"); exists(p) && !exists(e.xdg("opencode", "opencode.json")) {
				return p
			}
			return e.xdg("opencode", "opencode.json")
		},
		marks:    homes([]string{".local", "share", "opencode"}),
		Commands: []string{"opencode"},
	},
	{
		// https://pi.dev/docs/latest/mcp: built in since pi 0.99;
		// `pi mcp add` writes ~/.pi/agent/mcp.json.
		ID: "pi", Name: "pi", Aliases: []string{"pi-coding-agent"},
		Key: mcpServers, Entry: plain,
		file: home(".pi", "agent", "mcp.json"),
		CLI: &CLI{Name: "pi", Fallback: true,
			Add: func(exe string, args []string) []string {
				return append([]string{"mcp", "add", ServerName, "--", exe}, args...)
			},
			Remove: []string{"mcp", "remove", ServerName}},
	},
	{
		// https://www.kimi.com/code/docs/en/kimi-code-cli/customization/mcp.html:
		// ~/.kimi-code/mcp.json; the older Kimi CLI, ~/.kimi/mcp.json.
		ID: "kimi", Name: "Kimi Code", Aliases: []string{"kimi-cli", "kimi-code"},
		Key: mcpServers, Entry: plain,
		file: func(e *Env) string {
			if !exists(e.path(".kimi-code")) && exists(e.path(".kimi")) {
				return e.path(".kimi", "mcp.json")
			}
			return e.path(".kimi-code", "mcp.json")
		},
		marks: homes([]string{".kimi"}), Commands: []string{"kimi"},
	},
	{
		// https://platform.iflow.cn/en/cli/examples/mcp
		ID: "iflow", Name: "iFlow CLI", Aliases: []string{"iflow-cli"},
		Key: mcpServers, Entry: plain,
		file: home(".iflow", "settings.json"), Commands: []string{"iflow"},
	},
	{
		// https://github.com/charmbracelet/crush: "mcp" in crush.json.
		ID: "crush", Name: "Crush",
		Key: []string{"mcp"}, Entry: typed("stdio"),
		file:  unix(func(e *Env) string { return e.xdg("crush", "crush.json") }),
		marks: homes([]string{".local", "share", "crush"}), Commands: []string{"crush"},
	},
	{
		// https://ampcode.com/docs/customize/mcp: "amp.mcpServers".
		ID: "amp", Name: "Amp",
		Key: []string{"amp.mcpServers"}, Entry: plain,
		file: home(".config", "amp", "settings.json"), Commands: []string{"amp"},
	},
	{
		// https://block.github.io/goose/docs/guides/config-files
		ID: "goose", Name: "Goose", Format: GooseYAML,
		file: unix(func(e *Env) string { return e.xdg("goose", "config.yaml") }), Commands: []string{"goose"},
		Apps: []string{"Goose.app"},
	},
	{
		// https://github.com/deepseek-ai/deepseek-harness: dsh, its MCP
		// client a Cordis plugin in $DSH_HOME/cordis.patch.yml.
		ID: "deepseek-harness", Name: "DeepSeek Harness", Aliases: []string{"dsh", "deepseek"},
		Format: DshYAML,
		file: func(e *Env) string {
			if e.DshHome != "" {
				return filepath.Join(e.DshHome, "cordis.patch.yml")
			}
			return e.path(".dsh", "cordis.patch.yml")
		},
		Commands: []string{"dsh"},
	},
	{
		// https://modelcontextprotocol.io/quickstart/user
		ID: "claude-desktop", Name: "Claude Desktop", Aliases: []string{"claude-app"},
		Key: mcpServers, Entry: plain,
		file: func(e *Env) string {
			if e.goos() == "linux" {
				return ""
			}
			return e.appConfig("Claude", "claude_desktop_config.json")
		},
		Apps: []string{"Claude.app"},
	},
	{
		// https://code.visualstudio.com/docs/agents/reference/mcp-configuration:
		// the user profile's mcp.json, "servers".
		ID: "vscode", Name: "VS Code", Aliases: []string{"code", "copilot", "github-copilot", "vs-code"},
		Key: []string{"servers"}, Entry: typed("stdio"),
		file: func(e *Env) string { return e.appConfig("Code", "User", "mcp.json") },
		Apps: []string{"Visual Studio Code.app"},
	},
	{
		// https://docs.windsurf.com/windsurf/cascade/mcp
		ID: "windsurf", Name: "Windsurf",
		Key: mcpServers, Entry: plain,
		file: home(".codeium", "windsurf", "mcp_config.json"), Apps: []string{"Windsurf.app"},
	},
	{
		// https://zed.dev/docs/ai/mcp: "context_servers"; args required.
		ID: "zed", Name: "Zed",
		Key: []string{"context_servers"}, Entry: plain,
		file: func(e *Env) string {
			switch e.goos() {
			case "darwin":
				return e.path(".config", "zed", "settings.json")
			case "windows":
				return e.appConfig("Zed", "settings.json")
			}
			return e.xdg("zed", "settings.json")
		},
		Apps: []string{"Zed.app"},
	},
	{
		// https://kiro.dev/docs/mcp/configuration/
		ID: "kiro", Name: "Kiro",
		Key: mcpServers, Entry: plain,
		file: home(".kiro", "settings", "mcp.json"), marks: homes([]string{".kiro"}), Apps: []string{"Kiro.app"},
	},
	{
		// https://docs.cline.bot/mcp/configuring-mcp-servers: the VS Code
		// extension's cline_mcp_settings.json.
		ID: "cline", Name: "Cline", Aliases: []string{"cline-vscode"},
		Key: mcpServers, Entry: plain,
		file: func(e *Env) string {
			return e.appConfig("Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json")
		},
		marks: func(e *Env) []string {
			return []string{e.appConfig("Code", "User", "globalStorage", "saoudrizwan.claude-dev")}
		},
	},
}
