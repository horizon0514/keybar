package agents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const exe = "/Applications/KeyBar.app/Contents/MacOS/KeyBar"

func newEnv(t *testing.T) *Env {
	t.Helper()
	return &Env{Home: t.TempDir(), Exe: exe, LookPath: func(string) (string, error) { return "", errors.New("none") },
		Run: func(string, ...string) error { t.Error("ran a program"); return nil }}
}

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestNotInstalled(t *testing.T) {
	e := newEnv(t)
	for _, s := range e.Status() {
		if s.State != NotInstalled {
			t.Errorf("%s: %v", s.ID, s.State)
		}
	}
	if _, err := e.Connect(All[1]); err == nil {
		t.Error("connected an agent that is not there")
	}
	if _, err := os.Stat(filepath.Join(e.Home, ".cursor")); err == nil {
		t.Error("created Cursor's folder")
	}
}

const claudeJSON = `{
  "numStartups": 42,
  "projects": {
    "/Users/me/code/a": {
      "allowedTools": [],
      "mcpServers": {}
    }
  },
  "mcpServers": {
    "github": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {"GITHUB_PERSONAL_ACCESS_TOKEN": "placeholder-not-a-secret"}
    }
  },
  "zLast": "<keep \u003c as is>"
}
`

func TestClaudeJSONMerge(t *testing.T) {
	e := newEnv(t)
	path := filepath.Join(e.Home, ".claude.json")
	write(t, path, claudeJSON, 0o600)
	if s := e.StatusOf(All[0]); s.State != NotConnected || s.Via != "" {
		t.Fatalf("%+v", s)
	}
	s, err := e.Connect(All[0])
	if err != nil || s.State != Connected {
		t.Fatalf("%+v %v", s, err)
	}
	got := read(t, path)
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatal(err)
	}
	servers := m["mcpServers"].(map[string]any)
	if _, ok := servers["github"]; !ok {
		t.Error("lost another server")
	}
	kb := servers["keybar"].(map[string]any)
	if kb["command"] != exe || kb["type"] != "stdio" || len(kb["args"].([]any)) != 3 || kb["args"].([]any)[2] != "claude-code" {
		t.Errorf("entry %v", kb)
	}
	// Every other member kept, in its order, and the file's style.
	order := []string{`"numStartups"`, `"projects"`, `"mcpServers"`, `"github"`, `"keybar"`, `"zLast"`}
	last := -1
	for _, k := range order {
		i := strings.Index(got, k)
		if i <= last {
			t.Errorf("%s out of order:\n%s", k, got)
		}
		last = i
	}
	if !strings.Contains(got, `\u003c as is`) || !strings.HasSuffix(got, "}\n") || !strings.Contains(got, "\n  \"numStartups\": 42,") {
		t.Errorf("style changed:\n%s", got)
	}
	if read(t, path+BackupSuffix) != claudeJSON {
		t.Error("no backup of the original")
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
			t.Errorf("mode %v", st.Mode())
		}
	}
	// Connecting again writes nothing.
	_ = os.Remove(path + BackupSuffix)
	if _, err := e.Connect(All[0]); err != nil || read(t, path) != got {
		t.Error("not idempotent")
	}
	if _, err := os.Stat(path + BackupSuffix); err == nil {
		t.Error("backed up with nothing to change")
	}
	// Another program path: outdated, updated in place, the user's env kept.
	var withEnv map[string]any
	_ = json.Unmarshal([]byte(got), &withEnv)
	withEnv["mcpServers"].(map[string]any)["keybar"].(map[string]any)["env"] = map[string]any{"X": "1"}
	b, _ := json.MarshalIndent(withEnv, "", "  ")
	write(t, path, string(b), 0o600)
	e2 := *e
	e2.Exe = "/opt/KeyBar/keybar"
	if s := e2.StatusOf(All[0]); s.State != Outdated {
		t.Errorf("state %v", s.State)
	}
	if s, err := e2.Connect(All[0]); err != nil || s.State != Connected {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, path), `"X": "1"`) {
		t.Error("lost the entry's env")
	}
	// Disconnect: only keybar goes.
	if s, err := e2.Disconnect(All[0]); err != nil || s.State != NotConnected {
		t.Fatalf("%+v %v", s, err)
	}
	after := read(t, path)
	if strings.Contains(after, "keybar") || !strings.Contains(after, `"github"`) || !strings.Contains(after, `"numStartups"`) {
		t.Errorf("disconnect:\n%s", after)
	}
}

func TestCursorNewFileAndBadJSON(t *testing.T) {
	e := newEnv(t)
	_ = os.MkdirAll(filepath.Join(e.Home, ".cursor"), 0o755)
	if s := e.StatusOf(All[1]); s.State != NotConnected {
		t.Fatalf("%+v", s)
	}
	if _, err := e.Connect(All[1]); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.Home, ".cursor", "mcp.json")
	want := "{\n  \"mcpServers\": {\n    \"keybar\": {\n      \"command\": \"" + exe + "\",\n      \"args\": [\n        \"mcp\",\n        \"--client\",\n        \"cursor\"\n      ]\n    }\n  }\n}\n"
	if got := read(t, path); got != want {
		t.Errorf("got\n%s", got)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
			t.Errorf("mode %v", st.Mode())
		}
	}
	bad := "{\n  // a comment\n  \"mcpServers\": {,}\n}\n"
	write(t, path, bad, 0o644)
	if _, err := e.Connect(All[1]); err == nil || !strings.Contains(err.Error(), "没有修改") {
		t.Errorf("err %v", err)
	}
	if read(t, path) != bad {
		t.Error("changed a file it could not read")
	}
}

func TestSymlinkedSettings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	e := newEnv(t)
	real := filepath.Join(e.Home, "dotfiles", "mcp.json")
	write(t, real, `{"mcpServers":{}}`, 0o644)
	_ = os.MkdirAll(filepath.Join(e.Home, ".cursor"), 0o755)
	link := filepath.Join(e.Home, ".cursor", "mcp.json")
	_ = os.Symlink(real, link)
	if _, err := e.Connect(All[1]); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("replaced the link")
	}
	if !strings.Contains(read(t, real), "keybar") {
		t.Error("did not change the file linked to")
	}
}

const codexTOML = `# Codex settings
model = "o4-mini"

[mcp_servers.other]
command = "other-server"
args = ["--x"]

[mcp_servers.keybar]
# old entry
command = "/old/KeyBar"
args = [
  "mcp",
  "--client", "codex",
]
startup_timeout_sec = 20

[mcp_servers.keybar.env]
FOO = "bar"

[profiles.fast]
model = "o3"
`

func TestCodexTOML(t *testing.T) {
	e := newEnv(t)
	path := filepath.Join(e.Home, ".codex", "config.toml")
	write(t, path, codexTOML, 0o644)
	if s := e.StatusOf(All[2]); s.State != Outdated {
		t.Fatalf("%+v", s)
	}
	if _, err := e.Connect(All[2]); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	for _, want := range []string{"# Codex settings", "[mcp_servers.other]", `args = ["--x"]`, "# old entry", "startup_timeout_sec = 20",
		"[mcp_servers.keybar.env]", `FOO = "bar"`, "[profiles.fast]", `command = "` + exe + `"`, `args = ["mcp", "--client", "codex"]`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "/old/KeyBar") || strings.Count(got, "[mcp_servers.keybar]") != 1 {
		t.Errorf("old entry left:\n%s", got)
	}
	if read(t, path+BackupSuffix) != codexTOML {
		t.Error("no backup")
	}
	if s := e.StatusOf(All[2]); s.State != Connected {
		t.Errorf("%+v", s)
	}
	again := got
	if _, err := e.Connect(All[2]); err != nil || read(t, path) != again {
		t.Error("not idempotent")
	}
	if _, err := e.Disconnect(All[2]); err != nil {
		t.Fatal(err)
	}
	got = read(t, path)
	if strings.Contains(got, "keybar") || !strings.Contains(got, "[mcp_servers.other]") || !strings.Contains(got, "[profiles.fast]") || !strings.Contains(got, `model = "o4-mini"`) {
		t.Errorf("disconnect:\n%s", got)
	}
	// A fresh config: the table is appended.
	write(t, path, "model = \"o3\"\n", 0o644)
	if _, err := e.Connect(All[2]); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "model = \"o3\"\n\n[mcp_servers.keybar]\ncommand = \""+exe+"\"\nargs = [\"mcp\", \"--client\", \"codex\"]\n" {
		t.Errorf("append:\n%q", got)
	}
	// An inline table it cannot edit as text: refused, untouched.
	odd := "[mcp_servers]\nkeybar = { command = \"/old\", args = [] }\n"
	write(t, path, odd, 0o644)
	if _, err := e.Connect(All[2]); err == nil || read(t, path) != odd {
		t.Errorf("odd form: %v", err)
	}
}

func TestClaudeCLI(t *testing.T) {
	e := newEnv(t)
	var calls []string
	e.LookPath = func(n string) (string, error) {
		if n == "claude" {
			return "/usr/local/bin/claude", nil
		}
		return "", errors.New("no")
	}
	e.Run = func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if args[1] == "add" { // what claude mcp add does
			write(t, filepath.Join(e.Home, ".claude.json"), `{"mcpServers":{"keybar":{"type":"stdio","command":"`+exe+`","args":["mcp","--client","claude-code"],"env":{}}}}`, 0o600)
		}
		return nil
	}
	s := e.StatusOf(All[0])
	if s.State != NotConnected || s.Via != "claude CLI" {
		t.Fatalf("%+v", s)
	}
	if s, err := e.Connect(All[0]); err != nil || s.State != Connected {
		t.Fatalf("%+v %v", s, err)
	}
	want := []string{"/usr/local/bin/claude mcp remove keybar --scope user", "/usr/local/bin/claude mcp add keybar --scope user -- " + exe + " mcp --client claude-code"}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls %q", calls)
	}
	calls = nil
	if _, err := e.Connect(All[0]); err != nil || len(calls) != 0 {
		t.Error("ran claude when connected")
	}
}

func TestCheckExe(t *testing.T) {
	for _, p := range []string{"", "/private/var/folders/x/T/AppTranslocation/ABC/d/KeyBar.app/Contents/MacOS/KeyBar", "/tmp/go-build123/b001/exe/keybar"} {
		e := &Env{Exe: p}
		if e.Check() == nil {
			t.Errorf("%q accepted", p)
		}
	}
}

func TestFind(t *testing.T) {
	for _, s := range []string{"claude", "Claude Code", "claude-code", "CURSOR", "codex"} {
		if _, ok := Find(s); !ok {
			t.Error(s)
		}
	}
	if _, ok := Find("vim"); ok {
		t.Error("vim")
	}
}
