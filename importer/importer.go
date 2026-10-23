// Package importer brings a .env file's keys into KeyBar, deciding what to
// do with names KeyBar already has: the same value is skipped; a
// different one is overwritten (a rotation), kept, or imported under a
// new name. Values are compared, never shown.
package importer

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"keybar/project"
	"keybar/secret"
	"keybar/vault"
)

// State is what KeyBar has for a variable's name.
type State int

const (
	New      State = iota // no key of that name: added
	Same                  // a key of that name with the same value: skipped
	Conflict              // a key of that name with another value: Choice
	Empty                 // the value is empty: skipped
)

// Choice is what to do with a Conflict.
type Choice int

const (
	Keep      Choice = iota // keep KeyBar's value; the file's is not imported
	Overwrite               // the file's value replaces it: a rotation
	Rename                  // import under NewName
)

// ParseChoice reads skip|keep, overwrite, rename.
func ParseChoice(s string) (Choice, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "skip", "keep", "k", "":
		return Keep, nil
	case "overwrite", "o":
		return Overwrite, nil
	case "rename", "r":
		return Rename, nil
	}
	return Keep, fmt.Errorf("--on-conflict 是 skip、overwrite 或 rename：%q", s)
}

// Item is one variable of the file.
type Item struct {
	Var     string // its name in the file
	Key     string // the KeyBar key it goes to by default: the prefix and Var
	Line    int    // the line it was read from: the last, when repeated
	Lines   []int  // every line it is on, when repeated
	State   State
	Choice  Choice
	NewName string // for Rename
	// Skip: left out (unchecked in a scan's list).
	Skip bool
	// Likely: the value looks like a secret (a scan checks these).
	Likely bool
	// Masked is the value as its prefix and length: sk-… 51 位.
	Masked string
	// Elsewhere: another file of the same scan has this name; Same then
	// means the same value as there.
	Elsewhere string
	value     []byte
}

// Repeated tells whether the variable is on several lines of the file.
func (it *Item) Repeated() bool { return len(it.Lines) > 1 }

// Target is the key the item ends up in, or "" when nothing is imported.
func (it *Item) Target() string {
	if it.Skip {
		return ""
	}
	switch it.State {
	case New, Same:
		return it.Key
	case Conflict:
		switch it.Choice {
		case Overwrite:
			return it.Key
		case Rename:
			return it.NewName
		}
	}
	return ""
}

// Plan is a file read and compared with KeyBar, its conflicts to decide.
type Plan struct {
	Path    string
	Project string
	Items   []*Item
}

// Conflicts are the items to decide.
func (p *Plan) Conflicts() []*Item {
	var out []*Item
	for _, it := range p.Items {
		if it.State == Conflict {
			out = append(out, it)
		}
	}
	return out
}

// Repeated are the variables on several lines.
func (p *Plan) Repeated() []*Item {
	var out []*Item
	for _, it := range p.Items {
		if it.Repeated() {
			out = append(out, it)
		}
	}
	return out
}

// ProjectName is the folder's name, as the project's.
func ProjectName(path string) string {
	abs, _ := filepath.Abs(path)
	return filepath.Base(filepath.Dir(abs))
}

// Read reads path and compares its variables with v's keys. project names
// the project (the folder's name when empty), prefix goes before every
// name.
func Read(v *vault.Vault, path, proj, prefix string) (*Plan, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	vars, err := project.ParseDotenv(b)
	if err != nil {
		return nil, fmt.Errorf("%s：%w", abs, err)
	}
	if len(vars) == 0 {
		return nil, errors.New("文件里没有变量")
	}
	return FromVars(v, abs, proj, prefix, vars)
}

// FromVars is Read of variables read elsewhere (a shell's rc file).
func FromVars(v *vault.Vault, abs, proj, prefix string, vars []project.Var) (*Plan, error) {
	if proj == "" {
		proj = ProjectName(abs)
	}
	p := &Plan{Path: abs, Project: proj}
	byVar := map[string]*Item{}
	for _, x := range vars { // the last one wins, as dotenv loaders do
		it := byVar[x.Name]
		if it == nil {
			it = &Item{Var: x.Name, Key: prefix + x.Name}
			byVar[x.Name] = it
			p.Items = append(p.Items, it)
		}
		it.Line, it.value = x.Line, []byte(x.Value)
		it.Lines = append(it.Lines, x.Line)
		it.Likely, it.Masked = secret.Likely(x.Name, x.Value), secret.Mask(x.Value)
	}
	inv, err := v.Inventory()
	if err != nil {
		return nil, err
	}
	for _, it := range p.Items {
		if err := vault.CheckName(it.Key); err != nil {
			return nil, err
		}
		switch {
		case len(bytes.TrimSpace(it.value)) == 0:
			it.State = Empty
		default:
			if _, ok := inv.Find(it.Key); !ok {
				it.State = New
				continue
			}
			same, err := sameValue(v, it.Key, it.value)
			if err != nil {
				return nil, err
			}
			if same {
				it.State = Same
			} else {
				it.State = Conflict
				it.NewName = Suggest(inv, it.Key, proj, p.taken(it))
			}
		}
	}
	return p, nil
}

func sameValue(v *vault.Vault, name string, value []byte) (bool, error) {
	old, err := v.Value(name)
	if err != nil {
		return false, fmt.Errorf("读取 %s 以比较：%w", name, err)
	}
	return bytes.Equal(old, value), nil
}

// CheckName checks a new name for it: valid, not another item's, and not
// a key KeyBar holds another value in.
func (p *Plan) CheckName(v *vault.Vault, it *Item, name string) error {
	if err := vault.CheckName(name); err != nil {
		return err
	}
	if name == it.Key {
		return errors.New("这就是原来的名字：要替换它请选“覆盖”")
	}
	if slices.Contains(p.taken(it), name) {
		return fmt.Errorf("%s 是这个文件里另一个变量要用的名字", name)
	}
	inv, err := v.Inventory()
	if err != nil {
		return err
	}
	if _, ok := inv.Find(name); ok {
		if same, err := sameValue(v, name, it.value); err != nil || !same {
			return fmt.Errorf("KeyBar 里已有 %s（值不同），换一个名字", name)
		}
	}
	return nil
}

// taken are the names the plan's other items go to.
func (p *Plan) taken(except *Item) []string {
	var out []string
	for _, it := range p.Items {
		if it != except {
			out = append(out, it.Key)
			if it.NewName != "" {
				out = append(out, it.NewName)
			}
		}
	}
	return out
}

// Sanitize makes a project's name a name's suffix: MY_APP.
func Sanitize(s string) string {
	var b strings.Builder
	under := false
	for _, r := range strings.ToUpper(s) {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
			under = false
		} else if !under && b.Len() > 0 {
			b.WriteByte('_')
			under = true
		}
	}
	return strings.Trim(b.String(), "_")
}

// Suggest is a new name for a key whose name is taken: NAME_PROJECT, then
// NAME_PROJECT_2…, free in inv and not in taken.
func Suggest(inv *vault.Inventory, name, proj string, taken []string) string {
	suffix := Sanitize(proj)
	if suffix == "" {
		suffix = "IMPORTED"
	}
	base := name + "_" + suffix
	if len(base) > 120 {
		base = base[:120]
	}
	free := func(n string) bool {
		_, ok := inv.Find(n)
		return !ok && !slices.Contains(taken, n)
	}
	if free(base) {
		return base
	}
	for i := 2; ; i++ {
		if n := base + "_" + strconv.Itoa(i); free(n) {
			return n
		}
	}
}

// Result is what Apply did.
type Result struct {
	Added, Same, Overwritten, Renamed, Kept, Empty, Unchecked int
	Failed                                                    []string // "NAME：why"
	// Env maps the file's variables to the keys holding their values;
	// Mapped, those whose key has another name (renamed or prefixed),
	// which a keys.toml must record for keys run to find them.
	Env    map[string]string
	Mapped map[string]string
	// Pending are the overwritten keys that other platforms still hold
	// the old value of.
	Pending []string
}

// Imported counts the variables whose value KeyBar now holds.
func (r Result) Imported() int { return r.Added + r.Same + r.Overwritten + r.Renamed }

// Complete tells whether every value of the file is in KeyBar.
func (r Result) Complete() bool { return r.Kept == 0 && r.Empty == 0 && len(r.Failed) == 0 }

// Summary is the result in a line.
func (r Result) Summary() string {
	var s []string
	add := func(n int, what string) {
		if n > 0 {
			s = append(s, fmt.Sprintf("%s %d 个", what, n))
		}
	}
	add(r.Added, "新增")
	add(r.Overwritten, "覆盖")
	add(r.Renamed, "改名导入")
	add(r.Same, "已有相同值")
	add(r.Kept, "保留现有")
	add(r.Empty, "空值跳过")
	add(r.Unchecked, "未勾选")
	add(len(r.Failed), "失败")
	if len(s) == 0 {
		return "没有导入任何密钥"
	}
	return strings.Join(s, "，")
}

// Apply carries the plan out.
func (p *Plan) Apply(v *vault.Vault) Result {
	r := Result{Env: map[string]string{}, Mapped: map[string]string{}}
	desc := "从 " + shortPath(p.Path) + " 导入"
	addProject := func(name string) {
		_ = v.Edit(name, func(k *vault.Key) error { k.Projects = vault.Clean(append(k.Projects, p.Project)); return nil })
	}
	got := func(it *Item, key string) {
		r.Env[it.Var] = key
		if key != it.Var {
			r.Mapped[it.Var] = key
		}
	}
	fail := func(it *Item, name string, err error) { r.Failed = append(r.Failed, name+"："+err.Error()) }
	for _, it := range p.Items {
		if it.Skip {
			r.Unchecked++
			continue
		}
		switch it.State {
		case Empty:
			r.Empty++
		case Same:
			r.Same++
			addProject(it.Key)
			got(it, it.Key)
		case New:
			if err := v.Add(vault.Key{Name: it.Key, Projects: []string{p.Project}, Description: desc}, it.value); err != nil {
				fail(it, it.Key, err)
				continue
			}
			r.Added++
			got(it, it.Key)
		case Conflict:
			switch it.Choice {
			case Keep:
				r.Kept++
			case Overwrite:
				k, err := v.Rotate(it.Key, it.value, desc+"（覆盖）")
				if err != nil {
					fail(it, it.Key, err)
					continue
				}
				addProject(it.Key)
				r.Overwritten++
				got(it, it.Key)
				if len(k.Pending()) > 0 {
					r.Pending = append(r.Pending, it.Key)
				}
			case Rename:
				name := strings.TrimSpace(it.NewName)
				if err := vault.CheckName(name); err != nil {
					fail(it, it.Var, err)
					continue
				}
				inv, err := v.Inventory()
				if err != nil {
					fail(it, name, err)
					continue
				}
				if _, ok := inv.Find(name); ok {
					same, err := sameValue(v, name, it.value)
					switch {
					case err != nil:
						fail(it, name, err)
					case same:
						r.Same++
						addProject(name)
						got(it, name)
					default:
						fail(it, name, errors.New("这个名字也已有别的值"))
					}
					continue
				}
				if err := v.Add(vault.Key{Name: name, Projects: []string{p.Project}, Description: desc + "（原名 " + it.Var + "）"}, it.value); err != nil {
					fail(it, name, err)
					continue
				}
				r.Renamed++
				got(it, name)
			}
		}
	}
	return r
}

// WriteMapping records the renamed variables in the keys.toml next to the
// file, merged with what it had, so that keys run gives each variable its
// key. It returns the file's path, or "" when nothing needed it.
func (p *Plan) WriteMapping(r Result) (string, error) {
	if len(r.Mapped) == 0 {
		return "", nil
	}
	path := filepath.Join(filepath.Dir(p.Path), project.FileName)
	env := map[string]string{}
	if old, err := project.Load(path); err == nil {
		for k, v := range old.Env {
			env[k] = v
		}
	}
	for k, v := range r.Mapped {
		env[k] = v
	}
	return path, project.Write(path, p.Project, env)
}

func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// Pending is what the earlier files of a scan will add: name → value.
type Pending map[string][]byte

// Against marks the plan's new names that an earlier file of the same
// scan adds too: the same value is that one (Same); another is imported
// under NAME_PROJECT by default. It then records the plan's own new names.
func (p *Plan) Against(pend Pending, from map[string]string, v *vault.Vault) {
	inv, _ := v.Inventory()
	for _, it := range p.Items {
		if it.State != New || it.Skip {
			continue
		}
		old, ok := pend[it.Key]
		if !ok {
			continue
		}
		it.Elsewhere = from[it.Key]
		if bytes.Equal(old, it.value) {
			it.State = Same
			continue
		}
		it.State, it.Choice = Conflict, Rename
		if inv != nil {
			it.NewName = Suggest(inv, it.Key, p.Project, append(p.taken(it), keysOf(pend)...))
		}
	}
	for _, it := range p.Items {
		if it.State == New && !it.Skip {
			pend[it.Key] = it.value
			from[it.Key] = p.Path
		}
	}
}

func keysOf(m Pending) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Recheck brings the states up to date with KeyBar just before Apply,
// after earlier files of a scan were imported: a name now there with
// the same value is Same, with another a Conflict (renamed by default);
// one no longer there is New.
func (p *Plan) Recheck(v *vault.Vault) error {
	inv, err := v.Inventory()
	if err != nil {
		return err
	}
	for _, it := range p.Items {
		if it.Skip || it.State == Empty {
			continue
		}
		_, there := inv.Find(it.Key)
		switch {
		case !there && (it.State == Same || it.State == Conflict && it.Elsewhere != ""):
			it.State = New
		case there && it.State == New, there && it.State == Same && it.Elsewhere != "":
			same, err := sameValue(v, it.Key, it.value)
			if err != nil {
				return err
			}
			if same {
				it.State = Same
			} else {
				it.State, it.Choice = Conflict, Rename
				if it.NewName == "" {
					it.NewName = Suggest(inv, it.Key, p.Project, p.taken(it))
				}
			}
		}
	}
	return nil
}

// Valued are the items whose value is not empty.
func (p *Plan) Valued() []*Item {
	var out []*Item
	for _, it := range p.Items {
		if it.State != Empty {
			out = append(out, it)
		}
	}
	return out
}

// AttachEnvPlatforms records the .env file on each imported key so
// rotation can write the new value back (Kind env, Auto). Shell rc
// imports must not call this. path is the absolute file; it is stored
// as ~/… when under the home directory. When the KeyBar name differs
// from the variable (rename), Platform.Var keeps the file's name.
func AttachEnvPlatforms(v *vault.Vault, path string, r Result) {
	if len(r.Env) == 0 || path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	target := shortPath(abs)
	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}
	seen := map[string]bool{}
	for envName, keyName := range r.Env {
		if seen[keyName] {
			// two vars mapped to one key: keep the first Var; rare
			continue
		}
		seen[keyName] = true
		_ = v.Edit(keyName, func(k *vault.Key) error {
			for i := range k.Platforms {
				p := &k.Platforms[i]
				if p.Kind != "env" {
					continue
				}
				if !sameEnvTarget(p.Target, abs, target) {
					continue
				}
				if keyName != envName {
					p.Var = envName
				} else {
					p.Var = ""
				}
				p.Updated, p.Old = now, false
				return nil
			}
			p := vault.Platform{Kind: "env", Target: target}
			if keyName != envName {
				p.Var = envName
			}
			k.AddPlatform(p, now)
			return nil
		})
	}
}

func sameEnvTarget(recorded, abs, tilde string) bool {
	if recorded == abs || recorded == tilde {
		return true
	}
	if a, err := filepath.Abs(expandHomeQuick(recorded)); err == nil && filepath.Clean(a) == filepath.Clean(abs) {
		return true
	}
	return false
}

func expandHomeQuick(t string) string {
	t = strings.TrimSpace(t)
	if t == "~" || strings.HasPrefix(t, "~/") || strings.HasPrefix(t, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if t == "~" {
				return home
			}
			return home + t[1:]
		}
	}
	return t
}
