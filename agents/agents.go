// Package agents connects KeyBar's MCP server to the AI agents and editors
// on this computer by adding a "keybar" server to their MCP settings:
// merged into what is there, the file backed up first, nothing written
// when it is already right. The agents are data (registry.go).
package agents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// ServerName is KeyBar's entry in the agents' settings.
const ServerName = "keybar"

// Format is how an agent's settings file is written.
type Format int

const (
	JSON      Format = iota // JSON, comments and trailing commas allowed
	CodexTOML               // [mcp_servers.keybar]
	GooseYAML               // extensions: keybar:
	DshYAML                 // a Cordis patch list
)

func (f Format) String() string {
	return [...]string{"JSON", "TOML", "YAML", "YAML"}[f]
}

// CLI is an agent's own command for its MCP settings, used when it is
// installed: the safe way to change a file the agent rewrites itself.
type CLI struct {
	Name   string
	Add    func(exe string, args []string) []string
	Remove []string
	// Fallback: when the command fails, edit the file instead.
	Fallback bool
}

// Agent is an agent KeyBar can connect.
type Agent struct {
	ID      string   // claude-code: also its --client
	Name    string   // Claude Code
	Aliases []string // other names Find knows
	Format  Format
	// Key is the path to the servers object (JSON).
	Key []string
	// Entry is KeyBar's entry's fields, in order (JSON).
	Entry func(exe string, args []string) []field
	// file is the settings file; "" when the agent is not on this system.
	file func(e *Env) string
	// marks are files or folders whose presence means it is installed,
	// besides the settings file's folder (unless own is set).
	marks func(e *Env) []string
	// noDirMark: the settings file's folder does not tell (~ for ~/.claude.json).
	noDirMark bool
	Commands  []string // commands that mean it is installed
	Apps      []string // macOS apps that mean it is installed
	CLI       *CLI
}

// Find finds an agent by its ID, name or alias.
func Find(s string) (Agent, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	norm := func(x string) string {
		return strings.NewReplacer("-", "", " ", "", "_", "", "/", "").Replace(strings.ToLower(x))
	}
	for _, a := range All {
		if s == a.ID || s == strings.ToLower(a.Name) || norm(s) == norm(a.ID) || norm(s) == norm(a.Name) || slices.Contains(a.Aliases, s) {
			return a, true
		}
	}
	return Agent{}, false
}

// State is an agent's connection.
type State int

const (
	NotInstalled State = iota
	NotConnected
	Outdated  // a keybar entry, but for another program path
	Connected //
)

func (s State) Zh() string {
	return [...]string{"未安装", "未连接", "需要更新", "已连接"}[s]
}

// Status is an agent's state, and where its settings are.
type Status struct {
	Agent
	State State
	File  string // the settings file
	Via   string // "claude CLI" when it is configured by its command
	Err   string // the settings could not be read
}

// Env is the computer: the home folder, the program agents should run,
// and how to find and run other programs (tests fake them).
type Env struct {
	Home string
	Exe  string
	// LookPath finds a program; nil: exec.LookPath, then the usual places.
	LookPath func(string) (string, error)
	// Run runs a program; nil: exec with a timeout.
	Run func(name string, args ...string) error
	// CodexHome is $CODEX_HOME; ~/.codex when empty.
	CodexHome string
	// XDGConfig is $XDG_CONFIG_HOME; ~/.config when empty.
	XDGConfig string
	// AppData is %APPDATA% on Windows.
	AppData string
	// DshHome is $DSH_HOME; ~/.dsh when empty.
	DshHome string
	// GOOS is the system; runtime.GOOS when empty.
	GOOS string
}

// Args are the server's arguments for agent.
func Args(agent string) []string { return []string{"mcp", "--client", agent} }

func (e *Env) path(parts ...string) string {
	return filepath.Join(append([]string{e.Home}, parts...)...)
}

func (e *Env) goos() string {
	if e.GOOS != "" {
		return e.GOOS
	}
	return runtime.GOOS
}

// xdg is ~/.config, or $XDG_CONFIG_HOME.
func (e *Env) xdg(parts ...string) string {
	base := e.XDGConfig
	if base == "" {
		base = e.path(".config")
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

// appConfig is where desktop apps keep settings: ~/Library/Application
// Support, %APPDATA%, ~/.config.
func (e *Env) appConfig(parts ...string) string {
	var base string
	switch e.goos() {
	case "darwin":
		base = e.path("Library", "Application Support")
	case "windows":
		base = e.AppData
		if base == "" {
			base = e.path("AppData", "Roaming")
		}
	default:
		return e.xdg(parts...)
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

// File is agent's settings file; "" when it has none on this system.
func (e *Env) File(a Agent) string { return a.file(e) }

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// installed tells whether the agent is on this computer: its settings
// folder, its command or its app.
func (e *Env) installed(a Agent) bool {
	f := e.File(a)
	if f == "" {
		return false
	}
	if !a.noDirMark && exists(filepath.Dir(f)) {
		return true
	}
	if a.marks != nil {
		for _, m := range a.marks(e) {
			if exists(m) {
				return true
			}
		}
	}
	for _, c := range a.Commands {
		if e.look(c) != "" {
			return true
		}
	}
	if e.goos() == "darwin" && e.LookPath == nil {
		for _, app := range a.Apps {
			if exists(filepath.Join("/Applications", app)) || exists(e.path("Applications", app)) {
				return true
			}
		}
	}
	return false
}

func (e *Env) look(name string) string {
	if e.LookPath != nil {
		p, err := e.LookPath(name)
		if err != nil {
			return ""
		}
		return p
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	// An app started from the Dock has a short PATH: the usual places.
	for _, d := range []string{e.path(".claude", "local"), e.path(".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin",
		e.path(".npm-global", "bin"), e.path(".bun", "bin"), e.path(".opencode", "bin"), e.path(".cargo", "bin"), e.path("go", "bin")} {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// cli is agent's own command, when it has one and it is installed.
func (e *Env) cli(a Agent) string {
	if a.CLI == nil {
		return ""
	}
	return e.look(a.CLI.Name)
}

func (e *Env) run(name string, args ...string) error {
	if e.Run != nil {
		return e.Run(name, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("%s：%v %s", filepath.Base(name), err, msg)
	}
	return nil
}

// Check checks the program path agents would run.
func (e *Env) Check() error {
	switch {
	case e.Exe == "":
		return errors.New("找不到 KeyBar 程序的路径")
	case strings.Contains(e.Exe, "/AppTranslocation/"):
		return errors.New("KeyBar 正从隔离位置运行：先把它拖进“应用程序”文件夹，再打开它连接")
	case strings.Contains(e.Exe, "/go-build"):
		return errors.New("这是 go run 的临时程序，路径会失效：请用安装好的 KeyBar 连接")
	}
	return nil
}

// Supported are the agents KeyBar can connect on this system.
func (e *Env) Supported() []Agent {
	var out []Agent
	for _, a := range All {
		if e.File(a) != "" {
			out = append(out, a)
		}
	}
	return out
}

// Status reads every agent's state.
func (e *Env) Status() []Status {
	list := e.Supported()
	out := make([]Status, 0, len(list))
	for _, a := range list {
		out = append(out, e.StatusOf(a))
	}
	return out
}

// StatusOf reads agent's state.
func (e *Env) StatusOf(a Agent) Status {
	s := Status{Agent: a, File: e.File(a)}
	if !e.installed(a) {
		return s
	}
	if e.cli(a) != "" {
		s.Via = a.CLI.Name + " CLI"
	}
	cmd, args, found, err := e.read(a)
	switch {
	case err != nil:
		s.State, s.Err = NotConnected, err.Error()
	case !found:
		s.State = NotConnected
	case sameProgram(cmd, e.Exe) && slices.Equal(args, Args(a.ID)):
		s.State = Connected
	default:
		s.State = Outdated
	}
	return s
}

func sameProgram(a, b string) bool {
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// read reads KeyBar's entry in agent's settings.
func (e *Env) read(a Agent) (cmd string, args []string, found bool, err error) {
	b, err := os.ReadFile(e.File(a))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	switch a.Format {
	case CodexTOML:
		return tomlServer(b)
	case GooseYAML:
		return gooseServer(b)
	case DshYAML:
		return dshServer(b)
	}
	return jsonServer(b, a)
}

// Connect adds or updates KeyBar in agent's settings; nothing is written
// when it is already there.
func (e *Env) Connect(a Agent) (Status, error) {
	if err := e.Check(); err != nil {
		return e.StatusOf(a), err
	}
	s := e.StatusOf(a)
	switch {
	case s.State == Connected:
		return s, nil
	case s.State == NotInstalled:
		return s, fmt.Errorf("没找到 %s", a.Name)
	}
	var err error
	if cli := e.cli(a); cli != "" {
		_ = e.run(cli, a.CLI.Remove...)
		err = e.run(cli, a.CLI.Add(e.Exe, Args(a.ID))...)
		if a.CLI.Fallback && (err != nil || e.StatusOf(a).State != Connected) {
			err = e.edit(a, true)
		}
	} else {
		err = e.edit(a, true)
	}
	return e.StatusOf(a), err
}

// Disconnect removes KeyBar from agent's settings.
func (e *Env) Disconnect(a Agent) (Status, error) {
	s := e.StatusOf(a)
	if s.State == NotInstalled || s.State == NotConnected {
		return s, nil
	}
	var err error
	if cli := e.cli(a); cli != "" {
		err = e.run(cli, a.CLI.Remove...)
		if a.CLI.Fallback && (err != nil || e.StatusOf(a).State != NotConnected) {
			err = e.edit(a, false)
		}
	} else {
		err = e.edit(a, false)
	}
	return e.StatusOf(a), err
}

// edit adds (or removes) the entry in agent's settings file.
func (e *Env) edit(a Agent, add bool) error {
	path := e.File(a)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real // a dotfiles manager's link: change what it points to
	}
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var out []byte
	switch a.Format {
	case CodexTOML:
		out, err = setTOMLServer(old, e.Exe, Args(a.ID), add)
	case GooseYAML:
		out, err = setGooseServer(old, e.Exe, Args(a.ID), add)
	case DshYAML:
		out, err = setDshServer(old, e.Exe, Args(a.ID), add)
	default:
		out, err = setJSONServer(old, a, e.Exe, Args(a.ID), add)
	}
	if err != nil {
		return fmt.Errorf("%s：%w（没有修改）", path, err)
	}
	if bytes.Equal(out, old) {
		return nil
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
		if err := backup(path, old, mode); err != nil {
			return fmt.Errorf("备份 %s：%w（没有修改）", path, err)
		}
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeAtomic(path, out, mode)
}

// BackupSuffix names the copy of a settings file as it was before KeyBar
// last changed it.
const BackupSuffix = ".keybar-backup"

func backup(path string, b []byte, mode os.FileMode) error {
	return os.WriteFile(path+BackupSuffix, b, mode)
}

func writeAtomic(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".keybar-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil && runtime.GOOS != "windows" {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
