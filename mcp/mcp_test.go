package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"keybar/approval"
	"keybar/vault"
)

const (
	dummyA = "dummy-openai-value-1234"
	dummyB = "dummy-db-url-value-5678"
)

type fakeGate struct {
	mu    sync.Mutex
	allow bool
	asked []approval.Request
}

func (g *fakeGate) Ask(r approval.Request) approval.Decision {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked = append(g.asked, r)
	if g.allow {
		return approval.Decision{Allow: true}
	}
	return approval.Decision{Why: "denied"}
}

func setup(t *testing.T) (*Server, *fakeGate) {
	t.Helper()
	v := &vault.Vault{Dir: t.TempDir(), Secrets: &vault.Memory{}}
	_ = v.Add(vault.Key{Name: "OPENAI_API_KEY", Description: "OpenAI", Tags: []string{"ai"}, Platforms: []vault.Platform{{Kind: "github", Target: "o/r"}}}, []byte(dummyA))
	_ = v.Add(vault.Key{Name: "TIDB_URL", Description: "db"}, []byte(dummyB))
	_ = v.Add(vault.Key{Name: "VERCEL_TOKEN", Agent: "deny"}, []byte("dummy-vercel-0000"))
	g := &fakeGate{}
	return &Server{Vault: v, Gate: g, Version: "test"}, g
}

type resp struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// call sends the requests and returns the responses by id.
func call(t *testing.T, s *Server, reqs ...string) map[int]resp {
	t.Helper()
	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(strings.Join(reqs, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	got := map[int]resp{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var r resp
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("bad line %q", line)
		}
		got[r.ID] = r
	}
	return got
}

func req(id int, method string, params any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return string(b)
}

func tool(id int, name string, args any) string {
	return req(id, "tools/call", map[string]any{"name": name, "arguments": args})
}

type toolResult struct {
	Content []struct{ Text string } `json:"content"`
	IsError bool                    `json:"isError"`
}

func result(t *testing.T, r resp) toolResult {
	t.Helper()
	if r.Error != nil {
		t.Fatalf("error %+v", r.Error)
	}
	var tr toolResult
	_ = json.Unmarshal(r.Result, &tr)
	return tr
}

func TestHandshake(t *testing.T) {
	s, _ := setup(t)
	got := call(t, s,
		req(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "clientInfo": map[string]any{"name": "Claude Code"}, "capabilities": map[string]any{}}),
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		req(2, "tools/list", nil),
		req(3, "ping", nil),
		req(4, "resources/list", nil),
		`not json`,
	)
	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct{ Name string }
	}
	_ = json.Unmarshal(got[1].Result, &init)
	if init.ProtocolVersion != "2025-06-18" || init.ServerInfo.Name != "keybar" {
		t.Errorf("init %s", got[1].Result)
	}
	if s.client() != "mcp:claude-code" {
		t.Errorf("client %s", s.client())
	}
	var list struct{ Tools []struct{ Name string } }
	_ = json.Unmarshal(got[2].Result, &list)
	if len(list.Tools) != 3 {
		t.Errorf("tools %s", got[2].Result)
	}
	if got[4].Error == nil || got[4].Error.Code != -32601 {
		t.Errorf("unknown method %+v", got[4])
	}
	if got[0].Error == nil || got[0].Error.Code != -32700 {
		t.Errorf("parse error %+v", got[0])
	}
	if len(got) != 5 {
		t.Errorf("a notification was answered: %d responses", len(got))
	}
}

func TestListKeysHasNoValues(t *testing.T) {
	s, g := setup(t)
	got := call(t, s, tool(1, "list_keys", map[string]any{}), tool(2, "list_keys", map[string]any{"query": "ai"}))
	all := result(t, got[1]).Content[0].Text
	if strings.Contains(all, dummyA) || strings.Contains(all, dummyB) {
		t.Fatal("list_keys leaked a value")
	}
	if !strings.Contains(all, "OPENAI_API_KEY") || !strings.Contains(all, "GitHub Actions · o/r") || !strings.Contains(all, `"agentAccess": "denied"`) {
		t.Errorf("%s", all)
	}
	if q := result(t, got[2]).Content[0].Text; strings.Contains(q, "TIDB_URL") {
		t.Errorf("query: %s", q)
	}
	if len(g.asked) != 0 {
		t.Error("listing asked for approval")
	}
}

func TestRunWithSecrets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	s, g := setup(t)
	g.allow = true
	got := call(t, s, tool(1, "run_with_secrets", map[string]any{
		"command": []string{"sh", "-c", `echo "key=$OPENAI_API_KEY db=$DATABASE_URL"; echo "$OPENAI_API_KEY" >&2`},
		"keys":    []string{"OPENAI_API_KEY"}, "env": map[string]string{"DATABASE_URL": "TIDB_URL"},
		"reason": "tests",
	}))
	tr := result(t, got[1])
	out := tr.Content[0].Text
	if strings.Contains(out, dummyA) || strings.Contains(out, dummyB) {
		t.Fatalf("leaked: %s", out)
	}
	if !strings.Contains(out, "key=[REDACTED:OPENAI_API_KEY] db=[REDACTED:TIDB_URL]") || !strings.Contains(out, "exit code: 0") {
		t.Errorf("%s", out)
	}
	if len(g.asked) != 1 || g.asked[0].Action != "run" || strings.Join(g.asked[0].Keys, ",") != "OPENAI_API_KEY,TIDB_URL" || g.asked[0].Reason != "tests" {
		t.Errorf("asked %+v", g.asked)
	}
	evs := vault.ReadLog(s.Vault.Dir, 0, nil)
	if len(evs) != 1 || evs[0].Decision != "allowed-once" || evs[0].Client != "mcp:unknown" {
		t.Errorf("log %+v", evs)
	}
	noValues(t, s.Vault.Dir)
}

func TestRunWithProjectFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	s, g := setup(t)
	g.allow = true
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "keys.toml"), []byte("[env]\nDB = \"TIDB_URL\"\n"), 0o644)
	got := call(t, s, tool(1, "run_with_secrets", map[string]any{"command": `test -n "$DB" && echo ok-$DB`, "cwd": dir}))
	if out := result(t, got[1]).Content[0].Text; !strings.Contains(out, "ok-[REDACTED:TIDB_URL]") {
		t.Errorf("%s", out)
	}
}

func TestDenied(t *testing.T) {
	s, g := setup(t)
	got := call(t, s,
		tool(1, "run_with_secrets", map[string]any{"command": []string{"env"}, "keys": []string{"OPENAI_API_KEY"}}),
		tool(2, "request_secret", map[string]any{"name": "OPENAI_API_KEY"}),
		tool(3, "request_secret", map[string]any{"name": "VERCEL_TOKEN"}),
		tool(4, "request_secret", map[string]any{"name": "NOPE"}),
	)
	for id := 1; id <= 4; id++ {
		tr := result(t, got[id])
		if !tr.IsError || strings.Contains(tr.Content[0].Text, "dummy-") {
			t.Errorf("%d: %+v", id, tr)
		}
	}
	if len(g.asked) != 2 { // the denied-by-policy and the missing key are not asked
		t.Errorf("asked %d times", len(g.asked))
	}
	denied := 0
	for _, e := range vault.ReadLog(s.Vault.Dir, 0, nil) {
		if e.Decision == "denied" {
			denied++
		}
	}
	if denied != 3 {
		t.Errorf("%d denials logged, want 3", denied)
	}
}

func TestRequestSecretApproved(t *testing.T) {
	s, g := setup(t)
	g.allow = true
	got := call(t, s, tool(1, "request_secret", map[string]any{"name": "TIDB_URL", "reason": "migrate"}))
	if tr := result(t, got[1]); tr.IsError || tr.Content[0].Text != dummyB {
		t.Errorf("%+v", tr)
	}
	if g.asked[0].Action != "reveal" {
		t.Error(g.asked[0])
	}
}

func noValues(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		for _, v := range []string{dummyA, dummyB} {
			if bytes.Contains(b, []byte(v)) {
				t.Errorf("%s holds a value", e.Name())
			}
		}
	}
	_ = fmt.Sprint
}

func TestRunDetectsProjectKeys(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	s, g := setup(t)
	g.allow = true
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	sub := filepath.Join(repo, "web")
	_ = os.MkdirAll(sub, 0o755)
	// Names only: the example's placeholder values are not used.
	_ = os.WriteFile(filepath.Join(repo, ".env.example"), []byte("OPENAI_API_KEY=sk-placeholder\nVERCEL_TOKEN=\nPORT=3000\n"), 0o644)
	_ = os.WriteFile(filepath.Join(sub, ".env"), []byte("TIDB_URL=placeholder-not-used\n"), 0o600)
	got := call(t, s, tool(1, "run_with_secrets", map[string]any{
		"command": `echo "a=$OPENAI_API_KEY b=$TIDB_URL v=${VERCEL_TOKEN:-none}"`, "cwd": sub}))
	out := result(t, got[1]).Content[0].Text
	if !strings.Contains(out, "a=[REDACTED:OPENAI_API_KEY] b=[REDACTED:TIDB_URL] v=none") {
		t.Errorf("%s", out)
	}
	if !strings.Contains(out, "injected: OPENAI_API_KEY, TIDB_URL") || !strings.Contains(out, "skipped: PORT") || !strings.Contains(out, "does not let agents use them): VERCEL_TOKEN") {
		t.Errorf("notes: %s", out)
	}
	if len(g.asked) != 1 || strings.Join(g.asked[0].Keys, ",") != "OPENAI_API_KEY,TIDB_URL" {
		t.Errorf("asked %+v", g.asked)
	}
	// Nothing to inject: an error that says what to do, no approval asked.
	empty := t.TempDir()
	got = call(t, s, tool(2, "run_with_secrets", map[string]any{"command": "true", "cwd": empty}))
	if tr := result(t, got[2]); !tr.IsError || !strings.Contains(tr.Content[0].Text, "list_keys") || len(g.asked) != 1 {
		t.Errorf("%+v", tr)
	}
}

func TestToolsPreferRun(t *testing.T) {
	ts := tools()
	if ts[0]["name"] != "run_with_secrets" {
		t.Error("run_with_secrets is not first")
	}
	for _, x := range ts {
		if x["name"] == "request_secret" && !strings.HasPrefix(x["description"].(string), "Discouraged") {
			t.Error("request_secret does not discourage itself")
		}
	}
}

type fillGate struct {
	v     *vault.Vault
	asked []approval.Request
	add   bool
}

func (g *fillGate) Ask(r approval.Request) approval.Decision {
	g.asked = append(g.asked, r)
	if !g.add {
		return approval.Decision{Why: "denied"}
	}
	for _, n := range r.Missing {
		_ = g.v.Add(vault.Key{Name: n}, []byte("dummy-pasted-"+n))
	}
	return approval.Decision{Allow: true, Added: r.Missing}
}

func TestRunFillsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	s, _ := setup(t)
	g := &fillGate{v: s.Vault, add: true}
	s.Gate = g
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(repo, ".env.example"), []byte("OPENAI_API_KEY=\nGROQ_API_KEY=\nPORT=3000\n"), 0o644)
	got := call(t, s, tool(1, "run_with_secrets", map[string]any{"command": `echo "g=$GROQ_API_KEY"`, "cwd": repo}))
	out := result(t, got[1]).Content[0].Text
	if !strings.Contains(out, "g=[REDACTED:GROQ_API_KEY]") || !strings.Contains(out, "added to KeyBar by the user just now: GROQ_API_KEY") {
		t.Errorf("%s", out)
	}
	r := g.asked[0]
	if strings.Join(r.Keys, ",") != "OPENAI_API_KEY" || strings.Join(r.Missing, ",") != "GROQ_API_KEY" || r.Project != repo {
		t.Errorf("%+v", r)
	}
	// only missing names: still offered; declined, the usual error
	other := t.TempDir()
	_ = os.WriteFile(filepath.Join(other, ".env.example"), []byte("REPLICATE_API_TOKEN=\n"), 0o644)
	g.add = false
	got = call(t, s, tool(2, "run_with_secrets", map[string]any{"command": "true", "cwd": other}))
	if tr := result(t, got[2]); !tr.IsError || !strings.Contains(tr.Content[0].Text, "list_keys") || len(g.asked) != 2 || len(g.asked[1].Keys) != 0 {
		t.Errorf("%+v %+v", tr, g.asked)
	}
	// a name the user said never to ask for in this project
	set := vault.LoadSettings(s.Vault.Dir)
	set.AddNeverAsk(other, "REPLICATE_API_TOKEN")
	_ = vault.SaveSettings(s.Vault.Dir, set)
	call(t, s, tool(3, "run_with_secrets", map[string]any{"command": "true", "cwd": other}))
	if len(g.asked) != 2 {
		t.Error("asked for a never-ask name")
	}
}
