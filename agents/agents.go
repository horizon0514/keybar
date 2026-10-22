// Package agents connects KeyBar's MCP server to the AI agents on this
// computer, Claude Code, Cursor and Codex, by adding a "keybar" server to
// their MCP settings: merged into what is there, the file backed up first,
// nothing written when it is already right.
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

// Agent is an agent KeyBar can connect.
type Agent struct {
	ID   string // claude-code, cursor, codex: also its --client
	Name string // Claude Code
}

// All are the agents, in the order shown.
var All = []Agent{{"claude-code", "Claude Code"}, {"cursor", "Cursor"}, {"codex", "Codex"}}

// Find finds an agent by its ID or name.
func Find(s string) (Agent, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, a := range All {
		if s == a.ID || s == strings.ToLower(a.Name) || s == strings.ReplaceAll(a.ID, "-", "") || (a.ID == "claude-code" && s == "claude") {
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
	Via   string // "claude CLI" when Claude Code is configured by its command
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
}

// Args are the server's arguments for agent.
func Args(agent string) []string { return []string{"mcp", "--client", agent} }

func (e *Env) path(parts ...string) string {
	return filepath.Join(append([]string{e.Home}, parts...)...)
}

// File is agent's settings file.
func (e *Env) File(a Agent) string {
	switch a.ID {
	case "claude-code":
		return e.path(".claude.json")
	case "cursor":
		return e.path(".cursor", "mcp.json")
	}
	if e.CodexHome != "" {
		return filepath.Join(e.CodexHome, "config.toml")
	}
	return e.path(".codex", "config.toml")
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// installed tells whether the agent is on this computer: its settings
// folder or its command.
func (e *Env) installed(a Agent) bool {
	switch a.ID {
	case "claude-code":
		return exists(e.path(".claude.json")) || exists(e.path(".claude")) || e.claudeCLI() != ""
	case "cursor":
		return exists(e.path(".cursor")) || (runtime.GOOS == "darwin" && e.LookPath == nil && exists("/Applications/Cursor.app"))
	}
	return exists(filepath.Dir(e.File(a))) || e.look("codex") != ""
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
	for _, d := range []string{e.path(".claude", "local"), e.path(".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin", e.path(".npm-global", "bin"), e.path(".bun", "bin")} {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

func (e *Env) claudeCLI() string { return e.look("claude") }

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

// Status reads every agent's state.
func (e *Env) Status() []Status {
	out := make([]Status, 0, len(All))
	for _, a := range All {
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
	if a.ID == "claude-code" && e.claudeCLI() != "" {
		s.Via = "claude CLI"
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
	if a.ID == "codex" {
		return tomlServer(b)
	}
	return jsonServer(b)
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
	if a.ID == "claude-code" && s.Via != "" {
		// Claude Code rewrites ~/.claude.json while it runs: its own command
		// is the safe way to change it.
		cli := e.claudeCLI()
		_ = e.run(cli, "mcp", "remove", ServerName, "--scope", "user")
		err = e.run(cli, append([]string{"mcp", "add", ServerName, "--scope", "user", "--", e.Exe}, Args(a.ID)...)...)
	} else {
		err = e.edit(a, true)
	}
	if err != nil {
		return e.StatusOf(a), err
	}
	return e.StatusOf(a), nil
}

// Disconnect removes KeyBar from agent's settings.
func (e *Env) Disconnect(a Agent) (Status, error) {
	s := e.StatusOf(a)
	if s.State == NotInstalled || s.State == NotConnected {
		return s, nil
	}
	var err error
	if a.ID == "claude-code" && s.Via != "" {
		err = e.run(e.claudeCLI(), "mcp", "remove", ServerName, "--scope", "user")
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
	if a.ID == "codex" {
		out, err = setTOMLServer(old, e.Exe, Args(a.ID), add)
	} else {
		out, err = setJSONServer(old, e.Exe, Args(a.ID), a.ID == "claude-code", add)
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
