package agents

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func mustFind(t *testing.T, id string) Agent {
	t.Helper()
	a, ok := Find(id)
	if !ok {
		t.Fatalf("no agent %s", id)
	}
	return a
}

// Every JSON agent: connect into settings with another server and a
// comment, read back, idempotent, disconnect.
func TestEveryJSONAgent(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, a := range All {
			if a.Format != JSON || a.CLI != nil {
				continue
			}
			t.Run(goos+"/"+a.ID, func(t *testing.T) {
				e := newEnv(t)
				e.GOOS = goos
				e.AppData = filepath.Join(e.Home, "AppData", "Roaming")
				path := e.File(a)
				if path == "" {
					if (a.ID == "claude-desktop" && goos == "linux") || (a.ID == "crush" && goos == "windows") {
						return
					}
					t.Fatal("no settings file")
				}
				if s := e.StatusOf(a); s.State != NotInstalled {
					t.Fatalf("installed in an empty home: %+v", s)
				}
				// Another server, under the agent's own key, and a comment.
				other := `{"command": "other", "args": []}`
				if a.ID == "opencode" {
					other = `{"type": "local", "command": ["other"]}`
				}
				body := "{\n  // mine\n  \"theme\": \"dark\",\n" + nest(a.Key, `"other": `+other) + "\n}\n"
				write(t, path, body, 0o644)
				if s := e.StatusOf(a); s.State != NotConnected {
					t.Fatalf("%+v", s)
				}
				s, err := e.Connect(a)
				if err != nil || s.State != Connected {
					t.Fatalf("%+v %v", s, err)
				}
				got := read(t, path)
				if !strings.Contains(got, "// mine") || !strings.Contains(got, `"other"`) || !strings.Contains(got, `"theme": "dark"`) {
					t.Errorf("lost something:\n%s", got)
				}
				if read(t, path+BackupSuffix) != body {
					t.Error("no backup")
				}
				doc, err := decodeJSONC(got)
				if err != nil {
					t.Fatal(err)
				}
				var cur any = doc
				for _, k := range append(slices.Clone(a.Key), ServerName) {
					cur = cur.(map[string]any)[k]
				}
				entry := cur.(map[string]any)
				for _, f := range a.Entry(exe, Args(a.ID)) {
					if _, ok := entry[f.Key]; !ok {
						t.Errorf("entry lacks %s: %v", f.Key, entry)
					}
				}
				if _, err := e.Connect(a); err != nil || read(t, path) != got {
					t.Error("not idempotent")
				}
				if s, err := e.Disconnect(a); err != nil || s.State != NotConnected {
					t.Fatalf("%+v %v", s, err)
				}
				after := read(t, path)
				if strings.Contains(after, "keybar") || !strings.Contains(after, `"other"`) || !strings.Contains(after, "// mine") {
					t.Errorf("disconnect:\n%s", after)
				}
			})
		}
	}
}

// nest writes member inside the objects of path, indented.
func nest(path []string, member string) string {
	out := member
	for i := len(path) - 1; i >= 0; i-- {
		out = `"` + path[i] + `": {` + out + `}`
	}
	return "  " + out
}

func TestJSONCEdits(t *testing.T) {
	a := mustFind(t, "zed")
	args := Args("zed")
	cases := []struct{ name, in, want string }{
		{"empty file", "", "{\n  \"context_servers\": {\n    \"keybar\": {\n      \"command\": \"/k\",\n      \"args\": [\n        \"mcp\",\n        \"--client\",\n        \"zed\"\n      ]\n    }\n  }\n}\n"},
		{"trailing commas and comments", "// Zed settings\n{\n  \"theme\": \"One Dark\", // the theme\n  /* block */\n  \"context_servers\": {\n    \"a\": {\"command\": \"a\", \"args\": []},\n  },\n}\n",
			"// Zed settings\n{\n  \"theme\": \"One Dark\", // the theme\n  /* block */\n  \"context_servers\": {\n    \"a\": {\"command\": \"a\", \"args\": []},\n    \"keybar\": {\n      \"command\": \"/k\",\n      \"args\": [\n        \"mcp\",\n        \"--client\",\n        \"zed\"\n      ]\n    },\n  },\n}\n"},
		{"end comment after last member", "{\n  \"theme\": \"x\" // note\n}\n",
			"{\n  \"theme\": \"x\", // note\n  \"context_servers\": {\n    \"keybar\": {\n      \"command\": \"/k\",\n      \"args\": [\n        \"mcp\",\n        \"--client\",\n        \"zed\"\n      ]\n    }\n  }\n}\n"},
		{"one line", `{"theme":"x","context_servers":{"a":{"command":"a","args":[]}}}`,
			`{"theme":"x","context_servers":{"a":{"command":"a","args":[]}, "keybar": {"command":"/k","args":["mcp","--client","zed"]}}}`},
		{"tabs", "{\n\t\"context_servers\": {}\n}\n", "{\n\t\"context_servers\": {\n\t\t\"keybar\": {\n\t\t\t\"command\": \"/k\",\n\t\t\t\"args\": [\n\t\t\t\t\"mcp\",\n\t\t\t\t\"--client\",\n\t\t\t\t\"zed\"\n\t\t\t]\n\t\t}\n\t}\n}\n"},
		{"null servers", "{\n  \"context_servers\": null\n}\n", "{\n  \"context_servers\": {\n    \"keybar\": {\n      \"command\": \"/k\",\n      \"args\": [\n        \"mcp\",\n        \"--client\",\n        \"zed\"\n      ]\n    }\n  }\n}\n"},
	}
	for _, c := range cases {
		got, err := setJSONServer([]byte(c.in), a, "/k", args, true)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s:\n%s\nwant\n%s", c.name, got, c.want)
		}
		// And out again: everything else as it was.
		back, err := setJSONServer(got, a, "/k", args, false)
		if err != nil {
			t.Errorf("%s remove: %v", c.name, err)
			continue
		}
		if strings.Contains(string(back), "keybar") {
			t.Errorf("%s: left\n%s", c.name, back)
		}
		if c.in != "" {
			before, _ := decodeJSONC(c.in)
			after, _ := decodeJSONC(string(back))
			want := goDel(before, []string{"context_servers", ServerName})
			if m, ok := want.(map[string]any); ok && m["context_servers"] == nil {
				if am, _ := after.(map[string]any); am != nil {
					if cs, _ := am["context_servers"].(map[string]any); len(cs) == 0 {
						delete(am, "context_servers")
						delete(m, "context_servers")
					}
				}
			}
			if !sameJSON(want, after) {
				t.Errorf("%s: remove changed other things:\n%s", c.name, back)
			}
		}
	}
	// Removing the last member takes the comma before it; the only one
	// leaves {}.
	in := "{\n  \"context_servers\": {\n    \"a\": {\"command\": \"a\", \"args\": []},\n    \"keybar\": {\"command\": \"/k\", \"args\": []}\n  }\n}\n"
	got, err := setJSONServer([]byte(in), a, "/k", args, false)
	if err != nil || string(got) != "{\n  \"context_servers\": {\n    \"a\": {\"command\": \"a\", \"args\": []}\n  }\n}\n" {
		t.Errorf("remove last: %v\n%s", err, got)
	}
	in = "{\n  \"context_servers\": {\n    \"keybar\": {\"command\": \"/k\", \"args\": []}\n  }\n}\n"
	if got, err = setJSONServer([]byte(in), a, "/k", args, false); err != nil || string(got) != "{\n  \"context_servers\": {}\n}\n" {
		t.Errorf("remove only: %v\n%q", err, got)
	}
	// Updating an entry keeps the user's fields and comments in it.
	in = "{\n  \"context_servers\": {\n    \"keybar\": {\n      // mine\n      \"command\": \"/old\",\n      \"args\": [\"mcp\"],\n      \"env\": {\"A\": \"1\"}\n    }\n  }\n}\n"
	got, err = setJSONServer([]byte(in), a, "/k", args, true)
	if err != nil || !strings.Contains(string(got), "// mine") || !strings.Contains(string(got), `"env": {"A": "1"}`) || !strings.Contains(string(got), `"command": "/k"`) {
		t.Errorf("update: %v\n%s", err, got)
	}
	// Refused, untouched: broken, a duplicate key, not an object.
	for _, bad := range []string{"{\n  \"context_servers\": {\n", "{\"context_servers\": {}, \"context_servers\": {}}", "[]", "{\"context_servers\": [1]}", "{/* open"} {
		if _, err := setJSONServer([]byte(bad), a, "/k", args, true); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestOpencodeCommandArray(t *testing.T) {
	e := newEnv(t)
	a := mustFind(t, "opencode")
	// opencode.jsonc when it is the only one.
	jc := filepath.Join(e.Home, ".config", "opencode", "opencode.jsonc")
	write(t, jc, "{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"mcp\": {\n    \"keybar\": {\"type\": \"local\", \"command\": [\"/old\", \"mcp\", \"--client\", \"opencode\"], \"enabled\": false, \"environment\": {\"A\": \"1\"}}\n  }\n}\n", 0o644)
	if e.File(a) != jc {
		t.Fatalf("file %s", e.File(a))
	}
	if s := e.StatusOf(a); s.State != Outdated {
		t.Fatalf("%+v", s)
	}
	if s, err := e.Connect(a); err != nil || s.State != Connected {
		t.Fatalf("%+v %v", s, err)
	}
	got := read(t, jc)
	if !strings.Contains(got, `"enabled": true`) || !strings.Contains(got, `"environment": {"A": "1"}`) {
		t.Errorf("%s", got)
	}
	// XDG_CONFIG_HOME moves it.
	e.XDGConfig = filepath.Join(e.Home, "xdg")
	if !strings.HasPrefix(e.File(a), e.XDGConfig) {
		t.Error(e.File(a))
	}
}

const gooseYAML = `# goose settings
GOOSE_PROVIDER: openai
GOOSE_MODEL: gpt-4o
extensions:
  developer:
    enabled: true
    name: developer
    type: builtin
    timeout: 300
  github:
    name: GitHub
    cmd: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    enabled: true
    type: stdio
    timeout: 300
GOOSE_MODE: smart_approve
`

func TestGooseYAML(t *testing.T) {
	e := newEnv(t)
	a := mustFind(t, "goose")
	path := filepath.Join(e.Home, ".config", "goose", "config.yaml")
	write(t, path, gooseYAML, 0o600)
	if s := e.StatusOf(a); s.State != NotConnected {
		t.Fatalf("%+v", s)
	}
	if s, err := e.Connect(a); err != nil || s.State != Connected {
		t.Fatalf("%+v %v", s, err)
	}
	got := read(t, path)
	for _, want := range []string{"# goose settings", "  github:", "GOOSE_MODE: smart_approve", "  keybar:\n    name: keybar\n    type: stdio\n    cmd: \"" + exe + "\"\n    args: [\"mcp\", \"--client\", \"goose\"]\n    enabled: true"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if _, err := e.Connect(a); err != nil || read(t, path) != got {
		t.Error("not idempotent")
	}
	e2 := *e
	e2.Exe = "/opt/keybar"
	if s := e2.StatusOf(a); s.State != Outdated {
		t.Errorf("%v", s.State)
	}
	if _, err := e2.Connect(a); err != nil || strings.Count(read(t, path), "keybar:") != 1 || !strings.Contains(read(t, path), `"/opt/keybar"`) {
		t.Errorf("update: %v\n%s", err, read(t, path))
	}
	if s, err := e2.Disconnect(a); err != nil || s.State != NotConnected {
		t.Fatalf("%+v %v", s, err)
	}
	if read(t, path) != gooseYAML {
		t.Errorf("disconnect:\n%s", read(t, path))
	}
	// No extensions yet; then the last one removed: {}.
	write(t, path, "GOOSE_PROVIDER: openai\n", 0o600)
	if _, err := e.Connect(a); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Disconnect(a); err != nil || read(t, path) != "GOOSE_PROVIDER: openai\nextensions: {}\n" {
		t.Errorf("%v %q", err, read(t, path))
	}
	// A flow-style keybar it cannot edit: refused, untouched.
	odd := "extensions: {keybar: {cmd: /old, args: []}}\n"
	write(t, path, odd, 0o600)
	if _, err := e.Connect(a); err == nil || read(t, path) != odd {
		t.Errorf("odd: %v", err)
	}
	write(t, path, "extensions: [\n", 0o600)
	if _, err := e.Connect(a); err == nil {
		t.Error("broken YAML accepted")
	}
}

func TestDeepSeekHarness(t *testing.T) {
	e := newEnv(t)
	a := mustFind(t, "dsh")
	path := filepath.Join(e.Home, ".dsh", "cordis.patch.yml")
	write(t, path, "[]\n", 0o644)
	if s, err := e.Connect(a); err != nil || s.State != Connected {
		t.Fatalf("%+v %v", s, err)
	}
	got := read(t, path)
	if strings.Contains(got, "[]") || !strings.Contains(got, "name: '@deepseek-ai/dsh-mcp-client'") || !strings.Contains(got, "serverName: keybar") {
		t.Errorf("%s", got)
	}
	if s, err := e.Disconnect(a); err != nil || s.State != NotConnected || read(t, path) != "[]\n" {
		t.Errorf("%+v %v %q", s, err, read(t, path))
	}
	// Other patches kept.
	other := "# mine\n- insert:\n    - id: mcp-fetch\n      name: '@deepseek-ai/dsh-mcp-client'\n      config:\n        serverName: fetch\n        transport: stdio\n        command: uvx\n        args: ['mcp-server-fetch']\n"
	write(t, path, other, 0o644)
	if _, err := e.Connect(a); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(read(t, path), other) {
		t.Errorf("%s", read(t, path))
	}
	if _, err := e.Disconnect(a); err != nil || read(t, path) != other {
		t.Errorf("%v\n%s", err, read(t, path))
	}
	// A keybar the user wrote by hand: not ours to change.
	hand := strings.ReplaceAll(other, "fetch", "keybar")
	write(t, path, hand, 0o644)
	if _, err := e.Connect(a); err == nil || read(t, path) != hand {
		t.Errorf("hand-written: %v", err)
	}
	// $DSH_HOME.
	e.DshHome = filepath.Join(e.Home, "dsh")
	if e.File(a) != filepath.Join(e.Home, "dsh", "cordis.patch.yml") {
		t.Error(e.File(a))
	}
}

func TestPiCLI(t *testing.T) {
	e := newEnv(t)
	a := mustFind(t, "pi")
	_ = os.MkdirAll(filepath.Join(e.Home, ".pi", "agent"), 0o755)
	e.LookPath = func(n string) (string, error) {
		if n == "pi" {
			return "/usr/local/bin/pi", nil
		}
		return "", errors.New("no")
	}
	var calls []string
	fail := false
	e.Run = func(name string, args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		if fail {
			return errors.New("unknown command mcp")
		}
		if args[1] == "add" {
			write(t, filepath.Join(e.Home, ".pi", "agent", "mcp.json"), `{"mcpServers":{"keybar":{"command":"`+exe+`","args":["mcp","--client","pi"]}}}`, 0o600)
		}
		return nil
	}
	if s, err := e.Connect(a); err != nil || s.State != Connected || s.Via != "pi CLI" {
		t.Fatalf("%+v %v", s, err)
	}
	if want := "mcp remove keybar|mcp add keybar -- " + exe + " mcp --client pi"; strings.Join(calls, "|") != want {
		t.Errorf("calls %q", calls)
	}
	// An older pi without `pi mcp`: the file is edited instead.
	_ = os.Remove(filepath.Join(e.Home, ".pi", "agent", "mcp.json"))
	fail = true
	if s, err := e.Connect(a); err != nil || s.State != Connected {
		t.Fatalf("fallback: %+v %v", s, err)
	}
}

func TestSystems(t *testing.T) {
	e := newEnv(t)
	ids := func() (out []string) {
		for _, a := range e.Supported() {
			out = append(out, a.ID)
		}
		return
	}
	e.GOOS = "linux"
	if l := ids(); slices.Contains(l, "claude-desktop") || !slices.Contains(l, "goose") || len(l) != len(All)-1 {
		t.Errorf("linux %v", l)
	}
	e.GOOS = "windows"
	e.AppData = `C:\Users\me\AppData\Roaming`
	if l := ids(); slices.Contains(l, "goose") || slices.Contains(l, "crush") || !slices.Contains(l, "claude-desktop") {
		t.Errorf("windows %v", l)
	}
	e.GOOS = "darwin"
	for id, want := range map[string]string{
		"claude-desktop": "Library/Application Support/Claude/claude_desktop_config.json",
		"vscode":         "Library/Application Support/Code/User/mcp.json",
		"cline":          "Library/Application Support/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json",
		"zed":            ".config/zed/settings.json",
		"windsurf":       ".codeium/windsurf/mcp_config.json",
		"kiro":           ".kiro/settings/mcp.json",
		"gemini":         ".gemini/settings.json",
		"qwen":           ".qwen/settings.json",
		"iflow":          ".iflow/settings.json",
		"kimi":           ".kimi-code/mcp.json",
		"amp":            ".config/amp/settings.json",
		"crush":          ".config/crush/crush.json",
		"goose":          ".config/goose/config.yaml",
		"opencode":       ".config/opencode/opencode.json",
		"pi":             ".pi/agent/mcp.json",
	} {
		if got := e.File(mustFind(t, id)); got != filepath.Join(e.Home, filepath.FromSlash(want)) {
			t.Errorf("%s: %s", id, got)
		}
	}
	// Kimi: the older CLI's ~/.kimi when that is what is there.
	_ = os.MkdirAll(filepath.Join(e.Home, ".kimi"), 0o755)
	if got := e.File(mustFind(t, "kimi")); got != filepath.Join(e.Home, ".kimi", "mcp.json") {
		t.Error(got)
	}
	if s := e.StatusOf(mustFind(t, "kimi")); s.State != NotConnected {
		t.Errorf("kimi %+v", s)
	}
	// A command on the PATH counts as installed.
	e.LookPath = func(n string) (string, error) {
		if n == "gemini" {
			return "/usr/local/bin/gemini", nil
		}
		return "", errors.New("no")
	}
	if s := e.StatusOf(mustFind(t, "gemini")); s.State != NotConnected {
		t.Errorf("gemini %+v", s)
	}
	if s := e.StatusOf(mustFind(t, "qwen")); s.State != NotInstalled {
		t.Errorf("qwen %+v", s)
	}
}

func TestIDsUnique(t *testing.T) {
	seen := map[string]string{}
	for _, a := range All {
		for _, n := range append([]string{a.ID, strings.ToLower(a.Name)}, a.Aliases...) {
			if prev, ok := seen[n]; ok && prev != a.ID {
				t.Errorf("%s: %s and %s", n, prev, a.ID)
			}
			seen[n] = a.ID
		}
		if a.Format == JSON && (a.Entry == nil || len(a.Key) == 0) {
			t.Errorf("%s: no entry", a.ID)
		}
		if got, ok := Find(a.ID); !ok || got.ID != a.ID {
			t.Errorf("find %s", a.ID)
		}
	}
	for _, s := range []string{"dsh", "copilot", "kimi-cli", "gemini-cli", "Qwen Code", "claude"} {
		if _, ok := Find(s); !ok {
			t.Error(s)
		}
	}
}

// FuzzSetJSON: any input is either refused or edited so that only
// KeyBar's entry changed (setJSONServer checks that itself); never a panic.
func FuzzSetJSON(f *testing.F) {
	for _, s := range []string{"", "{}", "{\"mcpServers\":{\"a\":{}}}", "// c\n{\"mcpServers\": {\"keybar\": {\"command\": \"x\"},},}", "{\"a\": [1, {\"b\": null}], /* x */ \"mcpServers\": null}"} {
		f.Add(s)
	}
	a := All[1]
	f.Fuzz(func(t *testing.T, in string) {
		out, err := setJSONServer([]byte(in), a, "/k", Args("cursor"), true)
		if err != nil {
			return
		}
		if _, _, found, err := jsonServer(out, a); err != nil || !found {
			t.Fatalf("%q -> %q: %v", in, out, err)
		}
		if _, err := setJSONServer(out, a, "/k", Args("cursor"), false); err != nil {
			t.Fatalf("remove %q: %v", out, err)
		}
	})
}
