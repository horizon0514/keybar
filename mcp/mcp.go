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
	"keybar/secret"
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
			"instructions": "KeyBar keeps the user's API keys in the system keychain instead of .env files. " +
				"When a command needs API keys (tests, scripts, dev servers, deploys), call run_with_secrets with the command and the project's directory as cwd: " +
				"KeyBar finds the variables the project uses (names in .env.example / .env.sample / .env, from cwd up to the git root) and injects the keys it holds under those names, " +
				"after the user approves, and returns the output with every value replaced by [REDACTED:NAME]. Pass keys to choose them yourself; list_keys shows what exists (no values). " +
				"Do not ask the user to paste keys or to create .env files. Avoid request_secret: it puts a raw value into the conversation.",
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
			"name": "run_with_secrets", "title": "Run a command with the user's API keys",
			"description": "The way to use the user's API keys: run a command with them as environment variables, after the user approves in KeyBar. " +
				"Returns the exit code, stdout and stderr, with every secret value replaced by [REDACTED:NAME]; the values never reach you. " +
				"Without `keys`, KeyBar injects what the project needs: each variable named in .env.example / .env.sample / .env (from cwd up to the git root) that KeyBar holds a key of that name for (a keys.toml can map names). " +
				"Set `keys` to choose the keys yourself (each injected under its own name; see list_keys).",
			"inputSchema": schema(map[string]any{
				"command":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Program and arguments, e.g. [\"npm\",\"test\"]; a single string runs through the shell"},
				"cwd":             str("The project's directory (absolute): where the command runs and the keys are found"),
				"keys":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional: key names to inject, each under its own name. Leave out to use what the project needs"},
				"timeout_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 600, "description": "Default 120"},
				"reason":          str("Why the keys are needed; shown to the user"),
			}, "command"),
			"annotations": map[string]any{"destructiveHint": true, "openWorldHint": true},
		},
		{
			"name": "list_keys", "title": "List keys",
			"description": "List the API keys KeyBar manages: names, descriptions, tags, the projects using them, where else they are stored, rotation status. Never returns values.",
			"inputSchema": schema(map[string]any{"query": str("Filter by name, description, tag or project (optional)")}),
			"annotations": ro,
		},
		{
			"name": "request_secret", "title": "Reveal a key's raw value (discouraged)",
			"description": "Discouraged: returns one key's raw value into this conversation, where it can leak into logs, files and transcripts. " +
				"Use run_with_secrets instead: it runs your command with the key and you never see the value. " +
				"Only when the user explicitly asks for the value itself, and after they approve. Never print, log, store or commit it.",
			"inputSchema": schema(map[string]any{"name": str("The key's name"), "reason": str("Why run_with_secrets cannot do; shown to the user")}, "name", "reason"),
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
	return s.approveFill(action, keys, command, cwd, reason, nil, "")
}

// approveFill is approve offering, in the app, to add the names missing.
func (s *Server) approveFill(action string, keys []string, command, cwd, reason string, fill []string, root string) (approval.Decision, string) {
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
	d := s.Gate.Ask(approval.Request{Client: s.client(), Action: action, Keys: keys, Command: command, Cwd: cwd, Reason: reason, PID: os.Getpid(), Missing: fill, Project: root})
	ev.Decision = d.Code()
	ev.Keys = append(append([]string(nil), keys...), d.Added...)
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
		Env     map[string]string `json:"env"` // 0.1: variable → key
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
		return text("command 应是字符串数组，如 [\"npm\",\"test\"]，或一行命令", true)
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
	var notes, fill []string
	var root, noKeys string
	if len(env) == 0 {
		dir := cwd
		if dir == "" {
			dir, _ = os.Getwd()
		}
		inv, err := s.Vault.Inventory()
		if err != nil {
			return text(err.Error(), true)
		}
		// Keys agents may not use are left out, not asked for.
		var denied []string
		usable := func(n string) bool {
			k, ok := inv.Find(n)
			if ok && k.Agent == "deny" {
				denied = append(denied, n)
				return false
			}
			return ok
		}
		r, err := project.Resolve(dir, usable)
		if err != nil {
			return text(err.Error(), true)
		}
		env = r.Env
		for v, k := range env {
			if slices.Contains(denied, k) || r.Toml != "" && slices.Contains(r.TomlMissing, k) {
				delete(env, v)
			}
		}
		if len(denied) > 0 {
			slices.Sort(denied)
			notes = append(notes, "not injected (the user does not let agents use them): "+strings.Join(slices.Compact(denied), ", "))
		}
		var miss []string
		for _, n := range append(r.TomlMissing, r.Missing...) {
			if !slices.Contains(denied, n) {
				miss = append(miss, n)
			}
		}
		root = project.Root(dir)
		fill = secret.Fillable(miss, vault.LoadSettings(s.Vault.Dir).NeverAskIn(root))
		if len(miss) > 0 {
			notes = append(notes, "not in KeyBar, skipped: "+strings.Join(miss, ", "))
		}
		noKeys = fmt.Sprintf("在 %s 没找到可注入的密钥：这里到仓库根没有 .env.example / .env.sample / .env，或其中的变量名 KeyBar 里都没有。用 list_keys 看有哪些，再在 keys 里列出。", dir)
		if len(notes) > 0 {
			noKeys += "\n" + strings.Join(notes, "\n")
		}
		if len(env) == 0 && len(fill) == 0 {
			return text(noKeys, true)
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
	d, msg := s.approveFill("run", names, line, cwd, a.Reason, fill, root)
	if msg != "" && (len(names) > 0 || len(d.Added) == 0) {
		if len(names) == 0 && d.Why != "no-approver" && !strings.HasPrefix(d.Why, "error") {
			msg = noKeys // only an offer to add, and nothing was added
		}
		return text(msg, true)
	}
	if len(d.Added) > 0 {
		inv, err := s.Vault.Inventory()
		if err != nil {
			return text(err.Error(), true)
		}
		for _, n := range d.Added {
			if _, ok := inv.Find(n); ok && validEnv(n) {
				env[n] = n
				names = append(names, n)
			}
		}
		slices.Sort(names)
		for i, note := range notes {
			if !strings.HasPrefix(note, "not in KeyBar") {
				continue
			}
			var rest []string
			for _, n := range strings.Split(strings.TrimPrefix(note, "not in KeyBar, skipped: "), ", ") {
				if !slices.Contains(d.Added, n) {
					rest = append(rest, n)
				}
			}
			notes[i] = "added to KeyBar by the user just now: " + strings.Join(d.Added, ", ")
			if len(rest) > 0 {
				notes[i] += "\nnot in KeyBar, skipped: " + strings.Join(rest, ", ")
			}
		}
	}
	if len(names) == 0 {
		return text(noKeys, true)
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
	vars := make([]string, 0, len(env))
	for v := range env {
		vars = append(vars, v)
	}
	slices.Sort(vars)
	fmt.Fprintf(&b, "injected: %s\n", strings.Join(vars, ", "))
	for _, n := range notes {
		fmt.Fprintln(&b, n)
	}
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
