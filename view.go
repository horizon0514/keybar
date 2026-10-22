package main

import (
	"fmt"
	"strings"
	"time"

	"keybar/approval"
	"keybar/vault"

	"github.com/egoist/mygo/ui"
)

// The panel.
const (
	panelWidth, panelHeight = 600, 620
	minWidth, minHeight     = 480, 420
)

var (
	iconSearch = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>`))
	iconGear   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/></svg>`))
	iconBack   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 18-6-6 6-6"/></svg>`))
	iconAlert  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 8v4M12 16h.01"/></svg>`))
	iconPlus   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14M5 12h14"/></svg>`))
	iconLog    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 8v4l3 3"/><circle cx="12" cy="12" r="9"/></svg>`))
	iconShield = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>`))
	iconKey    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="7.5" cy="15.5" r="4.5"/><path d="m21 2-9.6 9.6M15.5 7.5l3 3L22 7l-3-3"/></svg>`))
	iconBot    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 8V4H8"/><rect width="16" height="12" x="4" y="8" rx="2"/><path d="M2 14h2M20 14h2M15 13v2M9 13v2"/></svg>`))
	iconInfo   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/></svg>`))
	iconX      = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18M6 6l12 12"/></svg>`))
)

// The look: a spacing scale of 4, 8, 12, 16 (and 20, 24 for the page's
// edges), and a type scale of title 15, body 13, secondary 12, meta 11;
// key names in monospace.
const (
	sp1, sp2, sp3, sp4, sp5 = 4, 8, 12, 16, 20

	fsTitle, fsBody, fsSmall, fsMeta = 15, 13, 12, 11
	mono                             = "monospace"
)

// ink is the palette for the window's theme.
type ink struct {
	dark                                         bool
	bg, sunk, card, code, field                  ui.Color // surfaces: the panel, a page under cards, a card, code, an input
	text, muted, faint                           ui.Color // primary, secondary, tertiary text
	line, sep                                    ui.Color // borders; separators inside a card
	link, accent, hover, chosen                  ui.Color
	gold, green, red, danger                     ui.Color
	surface, surfaceHover, surfacePressed, focus ui.Color
}

// colors are opaque; every text color is WCAG AA (4.5:1) on bg, sunk and
// card (TestContrast).
var colors = map[bool]map[string]string{
	false: {
		"bg": "#ffffff", "sunk": "#f5f5f7", "card": "#ffffff", "code": "#f4f4f6", "field": "#ffffff",
		"text": "#1d1d1f", "muted": "#4b4b52", "faint": "#6b6b73",
		"line": "#dedee3", "sep": "#ececf0",
		"link": "#0a5bd8", "accent": "#0a6cf0",
		"gold": "#8f5400", "green": "#17733a", "red": "#c21f1f", "danger": "#c21f1f",
		"surface": "#ffffff", "surfaceHover": "#f2f2f5", "surfacePressed": "#e8e8ec",
	},
	true: {
		"bg": "#1e1e21", "sunk": "#18181b", "card": "#26262a", "code": "#151517", "field": "#18181b",
		"text": "#ececef", "muted": "#b4b4bb", "faint": "#94949c",
		"line": "#36363c", "sep": "#2f2f34",
		"link": "#7cb1ff", "accent": "#2f7cf6",
		"gold": "#f2bd4b", "green": "#5ccf8a", "red": "#ff7d73", "danger": "#ff7d73",
		"surface": "#2c2c31", "surfaceHover": "#35353b", "surfacePressed": "#3e3e45",
	},
}

func palette(c *ui.Context) ink {
	dark := c.Theme().Dark
	p := colors[dark]
	h := func(n string) ui.Color { return ui.Hex(p[n]) }
	k := ink{dark: dark, bg: h("bg"), sunk: h("sunk"), card: h("card"), code: h("code"), field: h("field"),
		text: h("text"), muted: h("muted"), faint: h("faint"), line: h("line"), sep: h("sep"),
		link: h("link"), accent: h("accent"), gold: h("gold"), green: h("green"), red: h("red"), danger: h("danger"),
		surface: h("surface"), surfaceHover: h("surfaceHover"), surfacePressed: h("surfacePressed")}
	if dark {
		k.hover = ui.RGB(255, 255, 255).Alpha(0.05)
		k.chosen = k.accent.Alpha(0.2)
		k.focus = k.link.Alpha(0.6)
	} else {
		k.hover = ui.RGB(0, 0, 0).Alpha(0.035)
		k.chosen = k.accent.Alpha(0.10)
		k.focus = k.accent.Alpha(0.5)
	}
	return k
}

// theme is the system theme in the app's colors and sizes, for the
// widgets (buttons, inputs, switches): compact, 13pt, 7pt corners.
func (k ink) theme(base *ui.Theme) *ui.Theme {
	t := *base
	t.Background, t.Surface, t.SurfaceHover, t.SurfacePressed = k.bg, k.surface, k.surfaceHover, k.surfacePressed
	t.Border, t.Text, t.TextMuted = k.line, k.text, k.faint
	t.Accent, t.Focus = k.accent, k.focus
	t.AccentHover, t.AccentPressed = k.accent.Alpha(0.88), k.accent.Alpha(0.78)
	t.Danger, t.Warning, t.Success = k.danger, k.gold, k.green
	t.Radius, t.Spacing, t.FontSize = 7, 4, fsBody
	return &t
}

func (k ink) state(s vault.State) ui.Color {
	switch s {
	case vault.Overdue:
		return k.danger
	case vault.DueSoon:
		return k.gold
	}
	return k.green
}

func (a *app) view(c *ui.Context) {
	k := palette(c)
	c.SetTheme(k.theme(c.Theme()))
	c.Root().TextColor(k.text).Background(k.bg).Radius(12).BorderWidth(1).BorderColor(k.line).FontSize(fsBody)
	c.OnShortcut(ui.Cmd, ui.KeyQ, a.quit)
	if len(a.pending) > 0 {
		a.approvalView(c, k)
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyComma) {
		a.openSettings()
	}
	if c.Shortcut(ui.Cmd, ui.KeySlash) {
		a.cheat = !a.cheat
	}
	if a.cheat {
		if c.Shortcut(0, ui.KeyEscape) {
			a.cheat = false
		}
		a.cheatsheet(c, k)
		return
	}
	switch a.page {
	case pageDetail:
		a.detailView(c, k)
	case pageForm:
		a.formView(c, k)
	case pageLog:
		a.logView(c, k)
	case pageSettings:
		a.settingsView(c, k)
	default:
		a.listView(c, k)
	}
}

// The list

func (a *app) keysList(c *ui.Context) {
	n := len(a.rows())
	switch {
	case c.Shortcut(0, ui.KeyEscape):
		if a.query != "" {
			a.query = ""
			a.chosen = 0
		} else {
			a.hide()
		}
	case c.Shortcut(0, ui.KeyDown):
		a.choose(a.chosen + 1)
	case c.Shortcut(0, ui.KeyUp):
		a.choose(a.chosen - 1)
	case c.Shortcut(0, ui.KeyEnter):
		if k, ok := a.chosenKey(); ok {
			a.openDetail(k.Name)
		}
	case c.Shortcut(ui.Cmd, ui.KeyN):
		a.openAdd()
	case c.Shortcut(ui.Cmd, ui.KeyR):
		if k, ok := a.chosenKey(); ok {
			a.openRotate(k.Name)
		}
	case c.Shortcut(ui.Cmd, ui.KeyY):
		a.openLog()
	case c.Shortcut(ui.Cmd, ui.KeyC):
		if k, ok := a.chosenKey(); ok {
			a.copyValue(k.Name)
		}
	case c.Shortcut(ui.Cmd, ui.KeyF), c.Shortcut(ui.Cmd, ui.KeyL):
		a.input.Focus()
	}
	_ = n
}

func (a *app) listView(c *ui.Context, k ink) {
	a.keysList(c)
	ui.Column(c).Fill().Children(func() {
		a.grip(ui.Row(c.Key("header"))).Padding(sp3, sp3, sp3, sp3).Gap(sp1).AlignItems(ui.Center).Children(func() {
			a.searchField(c.Key("search"), k)
			ui.Box(c).Width(sp1)
			iconButton(c.Key("add"), k, iconPlus, "添加密钥  "+hk("⌘N"), a.openAdd)
			iconButton(c.Key("log"), k, iconLog, "访问记录  "+hk("⌘Y"), a.openLog)
			iconButton(c.Key("gear"), k, iconGear, "设置  "+hk("⌘,"), a.openSettings)
		})
		ui.Divider(c)
		rows := a.rows()
		a.list.Key = func(i int) any {
			if i < len(rows) {
				return rows[i].Name
			}
			return i
		}
		ui.Column(c.Key("main")).Grow(1).MinHeight(0).Children(func() {
			a.banner(c, k)
			ui.List(c.Key("list"), &a.list, len(rows), func(i int) {
				a.row(c.Key(rows[i].Name), k, i, rows[i], i == len(rows)-1)
			}).Grow(1).MinHeight(0).Padding(sp2, sp2).Gap(0).Label("密钥列表").Children(func() {
				if len(rows) == 0 {
					if len(a.inv.Keys) > 0 {
						empty(c, k, iconSearch, "没有匹配「"+a.query+"」的密钥", "试试名称、说明、平台或项目名。")
					} else {
						empty(c, k, iconKey, "还没有密钥", "按 "+hk("⌘N")+" 添加，或在终端运行 keys import .env 导入现有的 .env。")
					}
				}
			})
		})
		store, _, _ := strings.Cut(storeName(goos), "（")
		left := fmt.Sprintf("%d 个密钥 · 值在 %s", len(a.inv.Keys), store)
		if a.demo {
			left = "示例数据 · " + left
		}
		if a.invErr != "" {
			left = a.invErr
		}
		if a.toast != "" {
			left = a.toast
		}
		a.hintBar(c, k, left, "↑↓", "选择", "↵", "详情", "⌘R", "轮换", "⌘N", "添加", "⌘/", "快捷键")
	})
}

// searchField is the list's search: a rounded field with its icon.
func (a *app) searchField(c *ui.Context, k ink) {
	box := ui.Row(c).Grow(1).Height(34).Padding(0, sp3-2).Gap(sp2).Radius(8).AlignItems(ui.Center).
		Background(k.sunk).BorderWidth(1).BorderColor(k.line)
	box.Children(func() {
		ui.Icon(c, iconSearch).FontSize(15).TextColor(k.faint)
		in := ui.TextInputBase(c.Key("q"), &a.query).Bind(&a.input).Grow(1).FontSize(14).
			Placeholder("搜索密钥、平台、项目").Label("搜索")
		if in.Changed() {
			a.chosen = 0
		}
		if in.Submitted() {
			if key, ok := a.chosenKey(); ok {
				a.openDetail(key.Name)
			}
		}
		if a.focusInput {
			a.focusInput = false
			in.AutoFocus()
			a.input.Focus()
		}
		if a.query != "" {
			ui.Box(c.Key("clear")).Padding(2).Radius(4).Cursor(ui.CursorPointer).Label("清空").Tooltip("清空  esc").
				OnClick(func() { a.query, a.chosen = "", 0 }).Children(func() {
				ui.Icon(c, iconX).FontSize(12).TextColor(k.faint)
			})
		}
	})
}

func iconButton(c *ui.Context, k ink, icon *ui.SVG, tip string, fn func()) {
	label, _, _ := strings.Cut(tip, "  ")
	b := ui.Box(c).Size(30, 30).Radius(7).Center().Cursor(ui.CursorPointer).Tooltip(tip).Label(label).OnClick(fn)
	if b.Hovered() {
		b.Background(k.hover)
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(16).TextColor(k.muted)
	})
}

// banner is what needs doing, over the list.
func (a *app) banner(c *ui.Context, k ink) {
	over, soon, pend := a.counts()
	if over+soon+pend == 0 {
		return
	}
	var parts []string
	if over > 0 {
		parts = append(parts, fmt.Sprintf("%d 个已超期", over))
	}
	if soon > 0 {
		parts = append(parts, fmt.Sprintf("%d 个快到期", soon))
	}
	if pend > 0 {
		parts = append(parts, fmt.Sprintf("%d 个轮换后还有平台没更新", pend))
	}
	col := k.gold
	if over > 0 {
		col = k.danger
	}
	callout(c.Key("banner"), k, col, iconAlert, strings.Join(parts, " · ")).Margin(sp2, sp3, 0, sp3)
}

// callout is a tinted note with an icon.
func callout(c *ui.Context, k ink, col ui.Color, icon *ui.SVG, text string) ui.Element {
	return ui.Row(c).Gap(sp2).Padding(sp2, sp3).Radius(8).AlignItems(ui.Start).
		Background(col.Alpha(0.09)).BorderWidth(1).BorderColor(col.Alpha(0.22)).Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(col).Margin(1, 0, 0, 0)
		ui.Text(c, text).FontSize(fsSmall).FontWeight(500).TextColor(col).Grow(1).Shrink(1).LineHeight(1.45)
	})
}

// row is a key in the list: its name and a line of what and where, and
// its state in a pill on the right.
func (a *app) row(c *ui.Context, k ink, i int, key vault.Key, last bool) {
	now := a.now()
	st := a.inv.StatusOf(key, now)
	ui.Column(c).Children(func() {
		row := ui.Row(c.Key("r")).Padding(sp2+1, sp3).Radius(8).Gap(sp3).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		bg := ui.Transparent
		if i == a.chosen {
			bg = k.chosen
		} else if row.Hovered() {
			bg = k.hover
		}
		row.Background(bg).OnClick(func() {
			if a.chosen == i {
				a.openDetail(key.Name)
			} else {
				a.choose(i)
			}
		})
		row.Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
				ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
					ui.Text(c, key.Name).FontSize(fsBody).FontWeight(600).Font(mono).SingleLine().Shrink(1)
					if key.Agent == "deny" {
						tag(c, k, "代理禁用")
					}
				})
				var meta []string
				if key.Description != "" {
					meta = append(meta, key.Description)
				}
				if len(key.Platforms) > 0 {
					var ps []string
					for _, p := range key.Platforms {
						ps = append(ps, vault.KindName(p.Kind))
					}
					meta = append(meta, strings.Join(dedup(ps), "、"))
				}
				meta = append(meta, vault.Ago(key.LastRotated, now)+"轮换")
				ui.Text(c, strings.Join(meta, " · ")).FontSize(fsMeta + 0.5).TextColor(k.faint).SingleLine()
			})
			text, col := "正常", k.green
			switch {
			case st.State == vault.Overdue:
				text, col = st.Why, k.danger
			case st.Pending > 0:
				text, col = fmt.Sprintf("%d 个平台待更新", st.Pending), k.gold
			case st.State != vault.OK:
				text, col = st.Why, k.state(st.State)
			}
			pill(c, text, col).Tooltip(st.State.Zh())
		})
		// A hairline between rows, not beside the chosen one.
		if !last && i != a.chosen && i+1 != a.chosen {
			ui.Box(c.Key("sep")).Height(1).Margin(0, sp3).Background(k.sep)
		} else if !last {
			ui.Box(c.Key("sep")).Height(1)
		}
	})
}

func dedup(ss []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// pill is a state: colored text on its tint, fully rounded.
func pill(c *ui.Context, text string, col ui.Color) ui.Element {
	return ui.Text(c, text).FontSize(fsMeta).FontWeight(600).TextColor(col).Padding(2, sp2).Radius(10).
		Background(col.Alpha(0.12)).SingleLine().FontFeatures("tnum")
}

// tag is a neutral label beside a name.
func tag(c *ui.Context, k ink, text string) ui.Element {
	return ui.Text(c, text).FontSize(10).FontWeight(500).TextColor(k.faint).Padding(1, 6).Radius(4).
		BorderWidth(1).BorderColor(k.line).SingleLine()
}

// keycap is a key as printed on the keyboard.
func keycap(c *ui.Context, k ink, key string) ui.Element {
	return ui.Text(c, hk(key)).FontSize(10).FontWeight(500).TextColor(k.muted).Padding(0, 5).Radius(4).
		Background(k.bg).BorderWidth(1, 1, 2, 1).BorderColor(k.line).SingleLine().MinWidth(18).TextAlign(ui.Center)
}

// empty is a list's empty state: an icon, a title and what to do.
func empty(c *ui.Context, k ink, icon *ui.SVG, title, body string) {
	ui.Column(c.Key("empty")).Padding(48, 32).Gap(sp2).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(44, 44).Radius(22).Center().Background(k.sunk).Children(func() {
			ui.Icon(c, icon).FontSize(20).TextColor(k.faint)
		})
		ui.Text(c, title).FontSize(fsBody+1).FontWeight(600).Margin(sp1, 0, 0, 0)
		ui.Text(c, body).FontSize(fsSmall).TextColor(k.muted).LineHeight(1.5).TextAlign(ui.Center)
	})
}

// hintBar is the footer: a note on the left, keys and what they do on
// the right.
func (a *app) hintBar(c *ui.Context, k ink, left string, pairs ...string) {
	ui.Divider(c)
	a.grip(ui.Row(c.Key("hints"))).Padding(sp2, sp3+2).Gap(sp3).AlignItems(ui.Center).Background(k.sunk).Children(func() {
		ui.Text(c, left).FontSize(fsMeta).TextColor(k.faint).Grow(1).Shrink(1).MinWidth(0).SingleLine()
		ui.Row(c.Key("keys")).Gap(sp3).AlignItems(ui.Center).Shrink(0).Children(func() {
			for i := 0; i+1 < len(pairs); i += 2 {
				ui.Row(c.Key(i)).Gap(sp1 + 1).AlignItems(ui.Center).Children(func() {
					for _, kk := range strings.Fields(pairs[i]) {
						keycap(c.Key(kk), k, kk)
					}
					ui.Text(c, pairs[i+1]).FontSize(fsMeta).TextColor(k.faint).SingleLine()
				})
			}
		})
	})
}

// head is a page's title bar, with 返回.
func (a *app) head(c *ui.Context, k ink, title string, titleMono bool, right func()) {
	a.grip(ui.Row(c.Key("head"))).Height(52).Padding(0, sp3, 0, sp2).Gap(sp2).AlignItems(ui.Center).Children(func() {
		b := ui.Row(c.Key("back")).Gap(2).Padding(sp1, sp2, sp1, sp1).Radius(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).
			Label("返回").Tooltip("返回  esc").OnClick(a.back)
		if b.Hovered() {
			b.Background(k.hover)
		}
		b.Children(func() {
			ui.Icon(c, iconBack).FontSize(16).TextColor(k.muted)
			ui.Text(c, "返回").FontSize(fsBody).TextColor(k.muted)
		})
		ui.Box(c).Width(1).Height(16).Background(k.line).Margin(0, sp1)
		t := ui.Text(c, title).FontSize(fsTitle).FontWeight(650).Grow(1).Shrink(1).MinWidth(0).SingleLine()
		if titleMono {
			t.Font(mono).FontSize(fsTitle - 1)
		}
		if right != nil {
			right()
		}
	})
	ui.Divider(c)
}

// group is a titled card of rows, as in System Settings.
func group(c *ui.Context, k ink, title, desc string, body func()) {
	ui.Column(c.Key(title)).Gap(sp2).Children(func() {
		if title != "" || desc != "" {
			ui.Column(c).Gap(2).Padding(0, 2).Children(func() {
				if title != "" {
					ui.Text(c, title).FontSize(fsSmall).FontWeight(600).TextColor(k.muted)
				}
				if desc != "" {
					ui.Text(c, desc).FontSize(fsMeta + 0.5).TextColor(k.faint).LineHeight(1.45)
				}
			})
		}
		card(c.Key("card"), k, body)
	})
}

func card(c *ui.Context, k ink, body func()) ui.Element {
	return ui.Column(c).Radius(10).Background(k.card).BorderWidth(1).BorderColor(k.line).Clip().Children(body)
}

// cardRow is a row of a card, under a hairline unless first.
func cardRow(c *ui.Context, k ink, first bool) ui.Element {
	r := ui.Row(c).Padding(sp2+2, sp3).Gap(sp3).AlignItems(ui.Center).MinHeight(40)
	if !first {
		r.BorderWidth(1, 0, 0, 0).BorderColor(k.sep)
	}
	return r
}

// option is a card row with a switch.
func option(c *ui.Context, k ink, first bool, label, desc string, on *bool, changed func()) {
	cardRow(c.Key(label), k, first).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(fsBody)
			if desc != "" {
				ui.Text(c, desc).FontSize(fsMeta + 0.5).TextColor(k.faint).LineHeight(1.4)
			}
		})
		if ui.Switch(c.Key("sw"), on).Label(label).Changed() {
			changed()
		}
	})
}

// field is a card row: a label and its value.
func field(c *ui.Context, k ink, first bool, label, value string) ui.Element {
	return cardRow(c.Key(label), k, first).Padding(sp2+1, sp3).MinHeight(0).AlignItems(ui.Baseline).Children(func() {
		ui.Text(c, label).FontSize(fsSmall).TextColor(k.faint).Width(72).Shrink(0)
		ui.Text(c, value).FontSize(fsBody).Grow(1).Shrink(1).MinWidth(0).LineHeight(1.4).Selectable()
	})
}

// page is a scrolling page of groups on the sunk background.
func sunkPage(c *ui.Context, k ink, key string, body func()) {
	ui.Scroll(c.Key(key)).Grow(1).MinHeight(0).Background(k.sunk).Children(func() {
		ui.Column(c.Key("content")).Shrink(0).Padding(sp4, sp4, sp5, sp4).Gap(sp5).Children(body)
	})
}

// The detail

func (a *app) detailView(c *ui.Context, k ink) {
	key, ok := a.key(a.detail)
	if !ok {
		a.page = pageList
		a.listView(c, k)
		return
	}
	now := a.now()
	st := a.inv.StatusOf(key, now)
	if !a.confirm && !a.typing {
		switch {
		case c.Shortcut(0, ui.KeyEscape):
			a.back()
		case c.Shortcut(ui.Cmd, ui.KeyR):
			a.openRotate(key.Name)
		case c.Shortcut(ui.Cmd, ui.KeyC):
			a.copyValue(key.Name)
		case c.Shortcut(ui.Cmd, ui.KeyBackspace):
			a.confirm = true
		}
	}
	a.typing = false
	ui.Column(c).Fill().Children(func() {
		a.head(c, k, key.Name, true, func() {
			ui.Button(c.Key("copy"), "复制值").FontSize(fsSmall+0.5).Padding(5, sp3).Tooltip(hk("⌘C") + "，30 秒后清除剪贴板").OnClick(func() { a.copyValue(key.Name) })
			ui.PrimaryButton(c.Key("rotate"), "轮换…").FontSize(fsSmall+0.5).FontWeight(500).Padding(5, sp3).Tooltip(hk("⌘R")).OnClick(func() { a.openRotate(key.Name) })
		})
		sunkPage(c, k, "body", func() {
			// The state, in words, over the facts.
			col := k.state(st.State)
			state := st.State.Zh()
			if st.Why != "" {
				state = st.Why
			}
			ui.Row(c.Key("summary")).Gap(sp2).AlignItems(ui.Center).Padding(0, 2).Children(func() {
				pill(c, state, col)
				if st.Pending > 0 {
					pill(c, fmt.Sprintf("%d 个平台待更新", st.Pending), k.gold)
				}
				if key.Agent == "deny" {
					tag(c, k, "代理禁用")
				}
				if key.Description != "" {
					ui.Text(c, key.Description).FontSize(fsSmall).TextColor(k.muted).Grow(1).Shrink(1).MinWidth(0).SingleLine()
				}
			})
			group(c, k, "", "", func() {
				field(c, k, true, "上次轮换", day(key.LastRotated)+" · "+vault.Ago(key.LastRotated, now))
				if !st.Due.IsZero() {
					field(c, k, false, "下次轮换", fmt.Sprintf("%s · 每 %d 天", day(st.Due), int(a.inv.RotateEvery(key).Hours()/24)))
				} else {
					field(c, k, false, "下次轮换", "不提醒")
				}
				if !key.Expires.IsZero() {
					field(c, k, false, "过期", day(key.Expires))
				}
				field(c, k, false, "创建", day(key.Created))
				if len(key.Projects) > 0 {
					field(c, k, false, "使用者", strings.Join(key.Projects, "、"))
				}
				if len(key.Tags) > 0 {
					field(c, k, false, "标签", strings.Join(key.Tags, "、"))
				}
			})
			a.platformsSection(c, k, key)
			allow := key.Agent != "deny"
			group(c, k, "AI 代理", "", func() {
				option(c, k, true, "允许代理申请使用", "代理通过 MCP 或命令行用它时，每次都要你批准；关掉后一律拒绝。", &allow, func() {
					a.setAgent(key.Name, allow)
				})
			})
			if len(key.History) > 0 {
				group(c, k, "轮换历史", "", func() {
					n := 0
					for i := len(key.History) - 1; i >= 0 && i >= len(key.History)-8; i-- {
						h := key.History[i]
						by := map[string]string{"cli": "命令行", "app": "KeyBar"}[h.By]
						cardRow(c.Key(i), k, n == 0).Children(func() {
							ui.Text(c, h.At.Local().Format("2006-01-02 15:04")).FontSize(fsSmall).TextColor(k.muted).FontFeatures("tnum").Width(118).Shrink(0)
							ui.Text(c, h.Note).FontSize(fsSmall).Grow(1).Shrink(1).MinWidth(0).SingleLine()
							if by != "" {
								ui.Text(c, by).FontSize(fsMeta).TextColor(k.faint)
							}
						})
						n++
					}
				})
			}
			if evs := a.accessFor(key.Name, 5); len(evs) > 0 {
				group(c, k, "最近访问", "", func() {
					for i, ev := range evs {
						a.eventRow(c.Key(i), k, ev, false, i == 0)
					}
				})
			}
			ui.Row(c.Key("danger")).Children(func() {
				d := ui.Text(c.Key("rm"), "删除密钥…").FontSize(fsSmall).FontWeight(500).TextColor(k.danger).Padding(sp1, sp2).Radius(6).
					Cursor(ui.CursorPointer).Label("删除密钥").Tooltip(hk("⌘⌫")).OnClick(func() { a.confirm = true })
				if d.Hovered() {
					d.Background(k.danger.Alpha(0.08))
				}
			})
		})
		left := a.toast
		if left == "" {
			left = "值只在钥匙串里；这里只有元数据"
		}
		a.hintBar(c, k, left, "⌘R", "轮换", "⌘C", "复制值", "esc", "返回")
	})
	msg := "钥匙串里的值也会删除。"
	if len(key.Platforms) > 0 {
		msg = fmt.Sprintf("钥匙串里的值也会删除；它还存在于 %d 个平台，删除后不再提醒你轮换它们。", len(key.Platforms))
	}
	if ui.AlertDialog(c, &a.confirm, "删除 "+key.Name+"？", msg, "取消", "删除") == 1 {
		a.removeKey(key.Name)
	}
}

func day(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format(time.DateOnly)
}

func (a *app) platformsSection(c *ui.Context, k ink, key vault.Key) {
	desc := "轮换后逐个更新并勾选；GitHub Actions 和 Vercel 可以自动更新。"
	if len(key.Platforms) == 0 {
		desc = "记下它还存在哪里（GitHub Actions、Vercel、Railway、某个 .env…），轮换时就有清单。"
	}
	group(c, k, fmt.Sprintf("也存在于 · %d", len(key.Platforms)), desc, func() {
		for i, p := range key.Platforms {
			done := !p.Stale(key)
			id := key.Name + "/" + p.ID
			cardRow(c.Key(p.ID), k, i == 0).Children(func() {
				if ui.Checkbox(c.Key("cb"), &done, "").Label("已更新 " + p.Label()).Changed() {
					a.togglePlatform(key.Name, p, done)
				}
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, vault.KindName(p.Kind)).FontSize(fsBody).FontWeight(500).SingleLine().Shrink(0)
						ui.Text(c, p.Target).FontSize(fsSmall).TextColor(k.muted).SingleLine().Shrink(1).MinWidth(0)
					})
					var sub []string
					if v := p.VarName(key.Name); v != key.Name {
						sub = append(sub, "变量 "+v)
					}
					if len(p.Envs) > 0 {
						sub = append(sub, "环境 "+strings.Join(p.Envs, ","))
					}
					if p.Note != "" {
						sub = append(sub, p.Note)
					}
					col := k.faint
					switch m := a.syncMsg[id]; {
					case m != "":
						sub = append(sub, m)
						if strings.HasPrefix(m, "✗") {
							col = k.danger
						}
					case !done:
						sub = append(sub, "还是旧值")
						col = k.gold
					case !p.Updated.IsZero():
						sub = append(sub, "更新于 "+day(p.Updated))
					}
					if len(sub) > 0 {
						ui.Text(c, strings.Join(sub, " · ")).FontSize(fsMeta).TextColor(col).SingleLine()
					}
				})
				if p.Auto() {
					if a.syncing[id] {
						ui.Spinner(c).Size(14, 14)
					} else {
						ui.Button(c.Key("sync"), "自动更新").FontSize(fsSmall).Padding(3, 10).
							Tooltip("用 " + vault.KindName(p.Kind) + " 的 API 写入当前值").OnClick(func() { a.syncPlatform(key.Name, p, nil) })
					}
				}
				x := ui.Box(c.Key("rm")).Size(22, 22).Center().Radius(5).Cursor(ui.CursorPointer).Tooltip("从清单移除").Label("移除 " + p.Label()).
					OnClick(func() { a.removePlatform(key.Name, p.ID) })
				if x.Hovered() {
					x.Background(k.hover)
				}
				x.Children(func() { ui.Icon(c, iconX).FontSize(12).TextColor(k.faint) })
			})
		}
		cardRow(c.Key("addplat"), k, len(key.Platforms) == 0).Gap(sp2).Children(func() {
			ui.Icon(c, iconPlus).FontSize(14).TextColor(k.faint).Margin(0, 3)
			in := ui.TextInputBase(c.Key("plat"), &a.addPlat).Label("添加平台").FontSize(fsSmall).
				Placeholder("添加：github:owner/repo、vercel:my-app、railway:api").Grow(1)
			if in.Submitted() {
				a.addPlatform(key.Name)
			}
			if in.Focused() {
				a.typing = true
			}
			if strings.TrimSpace(a.addPlat) != "" {
				ui.Button(c.Key("addbtn"), "添加").FontSize(fsSmall).Padding(3, 10).OnClick(func() { a.addPlatform(key.Name) })
			}
		})
	})
}

// The form: add a key, or rotate one.

func (a *app) formView(c *ui.Context, k ink) {
	f := &a.form
	if c.Shortcut(0, ui.KeyEscape) {
		a.back()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyEnter) {
		a.submit()
	}
	title, titleMono := "添加密钥", false
	if f.rotate {
		title, titleMono = "轮换 "+f.name, true
	}
	ui.Column(c).Fill().Children(func() {
		a.head(c, k, title, titleMono, nil)
		input := func(key, label, ph string, v *string, password, focus bool) {
			ui.Column(c.Key(key)).Gap(6).Children(func() {
				ui.Text(c, label).FontSize(fsSmall).FontWeight(500).TextColor(k.muted)
				in := ui.TextInput(c.Key("in"), v).Label(label).Placeholder(ph).OnSubmit(a.submit)
				if password {
					in.Password()
				}
				if focus {
					in.AutoFocus()
				}
			})
		}
		half := func(key string, fn func()) { ui.Column(c.Key(key)).Grow(1).Basis(0).Children(fn) }
		sunkPage(c, k, "form", func() {
			if f.rotate {
				key, _ := a.key(f.name)
				callout(c.Key("how"), k, k.link, iconAlert, "先在服务商那里生成新密钥，再粘贴到这里。保存后钥匙串里的旧值即被替换。")
				ui.Column(c.Key("vals")).Gap(sp3).Children(func() {
					input("v1", "新值", "粘贴新密钥", &f.value, true, true)
					input("v2", "再输入一次", "", &f.value2, true, false)
					input("note", "备注（可选）", "例如：Railway 安全通告后轮换", &f.note, false, false)
				})
				if len(key.Platforms) > 0 {
					group(c, k, "保存后", "", func() {
						for i, p := range key.Platforms {
							cardRow(c.Key(p.ID), k, i == 0).Children(func() {
								ui.Text(c, vault.KindName(p.Kind)).FontSize(fsBody).FontWeight(500).Shrink(0)
								ui.Text(c, p.Target).FontSize(fsSmall).TextColor(k.muted).Grow(1).Shrink(1).MinWidth(0).SingleLine()
								if p.Auto() {
									pill(c, "自动更新", k.green)
								} else {
									pill(c, "手动更新", k.gold).Tooltip("更新后在清单里勾选")
								}
							})
						}
					})
				}
			} else {
				ui.Column(c.Key("vals")).Gap(sp3).Children(func() {
					input("name", "名称", "OPENAI_API_KEY", &f.name, false, true)
					input("value", "值", "只存进"+storeName(goos), &f.value, true, false)
					input("desc", "说明", "例如：OpenAI 个人账号，用于 rz-system", &f.desc, false, false)
					ui.Column(c.Key("plats")).Gap(6).Children(func() {
						ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "也存在于").FontSize(fsSmall).FontWeight(500).TextColor(k.muted)
							ui.Text(c, "每行一个 kind:target").FontSize(fsMeta).TextColor(k.faint)
						})
						ui.TextArea(c.Key("in"), &f.platforms).Label("也存在于").Placeholder("github:horizon0514/lb-bar\nvercel:gaotie-xiaosiji\nrailway:api").Height(76).Font(mono).FontSize(fsSmall)
					})
					ui.Row(c.Key("nums")).Gap(sp3).Children(func() {
						half("d", func() {
							input("days", "轮换周期（天）", fmt.Sprintf("默认 %d", vault.DefaultRotateDays), &f.days, false, false)
						})
						half("e", func() { input("exp", "过期日（可选）", "2027-01-19", &f.expires, false, false) })
					})
					ui.Row(c.Key("meta")).Gap(sp3).Children(func() {
						half("p", func() {
							input("projects", "使用者（逗号分隔）", "rz-system, lb-bar", &f.projects, false, false)
						})
						half("t", func() { input("tags", "标签", "prod, ai", &f.tags, false, false) })
					})
				})
			}
			if f.err != "" {
				callout(c.Key("err"), k, k.danger, iconAlert, f.err)
			}
		})
		ui.Divider(c)
		ui.Row(c.Key("btns")).Gap(sp2).Padding(sp3, sp4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "值不会写进任何文件或日志").FontSize(fsMeta).TextColor(k.faint).Grow(1).Shrink(1).SingleLine()
			keyButton(c.Key("cancel"), k, "取消", "esc", false, a.back)
			keyButton(c.Key("save"), k, "保存", "⌘↵", true, a.submit)
		})
	})
}

// keyButton is a button with its key on a keycap; primary in the accent
// color.
func keyButton(c *ui.Context, k ink, label, key string, primary bool, fn func()) ui.Element {
	var b ui.Element
	if primary {
		b = ui.PrimaryButton(c, "")
	} else {
		b = ui.Button(c, "")
	}
	b.Label(label).Tooltip(hk(key)).OnClick(fn).Padding(5, sp3).Gap(sp2).Children(func() {
		ui.Text(c, label).FontSize(fsBody).FontWeight(500).SingleLine()
		cap := ui.Text(c, hk(key)).FontSize(10).FontWeight(500).Padding(0, 4).Radius(4).SingleLine()
		if primary {
			cap.TextColor(ui.RGB(255, 255, 255).Alpha(0.85)).Background(ui.RGB(255, 255, 255).Alpha(0.18))
		} else {
			cap.TextColor(k.faint).BorderWidth(1).BorderColor(k.line)
		}
	})
	return b
}

// The access log

func (a *app) logView(c *ui.Context, k ink) {
	if c.Shortcut(0, ui.KeyEscape) {
		a.back()
		return
	}
	evs := vault.ReadLog(a.v.Dir, 300, nil)
	ui.Column(c).Fill().Children(func() {
		a.head(c, k, "访问记录", false, func() {
			ui.Text(c, fmt.Sprintf("最近 %d 条", len(evs))).FontSize(fsMeta).TextColor(k.faint)
		})
		sunkPage(c, k, "log", func() {
			if len(evs) == 0 {
				empty(c, k, iconLog, "还没有访问记录", "程序、命令行和 AI 代理每次用密钥都会记在这里（只记名字，不记值）。")
				return
			}
			card(c.Key("events"), k, func() {
				for i := len(evs) - 1; i >= 0; i-- {
					a.eventRow(c.Key(i), k, evs[i], true, i == len(evs)-1)
				}
			})
		})
		a.hintBar(c, k, "记录在 audit.jsonl，只有名字和结果", "esc", "返回")
	})
}

var actionNames = map[string]string{"get": "读取", "reveal": "读明文", "run": "运行", "copy": "复制", "rotate": "轮换", "add": "添加",
	"rm": "删除", "sync": "同步", "import": "导入", "list": "列出"}

func (a *app) eventRow(c *ui.Context, k ink, ev vault.Event, keys, first bool) {
	col := k.faint
	switch {
	case ev.Decision == "denied", ev.Decision == "timeout", ev.Decision == "error":
		col = k.danger
	case strings.HasPrefix(ev.Decision, "allowed"):
		col = k.green
	}
	cardRow(c, k, first).Padding(sp2, sp3).MinHeight(0).Children(func() {
		ui.Text(c, ev.Time.Local().Format("01-02 15:04")).FontSize(fsMeta).TextColor(k.faint).FontFeatures("tnum").Width(70).Shrink(0).SingleLine()
		ui.Text(c, approval.ClientName(ev.Client)).FontSize(fsSmall).FontWeight(500).Width(84).Shrink(0).SingleLine()
		act := actionNames[ev.Action]
		if act == "" {
			act = ev.Action
		}
		ui.Row(c).Gap(6).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
			ui.Text(c, act).FontSize(fsSmall).TextColor(k.muted).Shrink(0)
			what := ""
			if keys && len(ev.Keys) > 0 {
				what = strings.Join(ev.Keys, "、")
			}
			if ev.Detail != "" {
				if what != "" {
					what += " · "
				}
				what += ev.Detail
			}
			if what != "" {
				ui.Text(c, what).FontSize(fsSmall).Font(mono).Shrink(1).MinWidth(0).SingleLine()
			}
		})
		ui.Text(c, vault.DecisionZh(ev.Decision)).FontSize(fsMeta).FontWeight(500).TextColor(col).Shrink(0).SingleLine()
	})
}

// Approval: an agent's request takes the panel until answered.

func (a *app) approvalView(c *ui.Context, k ink) {
	p := a.pending[0]
	r := p.req
	// A moment before keys count, so that a key typed elsewhere as the
	// panel appears does not answer.
	ready := a.now().Sub(p.at) > 600*time.Millisecond
	if !ready {
		c.After(650 * time.Millisecond)
	}
	if ready {
		switch {
		case c.Shortcut(ui.Cmd, ui.KeyEnter):
			a.decide(true, true)
			return
		case c.Shortcut(0, ui.KeyEnter):
			a.decide(true, false)
			return
		case c.Shortcut(0, ui.KeyEscape):
			a.decide(false, false)
			return
		}
	}
	left := approval.Timeout - a.now().Sub(p.at)
	c.After(time.Second)
	reveal := r.Action == "reveal"
	ui.Column(c).Fill().Children(func() {
		a.grip(ui.Row(c.Key("head"))).Height(52).Padding(0, sp4).Gap(sp2 + 2).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(26, 26).Radius(13).Center().Background(k.gold.Alpha(0.14)).Children(func() {
				ui.Icon(c, iconShield).FontSize(14).TextColor(k.gold)
			})
			ui.Text(c, "需要你批准").FontSize(fsTitle).FontWeight(650).Grow(1)
			if n := len(a.pending); n > 1 {
				pill(c, fmt.Sprintf("还有 %d 个", n-1), k.gold)
			}
			ui.Text(c, fmt.Sprintf("%d 秒后自动拒绝", max(0, int(left.Seconds())))).FontSize(fsMeta).TextColor(k.faint).FontFeatures("tnum")
		})
		ui.Divider(c)
		ui.Scroll(c.Key("req")).Grow(1).MinHeight(0).Padding(sp5, sp5, sp4, sp5).Gap(sp4).Children(func() {
			who := approval.ClientName(r.Client)
			what := "要用这些密钥运行一个命令"
			if reveal {
				what = "要读取密钥的明文值"
			}
			ui.Column(c.Key("ask")).Gap(sp2 + 2).Children(func() {
				ui.Text(c, who+approval.Sep(who)+what).FontSize(17).FontWeight(650).LineHeight(1.35)
				ui.Row(c.Key("keys")).Gap(6).Wrap().Children(func() {
					for _, name := range r.Keys {
						ui.Row(c.Key(name)).Gap(5).Padding(3, sp2).Radius(6).AlignItems(ui.Center).
							Background(k.sunk).BorderWidth(1).BorderColor(k.line).Children(func() {
							ui.Icon(c, iconKey).FontSize(12).TextColor(k.gold)
							ui.Text(c, name).FontSize(fsSmall).FontWeight(600).Font(mono)
						})
					}
				})
			})
			if r.Command != "" {
				ui.Column(c.Key("cmd")).Gap(6).Children(func() {
					ui.Text(c, "命令").FontSize(fsMeta).FontWeight(600).TextColor(k.faint)
					ui.Row(c).Padding(sp2+2, sp3).Radius(8).Background(k.code).BorderWidth(1).BorderColor(k.line).Gap(sp2).Children(func() {
						ui.Text(c, "$").FontSize(fsSmall).Font(mono).TextColor(k.faint)
						ui.Text(c, r.Command).FontSize(fsSmall).Font(mono).LineHeight(1.45).MaxLines(6).Grow(1).Shrink(1).MinWidth(0).Selectable()
					})
				})
			}
			card(c.Key("facts"), k, func() {
				field(c, k, true, "来自", r.Client).Padding(sp2, sp3).MinHeight(0)
				if r.Cwd != "" {
					field(c, k, false, "目录", r.Cwd).Padding(sp2, sp3).MinHeight(0)
				}
				if r.Reason != "" {
					field(c, k, false, "理由", r.Reason).Padding(sp2, sp3).MinHeight(0)
				}
			})
			switch {
			case reveal:
				callout(c.Key("note"), k, k.gold, iconAlert, "批准后代理会拿到明文值。除非确有必要，建议拒绝，让它改用 run_with_secrets。")
			case r.Plain:
				callout(c.Key("note"), k, k.gold, iconAlert, "值只注入这个命令的进程，但输出不脱敏：命令打印出的值，调用它的程序都能看到。让它加上 --redact 更安全。")
			default:
				ui.Row(c.Key("note")).Gap(sp2).Padding(0, 2).AlignItems(ui.Start).Children(func() {
					ui.Icon(c, iconShield).FontSize(13).TextColor(k.green).Margin(2, 0, 0, 0)
					ui.Text(c, "值只注入这个命令的进程，输出里的值会被替换成 [REDACTED]，代理看不到明文。").FontSize(fsSmall).TextColor(k.muted).LineHeight(1.45).Grow(1).Shrink(1)
				})
			}
		})
		ui.Divider(c)
		ui.Row(c.Key("btns")).Gap(sp2).Padding(sp3, sp4).AlignItems(ui.Center).Background(k.sunk).Children(func() {
			keyButton(c.Key("deny"), k, "拒绝", "esc", false, func() { a.decide(false, false) })
			ui.Spacer(c)
			keyButton(c.Key("timed"), k, fmt.Sprintf("允许 %d 分钟", a.settings.GrantMinutes), "⌘↵", false, func() { a.decide(true, true) }).
				Tooltip("这段时间内这个代理再用这些密钥不再询问")
			keyButton(c.Key("once"), k, "允许一次", "↵", true, func() { a.decide(true, false) })
		})
	})
}

// The settings: one page, three tabs.

func (a *app) settingsView(c *ui.Context, k ink) {
	if c.Shortcut(0, ui.KeyEscape) {
		a.back()
		return
	}
	for i, key := range []ui.Key{ui.Key1, ui.Key2, ui.Key3} {
		if c.Shortcut(ui.Cmd, key) {
			a.openTab(i)
		}
	}
	ui.Column(c).Fill().Children(func() {
		a.head(c, k, "设置", false, func() {
			ui.Text(c, "KeyBar "+version).FontSize(fsMeta).TextColor(k.faint)
		})
		ui.Row(c.Key("body")).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c.Key("tabs")).Width(132).Padding(sp3, sp2).Gap(2).Background(k.sunk).BorderWidth(0, 1, 0, 0).BorderColor(k.line).Children(func() {
				for i, name := range tabNames {
					on := i == a.tab
					item := ui.Row(c.Key(name)).Padding(6, sp2+2).Radius(6).AlignItems(ui.Center).Gap(sp2).Cursor(ui.CursorPointer).
						Focusable().Label(name).OnClick(func() { a.openTab(i) })
					fg, w := k.muted, 400
					switch {
					case on:
						item.Background(k.chosen)
						fg, w = k.text, 600
					case item.Hovered():
						item.Background(k.hover)
					}
					item.Children(func() {
						ui.Icon(c, tabIcons[i]).FontSize(14).TextColor(map[bool]ui.Color{true: k.link, false: k.faint}[on])
						ui.Text(c, name).FontSize(fsBody).FontWeight(w).TextColor(fg).Grow(1).SingleLine()
					})
				}
			})
			ui.Scroll(c.Key(fmt.Sprint("tab", a.tab))).Grow(1).MinHeight(0).Background(k.sunk).Padding(sp4, sp4, sp5, sp4).Gap(sp5).Children(func() {
				switch a.tab {
				case tabGeneral:
					a.tabGeneral(c, k)
				case tabAgents:
					a.tabAgents(c, k)
				default:
					a.tabAbout(c, k)
				}
			})
		})
		a.hintBar(c, k, a.toast, "⌘1–3", "切换", "esc", "返回")
	})
}

var tabIcons = []*ui.SVG{iconGear, iconBot, iconInfo}

// numberField is a card row with a number: Enter, or 保存 once it
// changed, applies it.
func numberField(c *ui.Context, k ink, first bool, key, label, unit string, v *string, stored int, apply func()) {
	cardRow(c.Key(key), k, first).Children(func() {
		ui.Text(c, label).FontSize(fsBody).Grow(1)
		if strings.TrimSpace(*v) != fmt.Sprint(stored) {
			ui.Button(c.Key("ok"), "保存").FontSize(fsSmall).Padding(3, 10).OnClick(apply)
		}
		ui.TextInput(c.Key("in"), v).Label(label).Width(64).FontSize(fsBody).TextAlign(ui.End).FontFeatures("tnum").OnSubmit(apply)
		ui.Text(c, unit).FontSize(fsSmall).TextColor(k.muted).Width(28)
	})
}

func (a *app) tabGeneral(c *ui.Context, k ink) {
	group(c, k, "轮换提醒", fmt.Sprintf("每天检查一次：快到期或超期的密钥用%s提醒，%s图标旁显示数量。", notifyName(), barName()), func() {
		remind := a.settings.Remind
		option(c, k, true, "到期提醒", "", &remind, func() {
			a.settings.Remind = remind
			a.saveSettings(a.settings)
		})
		days := a.inv.RotateDays
		if days == 0 {
			days = vault.DefaultRotateDays
		}
		numberField(c, k, false, "days", "默认轮换周期", "天", &a.editDays, days, func() { a.applyNumber("days") })
		numberField(c, k, false, "remind", "提前提醒", "天", &a.editRemind, a.settings.RemindDays, func() { a.applyNumber("remind") })
	})
	group(c, k, "启动", "", func() {
		on := a.loginItem()
		option(c, k, true, "登录时启动", "在后台运行，才能提醒轮换、批准代理的请求。", &on, func() {
			if err := a.setLoginItem(on); err != nil {
				a.say("设置失败：" + err.Error())
			}
		})
		cardRow(c.Key("hotkey"), k, false).Children(func() {
			ui.Text(c, "唤起面板").FontSize(fsBody).Grow(1)
			ui.Row(c).Gap(3).Children(func() {
				for _, part := range shortcutParts(a.settings.Shortcut) {
					keycap(c.Key(part), k, part).FontSize(fsMeta).Padding(1, 6)
				}
			})
		})
	})
	group(c, k, "命令行", "终端里用 keys ls、keys run -- npm start 等。", func() {
		cardRow(c.Key("cli"), k, true).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, "keys 命令").FontSize(fsBody).Font(mono)
				ui.Text(c, "链接到本程序，与 App 共用钥匙串授权").FontSize(fsMeta + 0.5).TextColor(k.faint)
			})
			ui.Button(c.Key("install"), "安装…").FontSize(fsSmall).Padding(4, sp3).OnClick(func() {
				where, err := a.installCLI()
				if err != nil {
					a.say("安装失败：" + err.Error())
				} else {
					a.say("已安装：" + where)
				}
			})
		})
	})
}

// shortcutParts splits a shortcut into its keys, as the system spells
// them: ⌥ K on the Mac, Alt Shift K elsewhere.
func shortcutParts(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "+") {
		if macHints {
			p = strings.NewReplacer("CmdOrCtrl", "⌘", "Cmd", "⌘", "Ctrl", "⌃", "Alt", "⌥", "Shift", "⇧").Replace(p)
		}
		out = append(out, p)
	}
	return out
}

func notifyName() string {
	switch goos {
	case "darwin":
		return " macOS 通知"
	case "windows":
		return " Windows 通知"
	}
	return "桌面通知"
}

func barName() string {
	switch goos {
	case "darwin":
		return "菜单栏"
	case "windows":
		return "通知区域"
	}
	return "托盘"
}

func (a *app) tabAgents(c *ui.Context, k ink) {
	group(c, k, "批准", "代理（Claude Code、Cursor…）通过 MCP 申请用密钥时，这里弹出请求。按代理和密钥分别授权。", func() {
		numberField(c, k, true, "grant", "“限时允许”的时长", "分钟", &a.editGrant, a.settings.GrantMinutes, func() { a.applyNumber("grant") })
		cli := a.settings.CLIApproval
		option(c, k, false, "命令行非交互使用也要批准", "没有终端时（如代理在 shell 里运行 keys get / keys run），同样弹出请求。", &cli, func() {
			a.settings.CLIApproval = cli
			a.saveSettings(a.settings)
		})
	})
	gs := a.grants.List(a.now())
	group(c, k, fmt.Sprintf("当前授权 · %d", len(gs)), "", func() {
		if len(gs) == 0 {
			cardRow(c.Key("none"), k, true).Children(func() {
				ui.Text(c, "没有限时授权").FontSize(fsSmall).TextColor(k.faint)
			})
		}
		for i, g := range gs {
			cardRow(c.Key(i), k, i == 0).Children(func() {
				ui.Text(c, approval.ClientName(g.Client)).FontSize(fsBody).FontWeight(500).Shrink(0)
				ui.Text(c, g.Key).FontSize(fsSmall).Font(mono).TextColor(k.muted).Grow(1).Shrink(1).MinWidth(0).SingleLine()
				ui.Text(c, "到 "+g.Until.Local().Format("15:04")).FontSize(fsSmall).TextColor(k.faint).FontFeatures("tnum")
			})
		}
		if len(gs) > 0 {
			cardRow(c.Key("revoke"), k, false).Children(func() {
				ui.Spacer(c)
				ui.Button(c.Key("b"), "全部撤销").FontSize(fsSmall).Padding(3, 10).TextColor(k.danger).
					OnClick(func() { a.grants.Revoke(""); a.say("已撤销全部授权") })
			})
		}
	})
	group(c, k, "MCP 配置", "加到 Claude Code（~/.claude.json 或 claude mcp add）或 Cursor（~/.cursor/mcp.json）：", func() {
		ui.Text(c.Key("cfg"), a.mcpConfig("claude-code")).FontSize(fsMeta).Font(mono).Padding(sp3).Background(k.code).LineHeight(1.5).Selectable()
		cardRow(c.Key("copycfg"), k, false).Gap(sp2).Children(func() {
			ui.Spacer(c)
			ui.Button(c.Key("cc"), "复制 Claude Code 配置").FontSize(fsSmall).Padding(4, sp3).OnClick(func() { a.copy(a.mcpConfig("claude-code")); a.say("已复制") })
			ui.Button(c.Key("cur"), "复制 Cursor 配置").FontSize(fsSmall).Padding(4, sp3).OnClick(func() { a.copy(a.mcpConfig("cursor")); a.say("已复制") })
		})
	})
}

func (a *app) tabAbout(c *ui.Context, k ink) {
	ui.Row(c.Key("app")).Gap(sp3).AlignItems(ui.Center).Padding(0, 2).Children(func() {
		ui.Box(c).Size(40, 40).Radius(10).Center().Background(ui.Hex("#1c1d21")).Children(func() {
			ui.Icon(c, iconKey).FontSize(22).TextColor(ui.Hex("#e8a317"))
		})
		ui.Column(c).Gap(2).Grow(1).Shrink(1).Children(func() {
			ui.Text(c, "KeyBar").FontSize(fsTitle + 1).FontWeight(650)
			ui.Text(c, "版本 "+version+" · MIT").FontSize(fsSmall).TextColor(k.faint)
		})
	})
	group(c, k, "存储", "值只在"+storeName(goos)+"里；KeyBar 自己的文件里只有名字、平台、日期这些元数据和访问记录。", func() {
		field(c, k, true, "值", storeName(goos)+" · 服务 "+vault.Service)
		field(c, k, false, "数据目录", a.v.Dir)
		field(c, k, false, "源代码", "github.com/horizon0514/keybar")
	})
	group(c, k, "安全边界", "", func() {
		cardRow(c.Key("trust"), k, true).Children(func() {
			ui.Text(c, "批准机制挡住的是通过 MCP 和非交互命令行来的请求；以你的身份运行的任意程序仍可能直接读钥匙串（macOS 会按签名询问）。别给不信任的代码完整的 shell 权限。").
				FontSize(fsSmall).TextColor(k.muted).LineHeight(1.5).Grow(1).Shrink(1)
		})
	})
	ui.Row(c.Key("quit")).Children(func() {
		keyButton(c.Key("q"), k, "退出 KeyBar", "⌘Q", false, a.quit)
	})
}

func (a *app) cheatsheet(c *ui.Context, k ink) {
	type item struct{ keys, what string }
	groups := []struct {
		name  string
		items []item
	}{
		{"列表", []item{{"", "直接打字搜索名称、说明、平台、项目"}, {"↑ ↓", "选择"}, {"↵", "详情"}, {"⌘N", "添加密钥"}, {"⌘R", "轮换选中的密钥"},
			{"⌘C", "复制值（30 秒后清除剪贴板）"}, {"⌘Y", "访问记录"}}},
		{"详情", []item{{"⌘R", "轮换"}, {"⌘C", "复制值"}, {"⌘⌫", "删除"}, {"esc", "返回"}}},
		{"批准", []item{{"↵", "允许一次"}, {"⌘↵", fmt.Sprintf("允许 %d 分钟", a.settings.GrantMinutes)}, {"esc", "拒绝"}}},
		{"通用", []item{{strings.Join(shortcutParts(a.settings.Shortcut), " "), "唤起 / 隐藏面板"}, {"⌘,", "设置"}, {"", "拖动顶栏或底栏移动面板，双击复位"}, {"⌘Q", "退出"}}},
	}
	ui.Column(c).Fill().Children(func() {
		a.grip(ui.Row(c.Key("head"))).Height(52).Padding(0, sp4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "键盘快捷键").FontSize(fsTitle).FontWeight(650).Grow(1)
			ui.Text(c, "全键盘操作，鼠标可选").FontSize(fsMeta).TextColor(k.faint)
		})
		ui.Divider(c)
		sunkPage(c, k, "cheat", func() {
			for gi, g := range groups {
				group(c.Key(gi), k, g.name, "", func() {
					for i, it := range g.items {
						cardRow(c.Key(i), k, i == 0).Padding(7, sp3).MinHeight(0).Children(func() {
							ui.Text(c, it.what).FontSize(fsSmall + 0.5).Grow(1).Shrink(1).SingleLine()
							ui.Row(c).Gap(3).Children(func() {
								for _, kk := range strings.Fields(it.keys) {
									keycap(c.Key(kk), k, kk).FontSize(fsMeta).Padding(1, 6)
								}
							})
						})
					}
				})
			}
		})
		a.hintBar(c, k, "", "esc", "关闭")
	})
}

// grip makes a bar move the panel as it is dragged; a double click puts
// it back.
func (a *app) grip(el ui.Element) ui.Element {
	if a.winDrag == nil {
		return el
	}
	if _, _, ok := el.Dragged(); ok {
		if !a.moving {
			a.moving = true
			a.winDrag(dragStart)
		} else {
			a.winDrag(dragMove)
		}
	}
	if _, _, ok := el.DragEnded(); ok && a.moving {
		a.moving = false
		a.winDrag(dragEnd)
	}
	if el.DoubleClicked() {
		a.winDrag(dragReset)
	}
	return el
}

const (
	dragStart = iota
	dragMove
	dragEnd
	dragReset
)
