// Package vault is KeyBar's store: the values of the keys in the system's
// credential store (Secrets), and what is known about them, without the
// values, in inventory.json of the data folder.
package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// DefaultRotateDays is how often a key should be rotated, unless the
// inventory or the key says otherwise.
const DefaultRotateDays = 90

// Key is what KeyBar knows about a key: never its value.
type Key struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Platforms   []Platform `json:"platforms,omitempty"` // where else it is stored
	Projects    []string   `json:"projects,omitempty"`  // the programs, projects that use it
	Created     time.Time  `json:"created"`
	LastRotated time.Time  `json:"lastRotated"`
	Expires     time.Time  `json:"expires,omitzero"`
	RotateDays  int        `json:"rotateDays,omitempty"` // 0: the inventory's default; -1: never
	// Agent is what agents may do: "ask" (the default: each use needs
	// approval) or "deny" (never, even with approval).
	Agent   string     `json:"agent,omitempty"`
	History []Rotation `json:"history,omitempty"`
}

// Platform is a place the key is also stored, which a rotation must
// update: a GitHub repository's Actions secret, a Vercel project's
// environment variable, a Railway service, a project's .env, …
type Platform struct {
	ID     string `json:"id"`               // p1, p2…: stable within the key
	Kind   string `json:"kind"`             // github, vercel, railway, tidb, env, other…
	Target string `json:"target,omitempty"` // owner/repo, the Vercel project, a path…
	Var    string `json:"var,omitempty"`    // its name there; the key's when empty
	Note   string `json:"note,omitempty"`
	// Vercel: the environments (production, preview, development) and the
	// team; github and vercel: TokenKey names the KeyBar key holding the API
	// token for updating it (GITHUB_TOKEN, VERCEL_TOKEN by default).
	Envs     []string `json:"envs,omitempty"`
	Team     string   `json:"team,omitempty"`
	TokenKey string   `json:"tokenKey,omitempty"`
	// Updated is when it was last given the key's value; Stale, that it
	// still has the value before the last rotation: it is on the checklist.
	Updated time.Time `json:"updated,omitzero"`
	Old     bool      `json:"stale,omitempty"`
}

// Rotation is one change of the value.
type Rotation struct {
	At   time.Time `json:"at"`
	Note string    `json:"note,omitempty"`
	By   string    `json:"by,omitempty"` // cli, app
}

// Inventory is the file: the keys, and the defaults.
type Inventory struct {
	Version    int   `json:"version"`
	RotateDays int   `json:"rotateDays,omitempty"`
	Keys       []Key `json:"keys"`
}

// Label is the platform as a line of a checklist: "GitHub Actions · owner/repo".
func (p Platform) Label() string {
	s := KindName(p.Kind)
	if p.Target != "" {
		s += " · " + p.Target
	}
	return s
}

// VarName is the variable's name on the platform.
func (p Platform) VarName(key string) string {
	if p.Var != "" {
		return p.Var
	}
	return key
}

// Auto tells whether KeyBar can update the platform itself.
func (p Platform) Auto() bool { return p.Kind == "github" || p.Kind == "vercel" || p.Kind == "env" }

// Stale tells whether the platform still has the value before the last
// rotation.
func (p Platform) Stale(Key) bool { return p.Old }

// Pending is the checklist: the platforms not updated since the last
// rotation.
func (k Key) Pending() []Platform {
	var out []Platform
	for _, p := range k.Platforms {
		if p.Stale(k) {
			out = append(out, p)
		}
	}
	return out
}

// Platform finds a platform by its ID, or by its kind when the key has one
// of the kind.
func (k *Key) Platform(id string) (*Platform, bool) {
	for i := range k.Platforms {
		if k.Platforms[i].ID == id {
			return &k.Platforms[i], true
		}
	}
	var found *Platform
	for i := range k.Platforms {
		if k.Platforms[i].Kind == id {
			if found != nil {
				return nil, false
			}
			found = &k.Platforms[i]
		}
	}
	return found, found != nil
}

// AddPlatform adds p with a fresh ID; it counts as up to date.
func (k *Key) AddPlatform(p Platform, now time.Time) Platform {
	n := 1
	for _, q := range k.Platforms {
		var i int
		if _, err := fmt.Sscanf(q.ID, "p%d", &i); err == nil && i >= n {
			n = i + 1
		}
	}
	p.ID = fmt.Sprintf("p%d", n)
	p.Kind = strings.ToLower(strings.TrimSpace(p.Kind))
	if p.Updated.IsZero() {
		p.Updated = now
	}
	k.Platforms = append(k.Platforms, p)
	return p
}

var kindNames = map[string]string{
	"github": "GitHub Actions", "vercel": "Vercel", "railway": "Railway", "tidb": "TiDB Cloud",
	"env": ".env", "aws": "AWS", "gcp": "Google Cloud", "cloudflare": "Cloudflare", "fly": "Fly.io",
	"netlify": "Netlify", "render": "Render", "supabase": "Supabase", "1password": "1Password",
	"k8s": "Kubernetes", "docker": "Docker", "other": "其他",
}

// KindName is how the kind is shown.
func KindName(kind string) string {
	if n, ok := kindNames[kind]; ok {
		return n
	}
	return kind
}

// ParsePlatform reads "kind:target[:VAR][?options]", as the CLI takes
// it: github:owner/repo, vercel:my-app?env=production,preview&team=t_1,
// env:~/proj/.env, railway:api. Options: env (Vercel's environments, or a
// GitHub environment), team (Vercel), token (the key holding the API
// token), note.
func ParsePlatform(s string) (Platform, error) {
	kind, rest, _ := strings.Cut(strings.TrimSpace(s), ":")
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		return Platform{}, fmt.Errorf("平台格式是 kind:target，如 github:owner/repo、vercel:my-app、railway:api")
	}
	rest, opts, _ := strings.Cut(rest, "?")
	p := Platform{Kind: kind, Target: rest}
	if kind != "env" { // a path may hold colons (C:\…)
		if t, v, ok := strings.Cut(rest, ":"); ok {
			p.Target, p.Var = t, v
		}
	}
	for _, kv := range strings.Split(opts, "&") {
		if kv == "" {
			continue
		}
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "env", "envs":
			p.Envs = Clean(strings.Split(v, ","))
		case "team":
			p.Team = v
		case "token":
			p.TokenKey = v
		case "note":
			p.Note = v
		default:
			return p, fmt.Errorf("不认识的平台选项 %q（可用 env、team、token、note）", k)
		}
	}
	if kind == "github" && p.Target != "" && !strings.Contains(p.Target, "/") {
		return p, fmt.Errorf("GitHub 的目标应是 owner/repo：%q", p.Target)
	}
	return p, nil
}

var nameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\-]{0,127}$`)

// CheckName checks a key's name: letters, digits, _ . -, as env names
// are, starting with a letter or _.
func CheckName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("密钥名 %q 不合法：只能用字母、数字、_ . -，以字母或 _ 开头，最长 128", name)
	}
	return nil
}

// RotateEvery is the key's interval, 0 for never.
func (inv *Inventory) RotateEvery(k Key) time.Duration {
	d := k.RotateDays
	if d == 0 {
		d = inv.RotateDays
	}
	if d == 0 {
		d = DefaultRotateDays
	}
	if d < 0 {
		return 0
	}
	return time.Duration(d) * 24 * time.Hour
}

// Find returns the key named name.
func (inv *Inventory) Find(name string) (*Key, bool) {
	for i := range inv.Keys {
		if inv.Keys[i].Name == name {
			return &inv.Keys[i], true
		}
	}
	return nil, false
}

// Names are the keys' names.
func (inv *Inventory) Names() []string {
	out := make([]string, len(inv.Keys))
	for i, k := range inv.Keys {
		out[i] = k.Name
	}
	return out
}

func (inv *Inventory) sort() {
	slices.SortFunc(inv.Keys, func(a, b Key) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
}

const inventoryFile = "inventory.json"

// ErrBusy is another KeyBar holding the inventory's lock too long.
var ErrBusy = errors.New("inventory.json 被另一个 KeyBar 进程占用，请稍后重试")

func loadInventory(dir string) (*Inventory, error) {
	inv := &Inventory{Version: 1}
	b, err := os.ReadFile(filepath.Join(dir, inventoryFile))
	if errors.Is(err, os.ErrNotExist) {
		return inv, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, inv); err != nil {
		return nil, fmt.Errorf("inventory.json 损坏：%w", err)
	}
	return inv, nil
}

func saveInventory(dir string, inv *Inventory) error {
	inv.Version = 1
	inv.sort()
	b, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, inventoryFile), append(b, '\n'))
}

func writeFileAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Chmod(tmp, 0o600)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// lock takes the data folder's lock, for a read-modify-write of the
// inventory between the CLI, MCP servers and the app: a file made
// exclusively, taken over when older than staleLock.
func lock(dir string) (func(), error) {
	const staleLock = 10 * time.Second
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "inventory.lock")
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) > staleLock {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, ErrBusy
		}
		time.Sleep(20 * time.Millisecond)
	}
}
