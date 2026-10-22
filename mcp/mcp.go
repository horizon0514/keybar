// Package mcp is KeyBar's MCP server, on stdio, for agents: they list the
// keys (no values), run programs with keys (values redacted from the
// output), and, last resort, ask for a value. Every use of a value needs
// the user's approval, and is logged.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"keybar/approval"
	"keybar/project"
	"keybar/runner"
	"keybar/vault"
)

// Asker decides requests: an approval.Gate.
type Asker interface {
	Ask(approval.Request) approval.Decision
}

// Server serves one agent.
type Server struct {
	Vault   *vault.Vault
	Gate    Asker
	Client  string // the agent, as named by --client or its initialize
	Version string
	Now     func() time.Time

	mu     sync.Mutex
	out    io.Writer
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// Protocol versions it speaks, the newest first.
var versions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve answers in's requests on out until in ends. Tool calls run
// concurrently, as they may wait for the user.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	s.out = out
	s.ctx, s.cancel = context.WithCancel(context.Background())
	defer s.cancel()
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var m message
		if err := json.Unmarshal(line, &m); err != nil {
			s.send(message{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}
		if m.Method == "" { // a response to us: we ask nothing
			continue
		}
		if m.Method == "tools/call" && m.ID != nil {
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.reply(m)
			}()
			continue
		}
		s.reply(m)
	}
	s.wg.Wait()
	return sc.Err()
}

func (s *Server) send(m message) {
	m.JSONRPC = "2.0"
	b, _ := json.Marshal(m)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.out.Write(append(b, '\n'))
}

func (s *Server) reply(m message) {
	res, err := s.handle(m)
	if m.ID == nil { // a notification
		return
	}
	if err != nil {
		s.send(message{ID: m.ID, Error: err})
		return
	}
	s.send(message{ID: m.ID, Result: res})
}

func (s *Server) handle(m message) (any, *rpcError) {
	switch m.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
			ClientInfo      struct {
				Name string `json:"name"`
			} `json:"clientInfo"`
		}
		_ = json.Unmarshal(m.Params, &p)
		v := versions[0]
		if slices.Contains(versions, p.ProtocolVersion) {
			v = p.ProtocolVersion
		}
		if s.Client == "" {
			s.Client = clean(p.ClientInfo.Name)
		}
		return map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "keybar", "title": "KeyBar", "version": s.Version},
			"instructions": "KeyBar keeps the user's API keys in the system keychain. Use list_keys to see what exists (names and metadata only). " +
				"To use keys, call run_with_secrets: it runs a command with the keys as environment variables and returns its output with the values redacted. " +
				"Each use needs the user's approval, which may take a while. Use request_secret only when a raw value is unavoidable; never print, store or commit a value.",
		}, nil
	case "notifications/initialized", "notifications/cancelled":
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": tools()}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return nil, &rpcError{-32602, "invalid params"}
		}
		switch p.Name {
		case "list_keys":
			return s.listKeys(p.Arguments), nil
		case "run_with_secrets":
			return s.runWithSecrets(p.Arguments), nil
		case "request_secret":
			return s.requestSecret(p.Arguments), nil
		}
		return nil, &rpcError{-32602, "unknown tool " + p.Name}
	}
	return nil, &rpcError{-32601, "method not found: " + m.Method}
}

func clean(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		if r == ' ' {
			return '-'
		}
		if r < 0x20 {
			return -1
		}
		return r
	}, name)
}

func (s *Server) client() string { return "mcp:" + clean(s.Client) }

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func schema(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func tools() []map[string]any {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	ro := map[string]any{"readOnlyHint": true, "openWorldHint": false}
	return []map[string]any{
		{
			"name": "list_keys", "title": "List keys",
			"description": "List the API keys KeyBar manages: names, descriptions, tags, the projects using them, where else they are stored, rotation status. Never returns values.",
			"inputSchema": schema(map[string]any{"query": str("Filter by name, description, tag or project (optional)")}),
			"annotations": ro,
		},
		{
			"name": "run_with_secrets", "title": "Run a command with keys",
			"description": "Run a command with KeyBar keys injected as environment variables, after the user approves. Returns exit code, stdout and stderr, with every secret value replaced by [REDACTED:NAME]. " +
				"Give the keys as `keys` (env var = key name), as `env` (env var -> key name), or use the project's keys.toml (`project`, or found from `cwd`). Prefer this over request_secret.",
			"inputSchema": schema(map[string]any{
				"command":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Program and arguments, e.g. [\"npm\",\"test\"]; a single string runs through the shell"},
				"keys":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Key names, each injected under its own name"},
				"env":             map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Env var name -> KeyBar key name"},
				"project":         str("Path to a keys.toml (optional; else found from cwd when neither keys nor env is given)"),
				"cwd":             str("Working directory (absolute)"),
				"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 600, "description": "Default 120"},
				"reason":          str("Why the keys are needed; shown to the user"),
			}, "command"),
			"annotations": map[string]any{"destructiveHint": true, "openWorldHint": true},
		},
		{
			"name": "request_secret", "title": "Reveal a key's value",
			"description": "Return one key's raw value, after the user approves. Last resort: prefer run_with_secrets, which never exposes values. Never print, log, store or commit the value.",
			"inputSchema": schema(map[string]any{"name": str("The key's name"), "reason": str("Why the raw value is needed; shown to the user")}, "name"),
			"annotations": map[string]any{"readOnlyHint": true, "openWorldHint": false},
		},
	}
}

func text(s string, isErr bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": s}}, "isError": isErr}
}

// keyInfo is a key as agents see it.
type keyInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Projects    []string `json:"projects,omitempty"`
	AlsoStored  []string `json:"alsoStoredIn,omitempty"`
	Status      string   `json:"status"`
	LastRotated string   `json:"lastRotated,omitempty"`
	RotateBy    string   `json:"rotateBy,omitempty"`
	Expires     string   `json:"expires,omitempty"`
	AgentAccess string   `json:"agentAccess"`
}

func (s *Server) listKeys(args json.RawMessage) any {
	var a struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal(args, &a)
	inv, err := s.Vault.Inventory()
	if err != nil {
		return text(err.Error(), true)
	}
	q := strings.ToLower(a.Query)
	now := s.now()
	out := []keyInfo{}
	for _, k := range inv.Keys {
		hay := strings.ToLower(strings.Join(append(append([]string{k.Name, k.Description}, k.Tags...), k.Projects...), " "))
		if q != "" && !strings.Contains(hay, q) {
			continue
		}
		st := inv.StatusOf(k, now)
		ki := keyInfo{Name: k.Name, Description: k.Description, Tags: k.Tags, Projects: k.Projects, Status: st.State.String(), AgentAccess: "ask"}
		if k.Agent == "deny" {
			ki.AgentAccess = "denied"
		}
		for _, p := range k.Platforms {
			ki.AlsoStored = append(ki.AlsoStored, p.Label())
		}
		if !k.LastRotated.IsZero() {
			ki.LastRotated = k.LastRotated.Format(time.DateOnly)
		}
		if !st.Due.IsZero() {
			ki.RotateBy = st.Due.Format(time.DateOnly)
		}
		if !k.Expires.IsZero() {
			ki.Expires = k.Expires.Format(time.DateOnly)
		}
		out = append(out, ki)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	_ = s.Vault.Log(vault.Event{Client: s.client(), Action: "list", Decision: "ok"})
	return text(string(b), false)
}

// approve asks for keys: those the inventory has, which agents may use.
func (s *Server) approve(action string, keys []string, command, cwd, reason string) (approval.Decision, string) {
	inv, err := s.Vault.Inventory()
	if err != nil {
		return approval.Decision{}, err.Error()
	}
	var missing, denied []string
	for _, n := range keys {
		k, ok := inv.Find(n)
		switch {
		case !ok:
			missing = append(missing, n)
		case k.Agent == "deny":
			denied = append(denied, n)
		}
	}
	if len(missing) > 0 {
		return approval.Decision{}, fmt.Sprintf("KeyBar 里没有这些密钥：%s。用 list_keys 查看有哪些。", strings.Join(missing, ", "))
	}
	ev := vault.Event{Client: s.client(), Action: action, Keys: keys, Detail: command}
	if len(denied) > 0 {
		ev.Decision = "denied"
		ev.Detail = strings.TrimSpace(ev.Detail + " (policy)")
		_ = s.Vault.Log(ev)
		return approval.Decision{}, fmt.Sprintf("用户设置了不允许代理使用：%s。", strings.Join(denied, ", "))
	}
	d := s.Gate.Ask(approval.Request{Client: s.client(), Action: action, Keys: keys, Command: command, Cwd: cwd, Reason: reason, PID: os.Getpid()})
	ev.Decision = d.Code()
	_ = s.Vault.Log(ev)
	if !d.Allow {
		switch {
		case d.Why == "timeout":
			return d, "用户没有在时限内批准（已拒绝）。"
		case d.Why == "no-approver":
			return d, "无法请求批准：KeyBar 没有在运行，系统也没有可用的对话框。请让用户打开 KeyBar。"
		case strings.HasPrefix(d.Why, "error"):
			return d, "请求批准时出错：" + d.Why
		}
		return d, "用户拒绝了这次请求。不要重试，除非用户要求。"
	}
	return d, ""
}

func (s *Server) runWithSecrets(args json.RawMessage) any {
	var a struct {
		Command json.RawMessage   `json:"command"`
		Keys    []string          `json:"keys"`
		Env     map[string]string `json:"env"`
		Project string            `json:"project"`
		Cwd     string            `json:"cwd"`
		Timeout int               `json:"timeout_seconds"`
		Reason  string            `json:"reason"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return text("参数不对："+err.Error(), true)
	}
	var argv []string
	var line string
	if json.Unmarshal(a.Command, &line) == nil && line != "" {
		argv = runner.Shell(line)
	} else if json.Unmarshal(a.Command, &argv) != nil || len(argv) == 0 {
		return text("command 应是字符串数组，如 [\"npm\",\"test\"]", true)
	} else {
		line = runner.Display(argv)
	}
	cwd := a.Cwd
	if cwd != "" && !filepath.IsAbs(cwd) {
		return text("cwd 必须是绝对路径", true)
	}
	env := map[string]string{}
	for _, k := range a.Keys {
		env[k] = k
	}
	for v, k := range a.Env {
		env[v] = k
	}
	if a.Project != "" || len(env) == 0 {
		path := a.Project
		if path == "" {
			dir := cwd
			if dir == "" {
				dir, _ = os.Getwd()
			}
			p, ok := project.Find(dir)
			if !ok {
				return text("没指定 keys 或 env，也没找到 keys.toml", true)
			}
			path = p
		}
		f, err := project.Load(path)
		if err != nil {
			return text(err.Error(), true)
		}
		for v, k := range f.Env {
			if _, ok := env[v]; !ok {
				env[v] = k
			}
		}
	}
	for v := range env {
		if !validEnv(v) {
			return text("环境变量名不合法："+v, true)
		}
	}
	var names []string
	for _, k := range env {
		if !slices.Contains(names, k) {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	if _, msg := s.approve("run", names, line, cwd, a.Reason); msg != "" {
		return text(msg, true)
	}
	values, err := s.Vault.Values(names)
	if err != nil {
		return text(err.Error(), true)
	}
	inject := map[string][]byte{}
	for v, k := range env {
		inject[v] = values[k]
	}
	timeout := time.Duration(max(1, min(600, a.Timeout))) * time.Second
	if a.Timeout == 0 {
		timeout = 120 * time.Second
	}
	r := runner.Capture(s.ctx, argv, inject, values, cwd, timeout, nil)
	var b strings.Builder
	switch {
	case r.Err != nil:
		fmt.Fprintf(&b, "无法启动：%v\n", r.Err)
	case r.TimedOut:
		fmt.Fprintf(&b, "超时（%s），已终止\n", timeout)
	default:
		fmt.Fprintf(&b, "exit code: %d\n", r.Exit)
	}
	fmt.Fprintf(&b, "--- stdout ---\n%s\n--- stderr ---\n%s", r.Stdout, r.Stderr)
	return text(b.String(), r.Err != nil)
}

func validEnv(v string) bool {
	if v == "" || (v[0] >= '0' && v[0] <= '9') {
		return false
	}
	for _, r := range v {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func (s *Server) requestSecret(args json.RawMessage) any {
	var a struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Name == "" {
		return text("需要 name", true)
	}
	if _, msg := s.approve("reveal", []string{a.Name}, "", "", a.Reason); msg != "" {
		return text(msg, true)
	}
	v, err := s.Vault.Value(a.Name)
	if err != nil {
		return text(err.Error(), true)
	}
	return text(string(v), false)
}
