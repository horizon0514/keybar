// Package approval asks the user whether an agent may use keys: through
// the running KeyBar app (a local connection, IPC), or, without it, a
// system dialog. Grants are scoped to a client and a key.
package approval

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// Request is an agent asking to use keys.
type Request struct {
	ID      string   `json:"id"`
	Client  string   `json:"client"`            // mcp:claude-code, cli…
	Action  string   `json:"action"`            // reveal (the value itself), run (in a program)
	Keys    []string `json:"keys"`              // the keys' names
	Command string   `json:"command,omitempty"` // for run: the program and its arguments, as shown
	Cwd     string   `json:"cwd,omitempty"`
	Reason  string   `json:"reason,omitempty"` // what the agent says it needs them for
	PID     int      `json:"pid,omitempty"`
	Plain   bool     `json:"plain,omitempty"` // for run: the output is not redacted (keys run without --redact)
}

// Decision is the user's answer.
type Decision struct {
	Allow   bool          `json:"allow"`
	For     time.Duration `json:"for,omitempty"` // 0: this once
	Why     string        `json:"why,omitempty"` // denied, timeout, no-approver…
	Granted bool          `json:"granted,omitempty"`
}

// Code is the decision for the access log.
func (d Decision) Code() string {
	switch {
	case d.Granted:
		return "allowed-grant"
	case d.Allow && d.For == 0:
		return "allowed-once"
	case d.Allow:
		return fmt.Sprintf("allowed-%dm", int(d.For.Minutes()))
	case d.Why == "timeout":
		return "timeout"
	case d.Why != "" && d.Why != "denied":
		return "denied"
	}
	return "denied"
}

// Summary is the request in a sentence, for the dialog and the panel.
func (r Request) Summary() string {
	who := ClientName(r.Client)
	keys := strings.Join(r.Keys, "、")
	if r.Action == "reveal" {
		return fmt.Sprintf("%s%s请求读取 %s 的明文值", who, Sep(who), keys)
	}
	cmd := r.Command
	if len([]rune(cmd)) > 80 {
		cmd = string([]rune(cmd)[:79]) + "…"
	}
	if r.Plain {
		return fmt.Sprintf("%s%s请求用 %s 运行（输出不脱敏）：%s", who, Sep(who), keys, cmd)
	}
	return fmt.Sprintf("%s%s请求用 %s 运行：%s", who, Sep(who), keys, cmd)
}

// ClientName shows a client: "mcp:claude-code" as "Claude Code".
func ClientName(c string) string {
	n := strings.TrimPrefix(c, "mcp:")
	switch strings.ToLower(n) {
	case "claude-code", "claude code":
		return "Claude Code"
	case "cursor", "cursor-vscode":
		return "Cursor"
	case "cli":
		return "命令行"
	case "app":
		return "KeyBar"
	case "", "unknown":
		return "未知代理"
	}
	return n
}

// Grants are the keys a client may use without asking, until a time.
type Grants struct {
	mu sync.Mutex
	m  map[string]time.Time // client \x00 key → until
}

func grantKey(client, key string) string { return client + "\x00" + key }

// Add lets client use keys until until.
func (g *Grants) Add(client string, keys []string, until time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.m == nil {
		g.m = map[string]time.Time{}
	}
	for _, k := range keys {
		g.m[grantKey(client, k)] = until
	}
}

// Covers tells whether client may use all keys at now.
func (g *Grants) Covers(client string, keys []string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(keys) == 0 {
		return false
	}
	for _, k := range keys {
		if until, ok := g.m[grantKey(client, k)]; !ok || !now.Before(until) {
			return false
		}
	}
	return true
}

// Grant is one, for the panel.
type Grant struct {
	Client, Key string
	Until       time.Time
}

// List are the grants in force at now.
func (g *Grants) List(now time.Time) []Grant {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []Grant
	for k, until := range g.m {
		if !now.Before(until) {
			delete(g.m, k)
			continue
		}
		c, key, _ := strings.Cut(k, "\x00")
		out = append(out, Grant{Client: c, Key: key, Until: until})
	}
	slices.SortFunc(out, func(a, b Grant) int { return strings.Compare(a.Client+a.Key, b.Client+b.Key) })
	return out
}

// Revoke takes back every grant, or those of key.
func (g *Grants) Revoke(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for k := range g.m {
		if _, kk, _ := strings.Cut(k, "\x00"); key == "" || kk == key {
			delete(g.m, k)
		}
	}
}

// Approver decides requests.
type Approver interface {
	Approve(r Request) Decision
}

// Func is an Approver of a function, for tests.
type Func func(Request) Decision

func (f Func) Approve(r Request) Decision { return f(r) }

// Sep is what goes between a name and the Chinese after it: a space
// after Latin ("Claude Code 请求"), nothing after Chinese ("命令行请求").
func Sep(name string) string {
	r := []rune(name)
	if len(r) > 0 && r[len(r)-1] >= 0x2E80 {
		return ""
	}
	return " "
}
