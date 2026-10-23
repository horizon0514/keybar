package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"keybar/agents"
	"keybar/approval"
	"keybar/importer"
	"keybar/project"
	"keybar/remote"
	"keybar/vault"

	"github.com/egoist/mygo/ui"
)

type page int

const (
	pageList page = iota
	pageDetail
	pageForm
	pageLog
	pageSettings
	pageImport
	pageScan
)

// Settings' tabs.
const (
	tabGeneral = iota
	tabAgents
	tabAuto
	tabAbout
)

var tabNames = []string{"通用", "AI 代理", "自动录入", "关于"}

// The list's filters.
const (
	filterAll = iota
	filterDue
	filterDenied
)

var filterNames = []string{"全部", "需处理", "代理禁用"}

// pending is an agent's request waiting for the user.
type pending struct {
	req    approval.Request
	at     time.Time
	answer chan approval.Decision
	// for the names missing: the values pasted (kept only until
	// answered), and those not to ask for again in the project
	fill  map[string]*string
	never map[string]bool
}

// importing is the import page: a .env file's path, then what it holds
// and what to do with the names KeyBar already has.
type importing struct {
	path   string
	plan   *importer.Plan
	err    string
	chosen int  // the conflict chosen with ↑↓
	shred  bool // remove the file once every value is in KeyBar
	typing bool // the new name's field has the focus
}

// form is the add or rotate form: add asks name + value (+ optional
// description); rotate asks the new value (+ confirm) and an optional note.
// Platforms, rotate days, expiry, projects and tags live on the detail page.
type form struct {
	rotate                          bool // rotating name, else adding a key
	name, value, value2, desc, note string
	err                             string
}

// metaEdit is the detail page's editable fields for a key (not its value).
type metaEdit struct {
	name, desc, days, expires, projects, tags string
	err                                        string
}

// app is the panel's state and what it does; the shell (gui.go) gives it
// the window, the tray and the system, tests fakes.
type app struct {
	v        *vault.Vault
	inv      *vault.Inventory
	invErr   string
	settings vault.Settings
	grants   *approval.Grants
	syncer   *remote.Syncer
	demo     bool
	exe      string // the program, for the MCP config and the command line

	page    page
	query   string
	filter  int // filterAll, filterDue, filterDenied
	chosen  int
	detail  string // the key shown
	tab     int
	toast   string
	cheat   bool
	confirm bool // the delete dialog
	showDetailMore bool // advanced sections on the detail page unfolded

	form    form
	meta    metaEdit
	addPlat string
	syncing map[string]bool   // key/platform IDs being updated
	syncMsg map[string]string // and the last result

	pending []*pending

	imp         importing
	auto        autoState
	agents      *agents.Env     // the agents on this computer
	agentState  []agents.Status // as last read
	showMissing bool            // the agents not installed, unfolded
	agentErr    map[string]string

	// settings being edited
	editDays, editRemind, editGrant string

	list       ui.ListState
	input      ui.Handle
	focusInput bool
	typing     bool // a form's field has the focus

	now          func() time.Time
	post         func(func())                 // run on the UI thread
	spawn        func(func())                 // run in the background
	later        func(time.Duration, func())  // run on the UI thread after d
	show, hide   func()                       // the panel
	quit         func()                       //
	notify       func(id, title, body string) //
	copy         func(string)                 // to the clipboard
	trayChanged  func()                       //
	loginItem    func() bool                  // opens at login
	setLoginItem func(bool) error             //
	installCLI   func() (string, error)       // links keys into PATH
	saveSettings func(vault.Settings)         //
	openURL      func(string)                 //
	winDrag      func(phase int)              // moves the panel
	moving       bool
	pickFile     func(done func(path string)) // a file dialog; nil: none
}

func newApp(v *vault.Vault) *app {
	a := &app{v: v, grants: &approval.Grants{}, syncing: map[string]bool{}, syncMsg: map[string]string{}, agentErr: map[string]string{}}
	a.now = time.Now
	a.settings = vault.LoadSettings(v.Dir)
	a.syncer = &remote.Syncer{Token: v.Value}
	a.post = func(fn func()) { fn() }
	a.spawn = func(fn func()) { go fn() }
	a.later = func(d time.Duration, fn func()) { time.AfterFunc(d, func() { a.post(fn) }) }
	nop := func() {}
	a.show, a.hide, a.quit, a.trayChanged = nop, nop, nop, nop
	a.notify = func(string, string, string) {}
	a.copy = func(string) {}
	a.loginItem = func() bool { return false }
	a.setLoginItem = func(bool) error { return nil }
	a.installCLI = func() (string, error) { return "", fmt.Errorf("不支持") }
	a.saveSettings = func(s vault.Settings) { _ = vault.SaveSettings(v.Dir, s) }
	a.openURL = func(string) {}
	a.reload()
	return a
}

// reload reads the inventory again: the command line may have changed it.
func (a *app) reload() {
	inv, err := a.v.Inventory()
	if err != nil {
		a.invErr = err.Error()
		if a.inv == nil {
			a.inv = &vault.Inventory{}
		}
		return
	}
	a.inv, a.invErr = inv, ""
}

// rows are the keys the list shows: those matching the search, the most
// urgent first.
func (a *app) rows() []vault.Key {
	now := a.now()
	var out []vault.Key
	for _, k := range a.inv.Keys {
		if !matches(k, a.query) {
			continue
		}
		switch a.filter {
		case filterDue:
			if st := a.inv.StatusOf(k, now); st.State == vault.OK && st.Pending == 0 {
				continue
			}
		case filterDenied:
			if k.Agent != "deny" {
				continue
			}
		}
		out = append(out, k)
	}
	slices.SortStableFunc(out, func(x, y vault.Key) int {
		sx, sy := a.inv.StatusOf(x, now).State, a.inv.StatusOf(y, now).State
		if sx != sy {
			return int(sy) - int(sx)
		}
		return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
	})
	return out
}

func matches(k vault.Key, q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return true
	}
	hay := strings.ToLower(strings.Join(append(append([]string{k.Name, k.Description}, k.Tags...), k.Projects...), " "))
	for _, p := range k.Platforms {
		hay += " " + strings.ToLower(p.Label())
	}
	return strings.Contains(hay, q)
}

// counts are the keys overdue, due soon, and with platforms to update.
func (a *app) counts() (over, soon, pend int) {
	now := a.now()
	for _, k := range a.inv.Keys {
		st := a.inv.StatusOf(k, now)
		switch st.State {
		case vault.Overdue:
			over++
		case vault.DueSoon:
			soon++
		}
		if st.Pending > 0 {
			pend++
		}
	}
	return
}

func (a *app) key(name string) (vault.Key, bool) {
	k, ok := a.inv.Find(name)
	if !ok {
		return vault.Key{}, false
	}
	return *k, true
}

func (a *app) chosenKey() (vault.Key, bool) {
	rows := a.rows()
	if a.chosen < 0 || a.chosen >= len(rows) {
		return vault.Key{}, false
	}
	return rows[a.chosen], true
}

func (a *app) choose(i int) {
	n := len(a.rows())
	if n == 0 {
		a.chosen = 0
		return
	}
	a.chosen = max(0, min(i, n-1))
	a.list.ScrollIntoView(a.chosen)
}

// setFilter shows the list through filter f (wrapping around).
func (a *app) setFilter(f int) {
	n := len(filterNames)
	a.filter = (f%n + n) % n
	a.chosen = 0
	a.list.ScrollIntoView(0)
}

func (a *app) say(msg string) {
	a.toast = msg
	at := msg
	a.later(4*time.Second, func() {
		if a.toast == at {
			a.toast = ""
		}
	})
}

func (a *app) openDetail(name string) {
	a.reload()
	k, ok := a.key(name)
	if !ok {
		return
	}
	a.detail, a.page, a.addPlat, a.confirm = name, pageDetail, "", false
	a.showDetailMore = false
	a.loadMeta(k)
	for i, row := range a.rows() { // the list shows it chosen
		if row.Name == name {
			a.chosen = i
			a.list.ScrollIntoView(i)
		}
	}
}

func (a *app) loadMeta(k vault.Key) {
	days := ""
	switch k.RotateDays {
	case 0:
		days = ""
	case -1:
		days = "-1"
	default:
		days = strconv.Itoa(k.RotateDays)
	}
	exp := ""
	if !k.Expires.IsZero() {
		exp = k.Expires.Local().Format(time.DateOnly)
	}
	a.meta = metaEdit{
		name: k.Name, desc: k.Description, days: days, expires: exp,
		projects: strings.Join(k.Projects, ", "), tags: strings.Join(k.Tags, ", "),
	}
}

func (a *app) back() {
	switch a.page {
	case pageForm:
		if a.form.rotate {
			a.page = pageDetail
		} else {
			a.page = pageList
		}
	case pageImport:
		if a.imp.plan != nil {
			a.imp.plan, a.imp.err = nil, ""
			return
		}
		a.page = pageList
	case pageDetail, pageLog, pageSettings:
		a.page = pageList
	case pageScan:
		a.auto.scan = scanning{}
		a.page = pageList
	default:
		a.hide()
		return
	}
	a.form = form{}
	a.meta = metaEdit{}
	a.focusInput = a.page == pageList
}

// shown is the panel showing: what changed meanwhile.
func (a *app) shown() {
	a.reload()
	if a.page == pageList {
		a.focusInput = true
	}
}

func (a *app) openAdd() {
	a.form = form{}
	a.page = pageForm
}

func (a *app) openRotate(name string) {
	if _, ok := a.key(name); !ok {
		return
	}
	a.form = form{rotate: true, name: name}
	a.detail = name
	a.page = pageForm
}

// dueNames are keys overdue or due within the remind window, overdue first.
func (a *app) dueNames() (over, soon []string) {
	now := a.now()
	horizon := now.Add(time.Duration(a.settings.RemindDays) * 24 * time.Hour)
	for _, k := range a.inv.Keys {
		st := a.inv.StatusOf(k, now)
		switch {
		case st.State == vault.Overdue:
			over = append(over, k.Name)
		case !st.Due.IsZero() && st.Due.Before(horizon), !st.Expires.IsZero() && st.Expires.Before(horizon):
			soon = append(soon, k.Name)
		}
	}
	return over, soon
}

// openDueRotate opens the rotate form for the first overdue key, else the
// first due-soon one: the continuous path from a reminder.
func (a *app) openDueRotate() {
	over, soon := a.dueNames()
	switch {
	case len(over) > 0:
		a.openRotate(over[0])
	case len(soon) > 0:
		a.openRotate(soon[0])
	default:
		a.setFilter(filterDue)
		a.page = pageList
	}
}

func (a *app) openLog()      { a.page = pageLog }
func (a *app) openSettings() { a.openTab(a.tab) }

func (a *app) openTab(t int) {
	a.tab = max(0, min(t, len(tabNames)-1))
	a.page = pageSettings
	if a.tab == tabAgents {
		a.refreshAgents()
	}
	days := a.inv.RotateDays
	if days == 0 {
		days = vault.DefaultRotateDays
	}
	a.editDays, a.editRemind, a.editGrant = strconv.Itoa(days), strconv.Itoa(a.settings.RemindDays), strconv.Itoa(a.settings.GrantMinutes)
}

// submit saves the form.
func (a *app) submit() {
	f := &a.form
	f.err = ""
	if f.rotate {
		if f.value == "" {
			f.err = "请输入新值"
			return
		}
		if f.value != f.value2 {
			f.err = "两次输入的值不一致"
			return
		}
		a.v.By = "app"
		k, err := a.v.Rotate(f.name, []byte(f.value), strings.TrimSpace(f.note))
		if err != nil {
			f.err = err.Error()
			return
		}
		_ = a.v.Log(vault.Event{Client: "app", Action: "rotate", Keys: []string{f.name}, Decision: "ok"})
		value := []byte(f.value)
		a.form = form{}
		a.reload()
		a.openDetail(k.Name)
		n := 0
		for _, p := range k.Pending() {
			if p.Auto() {
				a.syncPlatform(k.Name, p, value)
				n++
			}
		}
		msg := "已轮换 " + k.Name
		if n > 0 {
			msg += fmt.Sprintf("，正在自动更新 %d 个平台", n)
		}
		if m := len(k.Pending()) - n; m > 0 {
			msg += fmt.Sprintf("；还有 %d 个要手动更新", m)
		}
		a.say(msg)
		a.trayChanged()
		return
	}
	name := strings.TrimSpace(f.name)
	if err := vault.CheckName(name); err != nil {
		f.err = err.Error()
		return
	}
	if _, ok := a.key(name); ok {
		f.err = "已有同名密钥：要换新值请在详情里点「轮换」"
		return
	}
	if strings.TrimSpace(f.value) == "" {
		f.err = "请输入值"
		return
	}
	k := vault.Key{Name: name, Description: strings.TrimSpace(f.desc)}
	if err := a.v.Add(k, []byte(f.value)); err != nil {
		f.err = err.Error()
		return
	}
	_ = a.v.Log(vault.Event{Client: "app", Action: "add", Keys: []string{name}, Decision: "ok"})
	a.form = form{}
	a.reload()
	a.openDetail(name)
	a.say("已添加 " + name)
	a.trayChanged()
}

func splitList(s string) []string {
	return vault.Clean(strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == '\n' }))
}

// saveMeta writes the detail page's editable fields.
func (a *app) saveMeta() {
	m := &a.meta
	m.err = ""
	if m.name == "" {
		return
	}
	var days int
	if s := strings.TrimSpace(m.days); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n == 0 {
			m.err = "轮换周期要一个天数，-1 表示不提醒"
			return
		}
		days = n
	}
	var exp time.Time
	if s := strings.TrimSpace(m.expires); s != "" {
		t, err := time.ParseInLocation(time.DateOnly, s, time.Local)
		if err != nil {
			m.err = "过期日格式是 2027-01-19"
			return
		}
		exp = t
	}
	err := a.v.Edit(m.name, func(k *vault.Key) error {
		k.Description = strings.TrimSpace(m.desc)
		k.RotateDays = days
		k.Expires = exp
		k.Projects = splitList(m.projects)
		k.Tags = splitList(m.tags)
		return nil
	})
	if err != nil {
		m.err = err.Error()
		return
	}
	a.reload()
	if k, ok := a.key(m.name); ok {
		a.loadMeta(k)
	}
	a.say("已保存 " + m.name)
	a.trayChanged()
}

// syncPlatform updates an automatic platform with the value, in the
// background.
func (a *app) syncPlatform(name string, p vault.Platform, value []byte) {
	id := name + "/" + p.ID
	if a.syncing[id] {
		return
	}
	a.syncing[id] = true
	a.syncMsg[id] = ""
	a.spawn(func() {
		if value == nil {
			v, err := a.v.Value(name)
			if err != nil {
				a.post(func() { a.syncDone(name, p, err) })
				return
			}
			value = v
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		err := a.syncer.Update(ctx, name, p, value)
		cancel()
		a.post(func() { a.syncDone(name, p, err) })
	})
}

func (a *app) syncDone(name string, p vault.Platform, err error) {
	id := name + "/" + p.ID
	delete(a.syncing, id)
	ev := vault.Event{Client: "app", Action: "sync", Keys: []string{name}, Detail: p.Label(), Decision: "ok"}
	if err != nil {
		ev.Decision = "error"
		a.syncMsg[id] = "✗ " + err.Error()
	} else {
		_ = a.v.MarkUpdated(name, p.ID)
		a.syncMsg[id] = "✓ 已自动更新"
	}
	_ = a.v.Log(ev)
	a.reload()
	a.trayChanged()
}

// togglePlatform ticks a platform off the checklist, or puts it back.
func (a *app) togglePlatform(name string, p vault.Platform, done bool) {
	var err error
	if done {
		err = a.v.MarkUpdated(name, p.ID)
	} else {
		err = a.v.Unmark(name, p.ID)
	}
	if err != nil {
		a.say(err.Error())
	}
	a.reload()
	a.trayChanged()
}

func (a *app) addPlatform(name string) {
	s := strings.TrimSpace(a.addPlat)
	if s == "" {
		return
	}
	p, err := vault.ParsePlatform(s)
	if err != nil {
		a.say(err.Error())
		return
	}
	now := a.now()
	if err := a.v.Edit(name, func(k *vault.Key) error { k.AddPlatform(p, now); return nil }); err != nil {
		a.say(err.Error())
		return
	}
	a.addPlat = ""
	a.reload()
}

func (a *app) removePlatform(name, id string) {
	_ = a.v.Edit(name, func(k *vault.Key) error {
		k.Platforms = slices.DeleteFunc(k.Platforms, func(p vault.Platform) bool { return p.ID == id })
		return nil
	})
	a.reload()
}

func (a *app) setAgent(name string, allow bool) {
	_ = a.v.Edit(name, func(k *vault.Key) error {
		k.Agent = ""
		if !allow {
			k.Agent = "deny"
		}
		return nil
	})
	if !allow {
		a.grants.Revoke(name)
	}
	a.reload()
}

// copyValue puts the value on the clipboard, for 30 seconds.
func (a *app) copyValue(name string) {
	v, err := a.v.Value(name)
	if err != nil {
		a.say("读取失败：" + err.Error())
		return
	}
	a.copy(string(v))
	_ = a.v.Log(vault.Event{Client: "app", Action: "copy", Keys: []string{name}, Decision: "ok"})
	a.say("已复制 " + name + " 的值，30 秒后清除剪贴板")
	a.later(30*time.Second, func() { a.copy("") })
}

func (a *app) removeKey(name string) {
	if err := a.v.Remove(name); err != nil {
		a.say(err.Error())
		return
	}
	a.grants.Revoke(name)
	_ = a.v.Log(vault.Event{Client: "app", Action: "rm", Keys: []string{name}, Decision: "ok"})
	a.confirm = false
	a.reload()
	a.page = pageList
	a.choose(a.chosen)
	a.say("已删除 " + name)
	a.trayChanged()
}

// request is an agent's request, from the IPC server's goroutine: a grant
// answers it, else the user, in the panel, within approval.Timeout.
func (a *app) request(r approval.Request) approval.Decision {
	if a.grants.Covers(r.Client, r.Keys, a.now()) && len(r.Missing) == 0 {
		return approval.Decision{Allow: true, Granted: true}
	}
	p := &pending{req: r, answer: make(chan approval.Decision, 1), fill: map[string]*string{}, never: map[string]bool{}}
	for _, n := range r.Missing {
		p.fill[n] = new(string)
	}
	a.post(func() {
		p.at = a.now()
		a.pending = append(a.pending, p)
		a.show()
		a.notify("approve-"+r.ID, "KeyBar：需要你批准", r.Summary())
		a.trayChanged()
	})
	select {
	case d := <-p.answer:
		return d
	case <-time.After(approval.Timeout):
		a.post(func() { a.drop(p) })
		return approval.Decision{Why: "timeout"}
	}
}

func (a *app) drop(p *pending) {
	a.pending = slices.DeleteFunc(a.pending, func(q *pending) bool { return q == p })
	a.trayChanged()
}

// decide answers the first request.
func (a *app) decide(allow bool, timed bool) {
	if len(a.pending) == 0 {
		return
	}
	p := a.pending[0]
	d := approval.Decision{Allow: allow, Why: "denied"}
	if allow {
		d.Why = ""
		d.Added = a.saveFilled(p)
		if timed {
			d.For = time.Duration(a.settings.GrantMinutes) * time.Minute
			a.grants.Add(p.req.Client, append(append([]string(nil), p.req.Keys...), d.Added...), a.now().Add(d.For))
		}
	}
	a.neverAsk(p)
	for _, v := range p.fill {
		*v = ""
	}
	p.answer <- d
	a.drop(p)
	if len(a.pending) == 0 {
		a.hide()
	}
}

// remind notifies, once a day, of the keys due.
func (a *app) remind() {
	if !a.settings.Remind {
		return
	}
	today := a.now().Format(time.DateOnly)
	if a.settings.LastReminded == today {
		return
	}
	a.reload()
	over, soon := a.dueNames()
	if len(over)+len(soon) == 0 {
		return
	}
	a.settings.LastReminded = today
	a.saveSettings(a.settings)
	var parts []string
	if len(over) > 0 {
		parts = append(parts, fmt.Sprintf("%d 个已超期：%s", len(over), names(over)))
	}
	if len(soon) > 0 {
		parts = append(parts, fmt.Sprintf("%d 个快到期：%s", len(soon), names(soon)))
	}
	a.notify("remind-"+today, "KeyBar：该轮换密钥了", strings.Join(parts, "；"))
}

func names(ns []string) string {
	if len(ns) > 3 {
		return strings.Join(ns[:3], "、") + " 等"
	}
	return strings.Join(ns, "、")
}

// trayTitle is the menu bar's text next to the icon: what needs the user.
func (a *app) trayTitle() string {
	over, soon, _ := a.counts()
	var s []string
	if n := len(a.pending); n > 0 {
		s = append(s, fmt.Sprintf("⏳%d", n))
	}
	if over+soon > 0 {
		s = append(s, fmt.Sprintf("⚠︎%d", over+soon))
	}
	return strings.Join(s, " ")
}

// trayLine is the menu's first line.
func (a *app) trayLine() string {
	over, soon, pend := a.counts()
	s := fmt.Sprintf("%d 个密钥", len(a.inv.Keys))
	if over > 0 {
		s += fmt.Sprintf(" · %d 个超期", over)
	}
	if soon > 0 {
		s += fmt.Sprintf(" · %d 个快到期", soon)
	}
	if pend > 0 {
		s += fmt.Sprintf(" · %d 个待同步", pend)
	}
	if over+soon+pend == 0 && len(a.inv.Keys) > 0 {
		s += " · 都正常"
	}
	return s
}

// applySettings saves the settings tab's numbers.
func (a *app) applyNumber(which string) {
	var s string
	switch which {
	case "days":
		s = a.editDays
	case "remind":
		s = a.editRemind
	case "grant":
		s = a.editGrant
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 || n > 3650 {
		a.say("请输入 1–3650 之间的整数")
		return
	}
	switch which {
	case "days":
		if err := a.v.Update(func(inv *vault.Inventory) error { inv.RotateDays = n; return nil }); err != nil {
			a.say(err.Error())
			return
		}
		a.reload()
	case "remind":
		a.settings.RemindDays = n
		a.saveSettings(a.settings)
	case "grant":
		a.settings.GrantMinutes = min(n, 24*60)
		a.saveSettings(a.settings)
	}
	a.say("已保存")
	a.trayChanged()
}

// Agents

func (a *app) refreshAgents() {
	if a.agents == nil {
		a.agentState = nil
		return
	}
	a.agentState = a.agents.Status()
}

// connectAgent adds KeyBar to the agent's MCP settings, or removes it.
func (a *app) connectAgent(ag agents.Agent, on bool) {
	if a.agents == nil {
		return
	}
	var err error
	if on {
		_, err = a.agents.Connect(ag)
	} else {
		_, err = a.agents.Disconnect(ag)
	}
	delete(a.agentErr, ag.ID)
	a.refreshAgents()
	if err != nil {
		a.agentErr[ag.ID] = err.Error()
		a.say(ag.Name + "：" + err.Error())
		return
	}
	act := map[bool]string{true: "connect", false: "disconnect"}[on]
	_ = a.v.Log(vault.Event{Client: "app", Action: act, Decision: "ok", Detail: ag.Name})
	if on {
		a.say("已连接 " + ag.Name + "：重启它（或开新会话）后生效")
	} else {
		a.say("已断开 " + ag.Name)
	}
}

// connectAll connects every agent installed and not connected.
func (a *app) connectAll() {
	n := 0
	for _, s := range a.agentState {
		if s.State == agents.NotConnected || s.State == agents.Outdated {
			a.connectAgent(s.Agent, true)
			if a.agentErr[s.ID] == "" {
				n++
			}
		}
	}
	if n > 1 {
		a.say(fmt.Sprintf("已连接 %d 个代理：重启它们（或开新会话）后生效", n))
	}
}

// toConnect counts the agents installed but not connected.
func (a *app) toConnect() int {
	n := 0
	for _, s := range a.agentState {
		if s.State == agents.NotConnected || s.State == agents.Outdated {
			n++
		}
	}
	return n
}

// Import

func (a *app) openImport() {
	a.imp = importing{}
	a.page = pageImport
}

// expandHome reads ~/ as the home folder.
func expandHome(p string) string {
	p = strings.Trim(strings.TrimSpace(p), `"'`)
	if p == "~" || strings.HasPrefix(p, "~/") || filepath.Separator == '\\' && strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// readImport reads the file and compares it with KeyBar.
func (a *app) readImport() {
	path := expandHome(a.imp.path)
	if path == "" {
		a.imp.err = "输入 .env 文件的路径，或拖进来"
		return
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		path = filepath.Join(path, ".env")
	}
	plan, err := importer.Read(a.v, path, "", "")
	if err != nil {
		a.imp.err = err.Error()
		return
	}
	a.imp.plan, a.imp.err, a.imp.chosen = plan, "", 0
}

// importChoose changes the chosen conflict's choice by d (wrapping).
func (a *app) importChoose(d int) {
	cs := a.imp.plan.Conflicts()
	if len(cs) == 0 {
		return
	}
	it := cs[max(0, min(a.imp.chosen, len(cs)-1))]
	it.Choice = importer.Choice((int(it.Choice) + d + 3) % 3)
}

// applyImport imports what the plan says.
func (a *app) applyImport() {
	p := a.imp.plan
	if p == nil {
		a.readImport()
		return
	}
	for _, it := range p.Conflicts() {
		if it.Choice == importer.Rename {
			if err := p.CheckName(a.v, it, strings.TrimSpace(it.NewName)); err != nil {
				a.imp.err = it.Var + "：" + err.Error()
				return
			}
		}
	}
	r := p.Apply(a.v)
	importer.AttachEnvPlatforms(a.v, p.Path, r)
	msg := "导入：" + r.Summary()
	if path, err := p.WriteMapping(r); err != nil {
		msg += "；写 keys.toml 失败：" + err.Error()
	} else if path != "" {
		msg += "；改名的记在 keys.toml"
	}
	if len(r.Failed) > 0 {
		msg += "；" + r.Failed[0]
	}
	if a.imp.shred && r.Complete() {
		if err := project.Shred(p.Path); err != nil {
			msg += "；删除 .env 失败：" + err.Error()
		} else {
			msg += "；已删除 .env"
		}
	}
	var names []string
	for _, k := range r.Env {
		if !slices.Contains(names, k) {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	_ = a.v.Log(vault.Event{Client: "app", Action: "import", Keys: names, Decision: "ok", Detail: p.Path})
	a.imp = importing{}
	a.reload()
	a.page = pageList
	a.focusInput = true
	a.say(msg)
	a.trayChanged()
}

// mcpConfig is the snippet for an agent's MCP settings.
func (a *app) mcpConfig(client string) string {
	exe := a.exe
	if exe == "" {
		exe = "keys"
	}
	q := strconv.Quote(exe)
	args := `["mcp"]`
	if client != "" {
		args = fmt.Sprintf(`["mcp", "--client", %q]`, client)
	}
	return fmt.Sprintf("{\n  \"mcpServers\": {\n    \"keybar\": {\n      \"command\": %s,\n      \"args\": %s\n    }\n  }\n}", q, args)
}

// accessFor are the latest events of a key, newest first.
func (a *app) accessFor(name string, n int) []vault.Event {
	evs := vault.ReadLog(a.v.Dir, 0, vault.ForKey(name))
	slices.Reverse(evs)
	if len(evs) > n {
		evs = evs[:n]
	}
	return evs
}
