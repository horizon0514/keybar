package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"keybar/clip"
	"keybar/importer"
	"keybar/scan"
	"keybar/secret"
	"keybar/vault"
)

// 自动录入: the keys copied, the .env files that appear, the machine's
// scan, the names a run misses.

type autoState struct {
	board    *clip.Watch
	onMain   bool // read the board on the UI thread (macOS)
	noSeq    bool // no change counter: read every other second
	clipBusy bool
	found    *clipFound

	watcher   *scan.Watcher
	watchBusy bool
	envNotes  []envNote

	scan      scanning
	typing    bool // the notice's name has the focus
	editRoots string
	rootsSet  bool
}

// clipFound is a key seen on the clipboard: kept in memory only, until
// saved, dismissed or two minutes old.
type clipFound struct {
	provider, name string
	masked         string
	value          []byte
	at             time.Time
}

// envNote: a .env new or changed under the roots, with keys KeyBar lacks.
type envNote struct {
	path string
	n    int
}

// scanning is the scan page.
type scanning struct {
	running bool
	file    string // one file (from a notice), else the machine
	groups  []scan.Group
	clean   map[string]bool
	mode    map[string]scan.Mode
	err     string
	typing  bool
}

const (
	clipEvery  = time.Second
	clipKeep   = 2 * time.Minute
	watchEvery = 20 * time.Second
)

// startAuto starts the clipboard and .env watching; b nil: no clipboard.
func (a *app) startAuto(b clip.Board, onMain bool) {
	if b != nil {
		_, hasSeq := b.Seq()
		a.auto.board, a.auto.onMain, a.auto.noSeq = &clip.Watch{B: b}, onMain, !hasSeq
		a.later(clipEvery, a.clipTick)
	}
	a.auto.watcher = scan.NewWatcher(a.scanOptions())
	a.later(5*time.Second, a.envTick)
}

func (a *app) scanOptions() scan.Options {
	home, _ := os.UserHomeDir()
	if a.agents != nil && a.agents.Home != "" {
		home = a.agents.Home
	}
	return scan.Options{Home: home, Roots: a.settings.ScanRoots, Ignored: a.settings.IgnoredFiles}
}

func (a *app) clipTick() {
	every := clipEvery
	if a.auto.noSeq {
		every = 2 * clipEvery
	}
	defer a.later(every, a.clipTick)
	if f := a.auto.found; f != nil && a.now().Sub(f.at) > clipKeep {
		a.dismissClip()
	}
	if !a.settings.Clipboard || a.auto.board == nil || a.auto.clipBusy {
		return
	}
	if a.auto.onMain {
		a.offerClip(a.auto.board.Next())
		return
	}
	a.auto.clipBusy = true
	w := a.auto.board
	a.spawn(func() {
		text := w.Next()
		a.post(func() {
			a.auto.clipBusy = false
			a.offerClip(text)
		})
	})
}

// offerClip looks at text copied: a key of a known provider that KeyBar
// does not have is offered for saving. Nothing else is kept.
func (a *app) offerClip(text string) {
	if text == "" || !a.settings.Clipboard {
		return
	}
	p, ok := secret.Detect(text)
	if !ok || a.haveValue([]byte(text)) {
		return
	}
	a.dismissClip()
	name := a.freeName(p.Var)
	a.auto.found = &clipFound{provider: p.Name, name: name, masked: secret.Mask(text), value: []byte(text), at: a.now()}
	a.notify("clipboard", "发现 "+p.Name+" key", "保存为 "+name+"：打开 KeyBar（值不会显示）")
	a.trayChanged()
}

// haveValue tells whether a key in KeyBar has this value.
func (a *app) haveValue(v []byte) bool {
	var names []string
	for _, k := range a.inv.Keys {
		names = append(names, k.Name)
	}
	if len(names) == 0 {
		return false
	}
	vals, err := a.v.Values(names)
	if err != nil {
		return false
	}
	found := false
	for _, x := range vals {
		if bytes.Equal(x, v) {
			found = true
		}
		clear(x)
	}
	return found
}

// freeName is name, or name_2, _3… when KeyBar has it.
func (a *app) freeName(name string) string {
	if _, ok := a.key(name); !ok {
		return name
	}
	for i := 2; ; i++ {
		n := fmt.Sprintf("%s_%d", name, i)
		if _, ok := a.key(n); !ok {
			return n
		}
	}
}

func (a *app) dismissClip() {
	if f := a.auto.found; f != nil {
		clear(f.value)
		a.auto.found = nil
		a.trayChanged()
	}
}

// saveClip saves the key copied under the name shown.
func (a *app) saveClip() {
	f := a.auto.found
	if f == nil {
		return
	}
	name := strings.TrimSpace(f.name)
	if err := vault.CheckName(name); err != nil {
		a.say(err.Error())
		return
	}
	if _, ok := a.key(name); ok {
		a.say(name + " 已经有了：换个名字")
		return
	}
	if err := a.v.Add(vault.Key{Name: name, Description: f.provider + " key（从剪贴板保存）"}, f.value); err != nil {
		a.say("保存失败：" + err.Error())
		return
	}
	_ = a.v.Log(vault.Event{Client: "app", Action: "clipboard", Keys: []string{name}, Decision: "ok", Detail: f.provider})
	a.dismissClip()
	a.reload()
	a.say("已保存 " + name + "，值在" + storeShort())
}

// envTick looks for new .env files under the roots.
func (a *app) envTick() {
	defer a.later(watchEvery, a.envTick)
	if !a.settings.WatchEnv || a.auto.watcher == nil || a.auto.watchBusy {
		return
	}
	w := a.auto.watcher
	o := a.scanOptions()
	w.Opts.Roots, w.Opts.Ignored, w.Opts.Home = o.Roots, o.Ignored, o.Home
	a.auto.watchBusy = true
	v := a.v
	a.spawn(func() {
		var notes []envNote
		for _, p := range w.Tick() {
			if n := unmanaged(v, p); n > 0 {
				notes = append(notes, envNote{path: p, n: n})
			}
		}
		a.post(func() {
			a.auto.watchBusy = false
			for _, n := range notes {
				a.auto.envNotes = slices.DeleteFunc(a.auto.envNotes, func(x envNote) bool { return x.path == n.path })
				a.auto.envNotes = append(a.auto.envNotes, n)
				a.notify("env-"+n.path, "KeyBar："+envLabel(n.path)+" 有新 key", fmt.Sprintf("%d 个像密钥的值不在 KeyBar：打开导入", n.n))
			}
			if len(notes) > 0 {
				a.trayChanged()
			}
		})
	})
}

// unmanaged counts a .env's likely secrets that KeyBar lacks.
func unmanaged(v *vault.Vault, path string) int {
	f, ok := scan.ReadEnv(path)
	if !ok {
		return 0
	}
	p, err := importer.FromVars(v, f.Path, f.Project, "", f.Vars)
	if err != nil {
		return 0
	}
	return scan.Unmanaged(p)
}

// envLabel is a .env as its folder and name: my-app/.env.
func envLabel(p string) string {
	return filepath.Base(filepath.Dir(p)) + "/" + filepath.Base(p)
}

func (a *app) dropEnvNote(path string) {
	a.auto.envNotes = slices.DeleteFunc(a.auto.envNotes, func(x envNote) bool { return x.path == path })
	a.trayChanged()
}

// ignoreEnv never tells of, nor scans, the file again.
func (a *app) ignoreEnv(path string) {
	if !slices.Contains(a.settings.IgnoredFiles, path) {
		a.settings.IgnoredFiles = append(a.settings.IgnoredFiles, path)
		a.saveSettings(a.settings)
	}
	if a.auto.watcher != nil {
		a.auto.watcher.Ignore(path)
	}
	a.dropEnvNote(path)
	a.say("不再提醒 " + a.shortHome(path))
}

// openScan scans the machine (or one file) in the background, then shows
// the checklist.
func (a *app) openScan(file string) {
	a.page = pageScan
	a.auto.scan = scanning{running: true, file: file, clean: map[string]bool{}, mode: map[string]scan.Mode{}}
	if file != "" {
		a.dropEnvNote(file)
	}
	o := a.scanOptions()
	v := a.v
	a.spawn(func() {
		var found []scan.Found
		if file != "" {
			if f, ok := scan.ReadEnv(file); ok {
				found = append(found, f)
			}
		} else {
			found = scan.Run(o)
		}
		groups, err := scan.Plans(v, found)
		a.post(func() {
			s := &a.auto.scan
			if !s.running || s.file != file {
				return // left meanwhile
			}
			s.running, s.groups = false, groups
			if err != nil {
				s.err = err.Error()
			}
			if file == "" {
				likely := 0
				for _, g := range groups {
					for _, it := range g.Plan.Items {
						if it.Likely {
							likely++
						}
					}
				}
				_ = a.v.Log(vault.Event{Client: "app", Action: "scan", Decision: "ok", Detail: fmt.Sprintf("%d 个文件，%d 个像密钥", len(groups), likely)})
			}
		})
	})
}

// importScan imports what is checked, file by file, and takes the plain
// text out of the files chosen for it.
func (a *app) importScan() {
	s := &a.auto.scan
	if s.running {
		return
	}
	for _, g := range s.groups {
		for _, it := range g.Plan.Conflicts() {
			if !it.Skip && it.Choice == importer.Rename {
				if err := vault.CheckName(strings.TrimSpace(it.NewName)); err != nil {
					s.err = it.Var + "：" + err.Error()
					return
				}
			}
		}
	}
	var added, files, cleaned int
	var errs []string
	for _, g := range s.groups {
		if !anyChecked(g) {
			continue
		}
		o := scan.Import(a.v, g, s.clean[g.Plan.Path], s.mode[g.Plan.Path])
		if o.Err != nil {
			errs = append(errs, a.shortHome(o.Path)+"："+o.Err.Error())
			continue
		}
		files++
		added += o.Result.Added + o.Result.Renamed + o.Result.Overwritten
		errs = append(errs, o.Result.Failed...)
		_ = a.v.Log(vault.Event{Client: "app", Action: "import", Keys: o.Keys(), Decision: "ok", Detail: o.Path})
		switch {
		case o.CleanErr != nil:
			errs = append(errs, a.shortHome(o.Path)+" 没有移除明文："+o.CleanErr.Error())
		case o.Cleaned > 0:
			cleaned++
			_ = a.v.Log(vault.Event{Client: "app", Action: "clean", Keys: o.Keys(), Decision: "ok", Detail: o.Path + " → " + o.Backup})
		}
		a.dropEnvNote(o.Path)
	}
	msg := fmt.Sprintf("从 %d 个文件导入了 %d 个密钥", files, added)
	if cleaned > 0 {
		msg += fmt.Sprintf("，%d 个文件移除了明文（原文件存为 %s）", cleaned, scan.BackupSuffix)
	}
	if len(errs) > 0 {
		msg += "；" + errs[0]
	}
	a.auto.scan = scanning{}
	a.reload()
	a.page = pageList
	a.focusInput = true
	a.say(msg)
}

func anyChecked(g scan.Group) bool {
	for _, it := range g.Plan.Items {
		if !it.Skip && it.State != importer.Empty {
			return true
		}
	}
	return false
}

// For an agent's request: the names missing, pasted into the approval.

// saveFilled saves the values pasted for the missing names.
func (a *app) saveFilled(p *pending) []string {
	var added []string
	proj := ""
	if p.req.Project != "" {
		proj = filepath.Base(p.req.Project)
	}
	for _, n := range p.req.Missing {
		v := strings.TrimSpace(*p.fill[n])
		if v == "" || p.never[n] {
			continue
		}
		k := vault.Key{Name: n, Description: "运行时补录"}
		if proj != "" {
			k.Projects = []string{proj}
		}
		if pr, ok := secret.Detect(v); ok {
			k.Description = pr.Name + " key（运行时补录）"
		}
		if _, ok := a.key(n); ok {
			continue // added meanwhile
		}
		if err := a.v.Add(k, []byte(v)); err != nil {
			a.say(n + " 没存上：" + err.Error())
			continue
		}
		added = append(added, n)
	}
	if len(added) > 0 {
		_ = a.v.Log(vault.Event{Client: p.req.Client, Action: "add", Keys: added, Decision: "ok", Detail: "批准时补录"})
		a.reload()
	}
	return added
}

// neverAsk records the names not to ask for again in the project.
func (a *app) neverAsk(p *pending) {
	var names []string
	for n, on := range p.never {
		if on {
			names = append(names, n)
		}
	}
	if len(names) == 0 || p.req.Project == "" {
		return
	}
	slices.Sort(names)
	a.settings.AddNeverAsk(p.req.Project, names...)
	a.saveSettings(a.settings)
}

// filled counts the values pasted.
func (p *pending) filled() int {
	n := 0
	for _, name := range p.req.Missing {
		if strings.TrimSpace(*p.fill[name]) != "" && !p.never[name] {
			n++
		}
	}
	return n
}

// autoSig changes when the tray menu's notices do.
func (a *app) autoSig() string {
	s := ""
	if f := a.auto.found; f != nil {
		s = f.provider
	}
	for _, n := range a.auto.envNotes {
		s += "|" + n.path + fmt.Sprint(n.n)
	}
	return s
}
