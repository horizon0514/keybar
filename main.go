// KeyBar keeps API keys in the system's keychain, not in .env files: a
// menu bar app that lists them with where else each is stored and when it
// was last rotated, reminds of rotations, and approves agents' requests;
// and its command line, keys, which runs programs with the keys they need
// and serves agents over MCP. One program: run as keys (or with a
// command), it is the command line.
package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"keybar/agents"
	"keybar/approval"
	"keybar/cli"
	"keybar/remote"
	"keybar/vault"

	"github.com/egoist/mygo"
)

const version = "0.2.0"

// cliCommands are the arguments that make the app its command line.
var cliCommands = []string{"help", "-h", "--help", "version", "add", "set", "get", "ls", "list", "show", "rm", "edit", "run",
	"import", "rotate", "done", "undone", "sync", "audit", "doctor", "mcp", "config", "export", "connect", "disconnect"}

// isCLI tells whether the program runs as the command line: named keys
// (a link to it, or keys.exe), or given a command.
func isCLI(argv []string) bool {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(argv[0])), ".exe")
	if name == "keys" {
		return true
	}
	return len(argv) > 1 && slices.Contains(cliCommands, argv[1])
}

// dataDir is KeyBar's folder: ~/Library/Application Support/KeyBar,
// %APPDATA%\KeyBar, ~/.config/keybar; KEYBAR_DIR elsewhere.
func dataDir() string {
	if d := os.Getenv("KEYBAR_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	name := "keybar"
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		name = "KeyBar"
	}
	return filepath.Join(base, name)
}

// systemSecrets is the system's credential store, under the app's
// identifier, which the app and its command line share.
type systemSecrets struct{}

func (systemSecrets) store() mygo.SecretsModule { return mygo.Secrets.Service(vault.Service) }

func (s systemSecrets) Get(a string) ([]byte, error) {
	b, err := s.store().Get(a)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return nil, vault.ErrNotFound
	}
	return b, err
}
func (s systemSecrets) Set(a string, v []byte) error { return s.store().Set(a, v) }
func (s systemSecrets) Delete(a string) error {
	err := s.store().Delete(a)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return nil
	}
	return err
}

func newVault(by string) *vault.Vault {
	dir := dataDir()
	_ = os.MkdirAll(dir, 0o700)
	return &vault.Vault{Dir: dir, Secrets: systemSecrets{}, By: by}
}

// agentEnv is this computer's agents, to run this program as KeyBar's MCP
// server.
func agentEnv() *agents.Env {
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return &agents.Env{Home: home, Exe: exe, CodexHome: os.Getenv("CODEX_HOME")}
}

func cliMain(args []string) int {
	v := newVault("cli")
	s := vault.LoadSettings(v.Dir)
	env := &cli.Env{
		Vault: v, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Version: version,
		Terminal: cli.Terminal, Password: cli.Password, Getwd: os.Getwd, Exec: cli.ExecCmd,
		Gate:   &approval.Gate{Dir: v.Dir, Dialog: approval.Dialog{Minutes: s.GrantMinutes}},
		Syncer: &remote.Syncer{Token: v.Value},
		Agents: agentEnv(),
	}
	return cli.Main(env, args)
}

func main() {
	if isCLI(os.Args) {
		os.Exit(cliMain(os.Args[1:]))
	}
	log.SetFlags(log.LstdFlags)
	guiMain()
}
