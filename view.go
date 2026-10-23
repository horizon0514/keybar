package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"keybar/agents"
	"keybar/approval"
	"keybar/vault"

	"github.com/egoist/mygo/ui"
)

// The panel: the list on the left, the chosen key on the right, as LB
// Bar's expanded panel; narrower than splitWidth, the list alone.
const (
	panelWidth, panelHeight = 880, 580
	minWidth, minHeight     = 480, 420
	leftWidth               = 340
	splitWidth              = 700
	pageWidth               = 640 // the forms' and pages' content, at most
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
	iconImport = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3v12M7 10l5 5 5-5"/><path d="M5 21h14"/></svg>`))
	iconFile   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><path d="M14 3v6h6"/></svg>`))
	iconX      = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18M6 6l12 12"/></svg>`))
)

// ink is the palette for the window's theme: LB Bar's.
type ink struct {
	bg, text, muted, faint, accent, link, line, hover, chosen, gold, green, red, card, danger ui.Color
}

// colors are opaque; every text color is WCAG AA (4.5:1) on bg and card
// (TestContrast). card is the opaque face of LB Bar's translucent one.
var colors = map[bool]map[string]string{
	false: {"bg": "#ffffff", "card": "#f4f5f7", "text": "#1f2328", "muted": "#57606a", "faint": "#626a73", "link": "#1a4fd6",
		"gold": "#8a5a00", "green": "#137a3a", "red": "#c62828", "danger": "#c62828", "line": "#d8dce1"},
	true: {"bg": "#1c1d21", "card": "#26272b", "text": "#e6e8eb", "muted": "#b0b5bd", "faint": "#8f96a0", "link": "#8ab4f8",
		"gold": "#f4c152", "green": "#5fd08a", "red": "#ff7b72", "danger": "#ff8a80", "line": "#3a3d44"},
}

func palette(c *ui.Context) ink {
	t := c.Theme()
	p := colors[t.Dark]
	h := func(n string) ui.Color { return ui.Hex(p[n]) }
	k := ink{bg: h("bg"), card: h("card"), text: h("text"), muted: h("muted"), faint: h("faint"), link: h("link"), accent: t.Accent,
		gold: h("gold"), green: h("green"), red: h("red"), danger: h("danger"), line: h("line")}
	if t.Dark {
		k.hover = ui.RGB(255, 255, 255).Alpha(0.05)
		// Softer selected-row background closer to system list gray
		k.chosen = ui.RGB(255, 255, 255).Alpha(0.09)
	} else {
		k.hover = ui.RGB(0, 0, 0).Alpha(0.04)
		// Muted gray selection in light mode, not a bright accent tint
		k.chosen = ui.RGB(0, 0, 0).Alpha(0.06)
	}
	return k
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
	// Use a plain content surface; the window chrome supplies its own frame.
	c.Root().TextColor(k.text).Background(k.bg)
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
	case pageForm:
		a.formView(c, k)
	case pageLog:
		a.logView(c, k)
	case pageSettings:
		a.settingsView(c, k)
	case pageImport:
		a.importView(c, k)
	case pageScan:
		a.scanView(c, k)
	default:
		a.mainView(c, k)
	}
}

// The list and the detail

func (a *app) keysList(c *ui.Context) {
	if a.auto.typing {
		a.auto.typing = false
		return
	}
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
		// Enter copies the value — the main path is search → list → copy.
		if k, ok := a.chosenKey(); ok {
			a.copyValue(k.Name)
		}
	case c.Shortcut(ui.Cmd, ui.KeyN):
		a.openAdd()
	case c.Shortcut(ui.Cmd, ui.KeyC):
		if k, ok := a.chosenKey(); ok {
			a.copyValue(k.Name)
		}
	case c.Shortcut(ui.Cmd, ui.KeyF):
		a.input.Focus()
	}
}

// detailKeys are the keys of the detail, once ↵ opened it.
func (a *app) detailKeys(c *ui.Context, key vault.Key) {
	if a.confirm || a.typing {
		a.typing = false
		return
	}
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

// mainView is the header, the list and the detail side by side (or the
// one in focus, when narrow), and the hints.
func (a *app) mainView(c *ui.Context, k ink) {
	key, open := a.key(a.detail)
	open = open && a.page == pageDetail
	if a.page == pageDetail && !open {
		a.page = pageList
	}
	if open {
		a.detailKeys(c, key)
	} else {
		a.keysList(c)
		key, open = a.chosenKey()
	}
	w, _ := c.Size()
	wide := w == 0 || w >= splitWidth
	ui.Column(c).Fill().Children(func() {
		a.header(c, k)
		// No filter tabs on the main list; simplify path to search → list → copy.
		ui.Divider(c)
		a.autoNotices(c, k)
		ui.Row(c.Key("panes")).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			if wide || a.page == pageList {
				col := ui.Column(c.Key("left")).MinHeight(0)
				if wide {
					col.Width(leftWidth)
				} else {
					col.Grow(1)
				}
				col.Children(func() { a.listPane(c, k) })
			}
			if wide {
				ui.Box(c.Key("split")).Width(1).Background(k.line)
			}
			if wide || a.page == pageDetail {
				ui.Column(c.Key("right")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
					if open {
						a.detailPane(c.Key(key.Name), k, key)
					} else {
						notice(c, k, "选一个密钥，看它的详情。", false)
					}
				})
			}
		})
		ui.Divider(c)
		left := fmt.Sprintf("%d 个 API key · 值在 %s", len(a.inv.Keys), storeShort())
		if a.demo {
			left = "示例数据 · " + left
		}
		if a.invErr != "" {
			left = a.invErr
		}
		if a.toast != "" {
			left = a.toast
		}
		// Footer keeps only a quiet factual line; no persistent shortcut strip
		a.hintBar(c, k, left, "")
	})
	if open && a.page == pageDetail {
		msg := "钥匙串里的值也会删除。"
		if len(key.Platforms) > 0 {
			msg = fmt.Sprintf("钥匙串里的值也会删除；它还存在于 %d 个平台，删除后不再提醒你轮换它们。", len(key.Platforms))
		}
		if ui.AlertDialog(c, &a.confirm, "删除 "+key.Name+"？", msg, "取消", "删除") == 1 {
			a.removeKey(key.Name)
		}
	}
}

func storeShort() string {
	s, _, _ := strings.Cut(storeName(goos), "（")
	return s
}

// header is the search, what needs doing, and the buttons.
func (a *app) header(c *ui.Context, k ink) {
	a.grip(ui.Row(c.Key("header"))).Padding(8, 12, 6, 14).Gap(8).AlignItems(ui.Center).Children(func() {
		// Slim, inline search — closer to a native toolbar search field
		ui.Icon(c, iconSearch).FontSize(14).TextColor(k.muted)
		in := ui.TextInputBase(c.Key("q"), &a.query).Bind(&a.input).Grow(1).FontSize(13).
			Placeholder("搜索密钥、平台、项目").Label("搜索")
		if in.Changed() {
			a.chosen = 0
			if a.page == pageDetail { // typing searches again
				a.page = pageList
			}
		}
		if in.Submitted() {
			if key, ok := a.chosenKey(); ok {
				a.copyValue(key.Name)
			}
		}
		if a.focusInput {
			a.focusInput = false
			in.AutoFocus()
			a.input.Focus()
		}
		if a.demo {
			badge(c, "示例数据", k.gold)
		}
		over, soon, pend := a.counts()
		chip := func(n int, label string, col ui.Color, click func()) {
			if n == 0 {
				return
			}
			row := ui.Row(c.Key(label)).Gap(4).AlignItems(ui.Center)
			if click != nil {
				row.Cursor(ui.CursorPointer).Label(label).OnClick(click)
			}
			row.Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(col)
				ui.Text(c, fmt.Sprintf("%d %s", n, label)).FontSize(11).TextColor(k.muted).SingleLine()
			})
		}
		chip(over, "超期", k.danger, a.openDueRotate)
		chip(soon, "快到期", k.gold, a.openDueRotate)
		chip(pend, "待同步", k.gold, func() { a.setFilter(filterDue) })
		iconButton(c.Key("add"), k, iconPlus, "添加密钥  "+hk("⌘N"), a.openAdd)
		iconButton(c.Key("gear"), k, iconGear, "设置  "+hk("⌘,"), a.openSettings)
	})
}

// tabs are the list's filters, as LB Bar's watchlist groups.
func (a *app) tabs(c *ui.Context, k ink) {
	a.grip(ui.Row(c.Key("tabs"))).Padding(0, 14, 8, 16).Gap(4).AlignItems(ui.Center).Children(func() {
		for i, n := range filterNames {
			on := i == a.filter
			bg, fg, w := ui.Transparent, k.muted, 450
			if on {
				bg, fg, w = k.chosen, k.link, 600
			}
			ui.Box(c.Key(n)).Padding(4, 10).Radius(7).Background(bg).Cursor(ui.CursorPointer).
				Label(n).Tooltip(hk(fmt.Sprintf("⌘%d", i+1))).OnClick(func() { a.setFilter(i) }).Children(func() {
				ui.Text(c, n).FontSize(12).FontWeight(w).TextColor(fg).SingleLine()
			})
		}
		ui.Spacer(c)
	})
}

func iconButton(c *ui.Context, k ink, icon *ui.SVG, tip string, fn func()) {
	label, _, _ := strings.Cut(tip, "  ")
	ui.Box(c).Padding(4).Radius(6).Cursor(ui.CursorPointer).Tooltip(tip).Label(label).OnClick(fn).Children(func() {
		ui.Icon(c, icon).FontSize(15).TextColor(k.faint)
	})
}

func (a *app) listPane(c *ui.Context, k ink) {
	rows := a.rows()
	a.list.Key = func(i int) any {
		if i < len(rows) {
			return rows[i].Name
		}
		return i
	}
	ui.List(c.Key("list"), &a.list, len(rows), func(i int) {
		a.row(c.Key(rows[i].Name), k, i, rows[i])
	}).Grow(1).MinHeight(0).Padding(6, 8).Gap(2).Label("密钥列表").Children(func() {
		if len(rows) == 0 {
			notice(c, k, hk("⌘N")+" 加一把钥匙", false)
		}
	})
}

// row is a key: its name and a line of what it is, and its state.
func (a *app) row(c *ui.Context, k ink, i int, key vault.Key) {
	now := a.now()
	st := a.inv.StatusOf(key, now)
	row := ui.Row(c).Padding(7, 10).Radius(9).Gap(10).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	bg := ui.Transparent
	switch {
	case i == a.chosen && a.page == pageDetail:
		bg = k.hover
	case i == a.chosen:
		bg = k.chosen
	case row.Hovered():
		bg = k.hover
	}
	// Click copies the secret; Enter/Cmd+C also copy.
	row.Background(bg).OnClick(func() { a.choose(i); a.copyValue(key.Name) })
	row.Children(func() {
		// Minimal row: status dot + key name only.
		ui.Box(c).Size(6, 6).Radius(3).Background(k.state(st.State)).Shrink(0)
		ui.Text(c, key.Name).FontSize(13.5).FontWeight(550).SingleLine().Shrink(1)
	})
}

// stateLine is a colored dot and the state in words.
func stateLine(c *ui.Context, k ink, text string, col ui.Color) {
	ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(col)
		ui.Text(c, text).FontSize(11).FontWeight(550).TextColor(col).SingleLine()
	})
}

// kinds are the short names of where a key also lives, as LB Bar's
// market tags: GH, Vercel…
func kinds(key vault.Key) []string {
	short := map[string]string{"github": "GH", "vercel": "Vercel", "railway": "Railway", "env": ".env", "tidb": "TiDB"}
	var out []string
	seen := map[string]bool{}
	for _, p := range key.Platforms {
		n := short[p.Kind]
		if n == "" {
			n = p.Kind
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(out) > 3 {
		out = append(out[:2], fmt.Sprintf("+%d", len(out)-2))
	}
	return out
}

// chipTag is a tiny tag, as LB Bar's HK / US.
func chipTag(c *ui.Context, k ink, text string) {
	ui.Text(c, text).FontSize(9).FontWeight(600).TextColor(k.faint).Padding(0, 4).Radius(3).Background(k.line.Alpha(0.5)).SingleLine().Shrink(0)
}

func badge(c *ui.Context, text string, col ui.Color) {
	ui.Text(c, text).FontSize(10).FontWeight(600).TextColor(col).Padding(1, 6).Radius(5).Background(col.Alpha(0.13)).SingleLine()
}

func notice(c *ui.Context, k ink, msg string, bad bool) {
	col := k.muted
	if bad {
		col = k.danger
	}
	ui.Row(c).Gap(8).Padding(24, 16).AlignItems(ui.Start).Children(func() {
		ui.Icon(c, iconAlert).FontSize(15).TextColor(col)
		ui.Text(c, msg).FontSize(13).TextColor(col).Grow(1).LineHeight(1.45)
	})
}

func (a *app) hintBar(c *ui.Context, k ink, left, right string) {
	a.grip(ui.Row(c.Key("hints"))).Padding(8, 16).Gap(14).AlignItems(ui.Center).FontSize(11).TextColor(k.faint).Children(func() {
		ui.Text(c, left).Grow(1).Shrink(1).MinWidth(0).SingleLine()
		ui.Text(c, right).SingleLine()
	})
}

// smallButton is LB Bar's outline button: "☆ 置顶到菜单  ⌘D".
func smallButton(c *ui.Context, k ink, label, key string, col ui.Color, fn func()) {
	text := label
	if key != "" {
		text += "  " + hk(key)
	}
	b := ui.Box(c).Padding(2, 8).Radius(6).BorderWidth(1).BorderColor(k.line).Cursor(ui.CursorPointer).
		Label(label).OnClick(fn)
	if key != "" {
		b.Tooltip(hk(key))
	}
	if b.Hovered() {
		b.Background(k.hover)
	}
	b.Children(func() { ui.Text(c, text).FontSize(11).TextColor(col).SingleLine() })
}

// detailPane is a key: its name and state, its dates, where else it
// lives, and its history.
func (a *app) detailPane(c *ui.Context, k ink, key vault.Key) {
	now := a.now()
	st := a.inv.StatusOf(key, now)
	open := a.page == pageDetail
	ui.Scroll(c.Key("detail")).Label("详情").Grow(1).MinHeight(0).MinWidth(0).Padding(14, 20, 18, 20).Gap(16).Children(func() {
		ui.Column(c.Key("head")).Gap(6).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, key.Name).FontSize(15).FontWeight(650).Font("monospace").SingleLine().Shrink(1)
				text, col := st.State.Zh(), k.state(st.State)
				if st.Why != "" {
					text = st.Why
				}
				stateLine(c, k, text, col)
				ui.Spacer(c)
				smallButton(c.Key("copy"), k, "复制值", "⌘C", k.muted, func() { a.copyValue(key.Name) })
				rotLabel := "轮换"
				if st.State == vault.Overdue || st.State == vault.DueSoon || st.Pending > 0 {
					rotLabel = "自动轮换"
				}
				smallButton(c.Key("rotate"), k, rotLabel, "⌘R", k.link, func() { a.openRotate(key.Name) })
			})
			if key.Description != "" {
				ui.Text(c, key.Description).FontSize(13).TextColor(k.muted).LineHeight(1.4)
			}
		})
		a.factsGrid(c.Key("facts"), k, key, st)
		if open {
			a.metaEditBlock(c.Key("meta"), k)
		}
		a.platformsBlock(c.Key("plats"), k, key, open)
		// Calmer detail by default: advanced sections behind a disclosure
		if !a.showDetailMore {
			linkButton(c.Key("more"), k, "更多详情", k.muted, func() { a.showDetailMore = true })
		} else {
			allow := key.Agent != "deny"
			ui.Row(c.Key("agent")).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
					ui.Text(c, "允许 AI 代理申请使用").FontSize(13)
					ui.Text(c, "通过 MCP 或命令行用它时，每次都要你批准；关掉后一律拒绝。").FontSize(11).TextColor(k.faint)
				})
				if ui.Switch(c.Key("sw"), &allow).Label("允许 AI 代理申请使用").Changed() {
					a.setAgent(key.Name, allow)
				}
			})
			if len(key.History) > 0 {
				block(c, k, "轮换历史", "", func() {
					for i := len(key.History) - 1; i >= 0 && i >= len(key.History)-6; i-- {
						h := key.History[i]
						ui.Row(c.Key(i)).Gap(10).Padding(2, 0).Children(func() {
							ui.Text(c, h.At.Local().Format("2006-01-02 15:04")).FontSize(12).TextColor(k.muted).Width(116).Shrink(0)
							ui.Text(c, h.Note).FontSize(12).Grow(1).Shrink(1).MinWidth(0).SingleLine()
							if by := map[string]string{"cli": "命令行", "app": "KeyBar"}[h.By]; by != "" {
								ui.Text(c, by).FontSize(11).TextColor(k.faint)
							}
						})
					}
				})
			}
			if evs := a.accessFor(key.Name, 5); len(evs) > 0 {
				block(c, k, "最近访问", "", func() {
					for i, ev := range evs {
						a.eventRow(c.Key(i), k, ev, false)
					}
				})
			}
			linkButton(c.Key("less"), k, "收起", k.muted, func() { a.showDetailMore = false })
		}
		if open {
			ui.Row(c.Key("danger")).Children(func() {
				smallButton(c.Key("rm"), k, "删除密钥…", "⌘⌫", k.danger, func() { a.confirm = true })
			})
		} else {
			ui.Text(c.Key("tip"), hk("↵ 打开详情：勾选平台、添加平台、删除")).FontSize(11).TextColor(k.faint)
		}
	})
}

// factsGrid is the key's dates and labels, two columns — plain rows, no card.
func (a *app) factsGrid(c *ui.Context, k ink, key vault.Key, st vault.Status) {
	now := a.now()
	next := "不提醒"
	if !st.Due.IsZero() {
		next = day(st.Due)
	}
	every := "不提醒"
	if d := a.inv.RotateEvery(key); d > 0 {
		every = fmt.Sprintf("%d 天", int(d.Hours()/24))
	}
	exp := "—"
	if !key.Expires.IsZero() {
		exp = day(key.Expires)
	}
	orDash := func(ss []string) string {
		if len(ss) == 0 {
			return "—"
		}
		return strings.Join(ss, "、")
	}
	agent := "要批准"
	if key.Agent == "deny" {
		agent = "禁止"
	}
	cells := [][2]string{
		{"上次轮换", vault.Ago(key.LastRotated, now)}, {"下次轮换", next},
		{"轮换周期", every}, {"过期", exp},
		{"使用者", orDash(key.Projects)}, {"标签", orDash(key.Tags)},
		{"创建", day(key.Created)}, {"AI 代理", agent},
	}
	ui.Column(c).Gap(2).Children(func() {
		for i := 0; i < len(cells); i += 2 {
			ui.Row(c.Key(i)).Padding(3, 0).Gap(16).Children(func() {
				for _, cell := range cells[i : i+2] {
					ui.Row(c.Key(cell[0])).Grow(1).Basis(0).Gap(8).Children(func() {
						ui.Text(c, cell[0]).FontSize(12).TextColor(k.muted).Grow(1).SingleLine()
						col := k.text
						if cell[0] == "下次轮换" && st.State != vault.OK {
							col = k.state(st.State)
						}
						ui.Text(c, cell[1]).FontSize(12).FontWeight(550).TextColor(col).SingleLine().Shrink(1).MinWidth(0)
					})
				}
			})
		}
	})
}

// metaEditBlock lets the open detail change description / rotate / expiry /
// projects / tags — the fields the add form no longer asks.
func (a *app) metaEditBlock(c *ui.Context, k ink) {
	m := &a.meta
	if m.name == "" {
		return
	}
	block(c, k, "更多", "说明、轮换周期、过期、使用者和标签", func() {
		fieldIn := func(key, label, ph string, v *string) {
			ui.Column(c.Key(key)).Gap(4).Margin(0, 0, 6, 0).Children(func() {
				ui.Text(c, label).FontSize(12).TextColor(k.muted)
				in := ui.TextInput(c.Key("in"), v).Label(label).Placeholder(ph)
				if in.Focused() {
					a.typing = true
				}
				if in.Submitted() {
					a.saveMeta()
				}
			})
		}
		fieldIn("desc", "说明", "例如：OpenAI 个人账号", &m.desc)
		ui.Row(c.Key("nums")).Gap(12).Children(func() {
			ui.Column(c.Key("d")).Grow(1).Basis(0).Children(func() {
				fieldIn("days", "轮换周期（天）", fmt.Sprintf("默认 %d，-1 不提醒", vault.DefaultRotateDays), &m.days)
			})
			ui.Column(c.Key("e")).Grow(1).Basis(0).Children(func() {
				fieldIn("exp", "过期日", "2027-01-19", &m.expires)
			})
		})
		ui.Row(c.Key("who")).Gap(12).Children(func() {
			ui.Column(c.Key("p")).Grow(1).Basis(0).Children(func() {
				fieldIn("projects", "使用者", "rz-system, lb-bar", &m.projects)
			})
			ui.Column(c.Key("t")).Grow(1).Basis(0).Children(func() {
				fieldIn("tags", "标签", "prod, ai", &m.tags)
			})
		})
		if m.err != "" {
			ui.Text(c.Key("err"), m.err).FontSize(12).TextColor(k.danger)
		}
		ui.Row(c.Key("save")).Children(func() {
			linkButton(c.Key("btn"), k, "保存元数据", k.link, a.saveMeta)
		})
	})
}

// block is a titled part of a page: a 13 semibold title, a muted note on
// its right.
func block(c *ui.Context, k ink, title, note string, body func()) {
	ui.Column(c.Key(title)).Gap(4).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(0, 0, 2, 0).Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(600).Grow(1)
			if note != "" {
				ui.Text(c, note).FontSize(11).TextColor(k.faint).SingleLine().Shrink(1)
			}
		})
		body()
	})
}

// platformsBlock is the checklist of where else the key lives.
func (a *app) platformsBlock(c *ui.Context, k ink, key vault.Key, open bool) {
	note := "轮换后逐个更新并勾选"
	if len(key.Platforms) == 0 {
		note = "记下它还存在哪里，轮换时就有清单"
	}
	block(c, k, fmt.Sprintf("也存在于 %d", len(key.Platforms)), note, func() {
		for _, p := range key.Platforms {
			done := !p.Stale(key)
			id := key.Name + "/" + p.ID
			row := ui.Row(c.Key(p.ID)).Gap(10).Padding(5, 8).Margin(0, -8).Radius(7).AlignItems(ui.Center)
			if row.Hovered() {
				row.Background(k.hover)
			}
			row.Children(func() {
				if ui.Checkbox(c.Key("cb"), &done, "").Label("已更新 " + p.Label()).Changed() {
					a.togglePlatform(key.Name, p, done)
				}
				ui.Text(c, p.Label()).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
				var sub []string
				if v := p.VarName(key.Name); v != key.Name {
					sub = append(sub, v)
				}
				if len(p.Envs) > 0 {
					sub = append(sub, strings.Join(p.Envs, ","))
				}
				if len(sub) > 0 {
					ui.Text(c, strings.Join(sub, " · ")).FontSize(11).TextColor(k.faint).SingleLine().Shrink(1).MinWidth(0)
				}
				ui.Spacer(c)
				col, status := k.faint, ""
				switch m := a.syncMsg[id]; {
				case m != "":
					status = m
					if strings.HasPrefix(m, "✗") {
						col = k.danger
					}
				case !done:
					status, col = "还是旧值", k.gold
				case !p.Updated.IsZero():
					status = "更新于 " + day(p.Updated)
				}
				ui.Text(c, status).FontSize(11).TextColor(col).SingleLine().Shrink(1).MinWidth(0)
				if p.Auto() {
					if a.syncing[id] {
						ui.Spinner(c).Size(12, 12)
					} else {
						smallButton(c.Key("sync"), k, "自动更新", "", k.link, func() { a.syncPlatform(key.Name, p, nil) })
					}
				}
				if open {
					ui.Box(c.Key("rm")).Padding(2).Radius(5).Cursor(ui.CursorPointer).Tooltip("从清单移除").Label("移除 " + p.Label()).
						OnClick(func() { a.removePlatform(key.Name, p.ID) }).Children(func() {
						ui.Icon(c, iconX).FontSize(11).TextColor(k.faint)
					})
				}
			})
		}
		if open {
			ui.Row(c.Key("addplat")).Gap(8).Padding(5, 0).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconPlus).FontSize(13).TextColor(k.faint)
				in := ui.TextInputBase(c.Key("plat"), &a.addPlat).Label("添加平台").FontSize(12).
					Placeholder("添加平台：github:owner/repo、vercel:my-app、railway:api").Grow(1)
				if in.Submitted() {
					a.addPlatform(key.Name)
				}
				if in.Focused() {
					a.typing = true
				}
				if strings.TrimSpace(a.addPlat) != "" {
					smallButton(c.Key("addbtn"), k, "添加", "↵", k.link, func() { a.addPlatform(key.Name) })
				}
			})
		}
	})
}

func day(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format(time.DateOnly)
}

// Pages: a title bar with 返回, content at most pageWidth wide, hints.

// head is a page's title bar, with 返回.
func (a *app) head(c *ui.Context, k ink, title string, right func()) {
	a.grip(ui.Row(c.Key("head"))).Padding(12, 16, 12, 12).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Row(c.Key("back")).Gap(2).Padding(4, 8, 4, 4).Radius(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).
			Label("返回").OnClick(a.back).Children(func() {
			ui.Icon(c, iconBack).FontSize(16).TextColor(k.muted)
			ui.Text(c, "返回").FontSize(13).TextColor(k.muted)
		})
		ui.Text(c, title).FontSize(15).FontWeight(650).Grow(1).Shrink(1).SingleLine()
		if right != nil {
			right()
		}
	})
	ui.Divider(c)
}

// pageBody is a page's scrolling content, at most pageWidth wide.
func pageBody(c *ui.Context, key string, gap float32, body func()) {
	ui.Scroll(c.Key(key)).Grow(1).MinHeight(0).Children(func() {
		ui.Row(c.Key("center")).Shrink(0).Children(func() {
			ui.Spacer(c)
			col := ui.Column(c.Key("content")).Padding(18, 24, 24, 24).Gap(gap)
			if w, _ := c.Size(); w > pageWidth {
				col.Width(pageWidth).Shrink(0)
			} else {
				col.Grow(1)
			}
			col.Children(body)
			ui.Spacer(c)
		})
	})
}

// section is a titled group of settings.
func section(c *ui.Context, k ink, title, desc string, body func()) {
	ui.Column(c.Key(title)).Gap(8).Children(func() {
		ui.Text(c, title).FontSize(13).FontWeight(600)
		if desc != "" {
			ui.Text(c, desc).FontSize(12).TextColor(k.muted).LineHeight(1.45)
		}
		body()
	})
}

// option is a row with a switch on its right.
func option(c *ui.Context, k ink, label, desc string, on *bool, changed func()) {
	ui.Row(c.Key(label)).Gap(12).Padding(4, 0).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
			ui.Text(c, label).FontSize(13)
			if desc != "" {
				ui.Text(c, desc).FontSize(11).TextColor(k.muted).LineHeight(1.4)
			}
		})
		if ui.Switch(c.Key("sw"), on).Label(label).Changed() {
			changed()
		}
	})
}

func field(c *ui.Context, k ink, label, value string) {
	ui.Row(c.Key(label)).Gap(12).Padding(3, 0).AlignItems(ui.Baseline).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(k.muted).Width(64).Shrink(0)
		ui.Text(c, value).FontSize(13).Grow(1).Shrink(1).MinWidth(0).LineHeight(1.4).Selectable()
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
	title := "添加密钥"
	if f.rotate {
		title = "轮换 " + f.name
	}
	ui.Column(c).Fill().Children(func() {
		a.head(c, k, title, nil)
		input := func(key, label, ph string, v *string, password, focus bool) {
			ui.Column(c.Key(key)).Gap(5).Children(func() {
				ui.Text(c, label).FontSize(12).TextColor(k.muted)
				in := ui.TextInput(c.Key("in"), v).Label(label).Placeholder(ph).OnSubmit(a.submit)
				if password {
					in.Password()
				}
				if focus {
					in.AutoFocus()
				}
			})
		}
		pageBody(c, "form", 14, func() {
			if f.rotate {
				key, _ := a.key(f.name)
				ui.Text(c, "在服务商那里生成新密钥，粘贴到这里。保存后钥匙串更新；GitHub Actions / Vercel / 本地 .env 会自动推送，其它平台进清单。").FontSize(12).TextColor(k.muted).LineHeight(1.45)
				input("v1", "新值", "粘贴新密钥", &f.value, true, true)
				input("v2", "再输入一次", "", &f.value2, true, false)
				input("note", "备注（可选）", "例如：泄露通报后轮换", &f.note, false, false)
				if len(key.Platforms) > 0 {
					block(c, k, "保存后", "", func() {
						for _, p := range key.Platforms {
							ui.Row(c.Key(p.ID)).Gap(8).Padding(3, 0).AlignItems(ui.Center).Children(func() {
								ui.Text(c, p.Label()).FontSize(13).Grow(1).Shrink(1).SingleLine()
								if p.Auto() {
									stateLine(c, k, "自动更新", k.green)
								} else {
									stateLine(c, k, "手动更新，之后勾选", k.gold)
								}
							})
						}
					})
				}
			} else {
				ui.Text(c, "名称和值就够了。平台、轮换周期这些，以后在详情里补。").FontSize(12).TextColor(k.muted).LineHeight(1.45)
				input("name", "名称", "OPENAI_API_KEY", &f.name, false, true)
				input("value", "值", "只存进"+storeShort(), &f.value, true, false)
				input("desc", "说明（可选）", "例如：OpenAI 个人账号", &f.desc, false, false)
			}
			if f.err != "" {
				ui.Text(c.Key("err"), f.err).FontSize(12).TextColor(k.danger).LineHeight(1.4)
			}
			ui.Row(c.Key("btns")).Gap(8).Margin(4, 0, 0, 0).Children(func() {
				ui.Spacer(c)
				ui.Button(c.Key("cancel"), "取消").OnClick(a.back)
				ui.PrimaryButton(c.Key("save"), "保存  "+hk("⌘↵")).OnClick(a.submit)
			})
		})
		ui.Divider(c)
		a.hintBar(c, k, "值不会写进任何文件或日志", hint("⇥", "下一项", "⌘↵", "保存", "esc", "取消"))
	})
}

// The access log

func (a *app) logView(c *ui.Context, k ink) {
	if c.Shortcut(0, ui.KeyEscape) {
		a.back()
		return
	}
	evs := vault.ReadLog(a.v.Dir, 300, nil)
	ui.Column(c).Fill().Children(func() {
		a.head(c, k, "访问记录", func() { ui.Text(c, fmt.Sprintf("最近 %d 条", len(evs))).FontSize(11).TextColor(k.faint) })
		ui.Scroll(c.Key("log")).Grow(1).MinHeight(0).Padding(10, 18).Gap(0).Children(func() {
			if len(evs) == 0 {
				notice(c, k, "还没有访问记录。程序、命令行和 AI 代理每次用密钥都会记在这里（只记名字，不记值）。", false)
			}
			for i := len(evs) - 1; i >= 0; i-- {
				a.eventRow(c.Key(i), k, evs[i], true)
			}
		})
		ui.Divider(c)
		a.hintBar(c, k, "记录在 audit.jsonl，只有名字和结果", hint("esc", "返回"))
	})
}

var actionNames = map[string]string{"get": "读取", "reveal": "读明文", "run": "运行", "copy": "复制", "rotate": "轮换", "add": "添加",
	"rm": "删除", "sync": "同步", "import": "导入", "list": "列出", "connect": "连接代理", "disconnect": "断开代理",
	"scan": "扫描本机", "clipboard": "从剪贴板保存", "clean": "移除明文"}

func (a *app) eventRow(c *ui.Context, k ink, ev vault.Event, keys bool) {
	col := k.muted
	switch {
	case ev.Decision == "denied", ev.Decision == "timeout", ev.Decision == "error":
		col = k.danger
	case strings.HasPrefix(ev.Decision, "allowed"):
		col = k.green
	}
	ui.Row(c).Gap(10).Padding(5, 0).AlignItems(ui.Center).Children(func() {
		ui.Text(c, ev.Time.Local().Format("01-02 15:04")).FontSize(11).TextColor(k.faint).Width(72).Shrink(0).SingleLine()
		ui.Text(c, approval.ClientName(ev.Client)).FontSize(12).Width(88).Shrink(0).SingleLine()
		act := actionNames[ev.Action]
		if act == "" {
			act = ev.Action
		}
		what := act
		if keys && len(ev.Keys) > 0 {
			what += " " + strings.Join(ev.Keys, "、")
		}
		if ev.Detail != "" {
			what += " · " + ev.Detail
		}
		ui.Text(c, what).FontSize(12).Grow(1).Shrink(1).MinWidth(0).SingleLine()
		stateLine(c, k, vault.DecisionZh(ev.Decision), col)
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
	ui.Column(c).Fill().Children(func() {
		a.grip(ui.Row(c.Key("head"))).Padding(14, 20).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconShield).FontSize(16).TextColor(k.gold)
			ui.Text(c, "需要你批准").FontSize(15).FontWeight(650).Grow(1)
			if n := len(a.pending); n > 1 {
				badge(c, fmt.Sprintf("还有 %d 个", n-1), k.gold)
			}
			ui.Text(c, fmt.Sprintf("%d 秒后自动拒绝", max(0, int(left.Seconds())))).FontSize(11).TextColor(k.faint)
		})
		ui.Divider(c)
		pageBody(c, "req", 14, func() {
			who := approval.ClientName(r.Client)
			what := "要用这些密钥运行一个命令"
			switch {
			case r.Action == "reveal":
				what = "要读取密钥的明文值"
			case len(r.Keys) == 0:
				what = "要运行一个命令，项目要的密钥 KeyBar 里还没有"
			}
			ui.Text(c, who+approval.Sep(who)+what).FontSize(17).FontWeight(650).LineHeight(1.35)
			if len(r.Keys) > 0 {
				ui.Row(c.Key("keys")).Gap(6).Wrap().Children(func() {
					for _, name := range r.Keys {
						ui.Text(c.Key(name), name).FontSize(12).FontWeight(600).Font("monospace").Padding(2, 6).Radius(5).Background(k.line.Alpha(0.5))
					}
				})
			}
			if r.Command != "" {
				ui.Text(c.Key("cmd"), r.Command).FontSize(12).Font("monospace").Padding(8, 10).Radius(8).Background(k.card).LineHeight(1.45).MaxLines(6).Selectable()
			}
			a.fillRows(c.Key("fill"), k, p)
			ui.Column(c.Key("facts")).Gap(0).Children(func() {
				field(c, k, "来自", r.Client)
				if r.Cwd != "" {
					field(c, k, "目录", r.Cwd)
				}
				if r.Reason != "" {
					field(c, k, "理由", r.Reason)
				}
			})
			note := "值只注入这个命令的进程，输出里的值会被替换成 [REDACTED]，代理看不到明文。"
			col := k.muted
			switch {
			case r.Action == "reveal":
				note, col = "批准后代理会拿到明文值。除非确有必要，建议拒绝，让它改用 run_with_secrets。", k.gold
			case r.Plain:
				note, col = "值只注入这个命令的进程，但输出不脱敏：命令打印出的值，调用它的程序都能看到。让它加上 --redact 更安全。", k.gold
			}
			ui.Row(c.Key("note")).Gap(6).AlignItems(ui.Start).Children(func() {
				if col == k.gold {
					ui.Icon(c, iconAlert).FontSize(13).TextColor(col).Margin(2, 0, 0, 0)
				}
				ui.Text(c, note).FontSize(12).TextColor(col).LineHeight(1.45).Grow(1).Shrink(1)
			})
			ui.Row(c.Key("btns")).Gap(8).Margin(6, 0, 0, 0).Children(func() {
				if len(r.Keys) == 0 {
					ui.Button(c.Key("deny"), "跳过  esc").OnClick(func() { a.decide(false, false) })
					ui.Spacer(c)
					ui.PrimaryButton(c.Key("once"), "保存并运行  "+hk("↵")).Disabled(p.filled() == 0).OnClick(func() { a.decide(true, false) })
					return
				}
				once := "允许一次"
				if p.filled() > 0 {
					once = fmt.Sprintf("保存 %d 个并允许一次", p.filled())
				}
				ui.Button(c.Key("deny"), "拒绝  esc").OnClick(func() { a.decide(false, false) })
				ui.Spacer(c)
				ui.Button(c.Key("timed"), fmt.Sprintf("允许 %d 分钟  %s", a.settings.GrantMinutes, hk("⌘↵"))).
					Tooltip("这段时间内这个代理再用这些密钥不再询问").OnClick(func() { a.decide(true, true) })
				ui.PrimaryButton(c.Key("once"), once+"  "+hk("↵")).OnClick(func() { a.decide(true, false) })
			})
		})
		ui.Divider(c)
		a.hintBar(c, k, "每次决定都记在访问记录里", hint("↵", "允许一次", "⌘↵", fmt.Sprintf("允许 %d 分钟", a.settings.GrantMinutes), "esc", "拒绝"))
	})
}

// The settings: one page, three tabs, as LB Bar's.

func (a *app) settingsView(c *ui.Context, k ink) {
	if c.Shortcut(0, ui.KeyEscape) {
		a.back()
		return
	}
	for i, key := range []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4} {
		if c.Shortcut(ui.Cmd, key) {
			a.openTab(i)
		}
	}
	if a.tab == tabAgents && c.Shortcut(ui.Cmd, ui.KeyEnter) {
		a.connectAll()
	}
	ui.Column(c).Fill().Children(func() {
		a.head(c, k, "设置", func() { ui.Text(c, "KeyBar "+version).FontSize(11).TextColor(k.faint) })
		ui.Row(c.Key("body")).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c.Key("tabs")).Width(132).Padding(10, 8).Gap(2).Background(k.card).Children(func() {
				for i, name := range tabNames {
					on := i == a.tab
					bg, fg, w := ui.Transparent, k.text, 450
					if on {
						bg, fg, w = k.chosen, k.link, 600
					}
					ui.Row(c.Key(name)).Padding(7, 10).Radius(7).Background(bg).AlignItems(ui.Center).Cursor(ui.CursorPointer).
						Focusable().Label(name).OnClick(func() { a.openTab(i) }).Children(func() {
						ui.Text(c, name).FontSize(13).FontWeight(w).TextColor(fg).Grow(1).SingleLine()
						ui.Text(c, hk(fmt.Sprintf("⌘%d", i+1))).FontSize(10).TextColor(k.faint)
					})
				}
			})
			ui.Divider(c)
			ui.Scroll(c.Key(fmt.Sprint("tab", a.tab))).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c.Key("content")).Shrink(0).MaxWidth(pageWidth).Padding(18, 24, 24, 24).Gap(20).Children(func() {
					switch a.tab {
					case tabGeneral:
						a.tabGeneral(c, k)
					case tabAgents:
						a.tabAgents(c, k)
					case tabAuto:
						a.tabAuto(c, k)
					default:
						a.tabAbout(c, k)
					}
				})
			})
		})
		ui.Divider(c)
		hints := hint("⌘1–4", "切换", "⇥", "下一项", "↵", "确定", "esc", "返回")
		if a.tab == tabAgents && a.toConnect() > 0 {
			hints = hint("⌘↵", "全部连接", "⌘1–4", "切换", "esc", "返回")
		}
		a.hintBar(c, k, a.toast, hints)
	})
}

// numberField is a number setting: Enter, or 保存 once changed, applies it.
func numberField(c *ui.Context, k ink, key, label, unit string, v *string, stored int, apply func()) {
	ui.Row(c.Key(key)).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(13).Grow(1)
		ui.TextInput(c.Key("in"), v).Label(label).Width(72).OnSubmit(apply)
		ui.Text(c, unit).FontSize(12).TextColor(k.muted).Width(28)
		ui.Button(c.Key("ok"), "保存").Disabled(strings.TrimSpace(*v) == fmt.Sprint(stored)).OnClick(apply)
	})
}

func (a *app) tabGeneral(c *ui.Context, k ink) {
	section(c, k, "轮换提醒", fmt.Sprintf("每天检查一次：快到期或超期的密钥用%s提醒，%s图标旁显示数量。", notifyName(), barName()), func() {
		remind := a.settings.Remind
		option(c, k, "到期提醒", "", &remind, func() {
			a.settings.Remind = remind
			a.saveSettings(a.settings)
		})
		days := a.inv.RotateDays
		if days == 0 {
			days = vault.DefaultRotateDays
		}
		numberField(c, k, "days", "默认轮换周期", "天", &a.editDays, days, func() { a.applyNumber("days") })
		numberField(c, k, "remind", "提前提醒", "天", &a.editRemind, a.settings.RemindDays, func() { a.applyNumber("remind") })
	})
	section(c, k, "常用操作", "", func() {
		ui.Row(c.Key("actions")).Gap(8).Children(func() {
			smallButton(c.Key("import"), k, "导入 .env…", "", k.link, a.openImport)
			smallButton(c.Key("log"), k, "访问记录…", "", k.muted, a.openLog)
		})
	})
	section(c, k, "启动", "", func() {
		on := a.loginItem()
		option(c, k, "登录时启动", "在后台运行，才能提醒轮换、批准代理的请求。", &on, func() {
			if err := a.setLoginItem(on); err != nil {
				a.say("设置失败：" + err.Error())
			}
		})
		field(c, k, "唤起", keysLabel(a.settings.Shortcut))
	})
	section(c, k, "命令行 keys", "终端里用 keys ls、keys run -- npm start 等。链接到本程序，与 App 共用钥匙串授权。", func() {
		ui.Row(c.Key("cli")).Children(func() {
			ui.Button(c.Key("install"), "安装 keys 命令").OnClick(func() {
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
	a.connectSection(c, k)
	section(c, k, "批准", "代理每次用密钥，这里都会弹出请求：允许一次、限时允许或拒绝，按代理和密钥分别授权。", func() {
		numberField(c, k, "grant", "限时允许", "分钟", &a.editGrant, a.settings.GrantMinutes, func() { a.applyNumber("grant") })
		cli := a.settings.CLIApproval
		option(c, k, "命令行非交互使用也要批准", "没有终端时（如代理在 shell 里运行 keys get / keys run），同样弹出请求。", &cli, func() {
			a.settings.CLIApproval = cli
			a.saveSettings(a.settings)
		})
	})
	gs := a.grants.List(a.now())
	section(c, k, fmt.Sprintf("当前授权 %d", len(gs)), "", func() {
		if len(gs) == 0 {
			ui.Text(c, "没有限时授权。").FontSize(12).TextColor(k.muted)
		}
		for i, g := range gs {
			ui.Row(c.Key(i)).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, approval.ClientName(g.Client)).FontSize(13)
				ui.Text(c, g.Key).FontSize(12).TextColor(k.muted).Grow(1).Shrink(1).SingleLine()
				ui.Text(c, "到 "+g.Until.Local().Format("15:04")).FontSize(12).TextColor(k.muted)
			})
		}
		if len(gs) > 0 {
			ui.Row(c.Key("revoke")).Children(func() {
				smallButton(c.Key("b"), k, "全部撤销", "", k.muted, func() { a.grants.Revoke(""); a.say("已撤销全部授权") })
			})
		}
	})
	section(c, k, "其它代理", "支持 MCP 的代理都能用：命令是本程序，参数 mcp --client 名字。", func() {
		ui.Row(c.Key("copycfg")).Gap(8).Children(func() {
			smallButton(c.Key("json"), k, "复制 MCP 配置", "", k.link, func() { a.copy(a.mcpConfig("")); a.say("已复制 MCP 配置") })
		})
	})
}

// connectSection is the agents on this computer, each with its state as
// a dot and one action: LB Bar's rows, no cards.
func (a *app) connectSection(c *ui.Context, k ink) {
	note := "把 KeyBar 加进代理的 MCP 设置（改之前先备份原文件）。之后代理用 run_with_secrets 带着密钥跑命令：按项目 .env.example 里的变量名自动挑密钥，每次你点批准，代理看不到值。"
	section(c, k, "连接 AI 代理", note, func() {
		if a.agents == nil {
			ui.Text(c, "这里没法检测代理。").FontSize(12).TextColor(k.muted)
			return
		}
		// Installed first; the rest folded under 未安装, dimmed.
		var missing []agents.Status
		ui.Column(c.Key("agents")).Gap(0).Children(func() {
			for _, st := range a.agentState {
				if st.State == agents.NotInstalled {
					missing = append(missing, st)
					continue
				}
				a.agentRow(c.Key(st.ID), k, st)
			}
			if len(missing) == 0 {
				return
			}
			chev := "›"
			if a.showMissing {
				chev = "⌄"
			}
			label := fmt.Sprintf("未安装 %d", len(missing))
			fold := ui.Row(c.Key("fold")).Gap(10).Padding(7, 8).Margin(0, -8).Radius(7).AlignItems(ui.Center).
				Cursor(ui.CursorPointer).Label(label).OnClick(func() { a.showMissing = !a.showMissing })
			if fold.Hovered() {
				fold.Background(k.hover)
			}
			fold.Children(func() {
				ui.Box(c).Size(7, 7).Shrink(0)
				ui.Text(c, label).FontSize(12).TextColor(k.faint).Grow(1)
				ui.Text(c, chev).FontSize(13).TextColor(k.faint)
			})
			if a.showMissing {
				for _, st := range missing {
					a.agentRow(c.Key("m-"+st.ID), k, st)
				}
			}
		})
		if n := a.toConnect(); n > 1 {
			ui.Row(c.Key("all")).Children(func() {
				smallButton(c.Key("b"), k, fmt.Sprintf("全部连接（%d）", n), "⌘↵", k.link, a.connectAll)
			})
		}
	})
}

func (a *app) agentRow(c *ui.Context, k ink, st agents.Status) {
	if st.State == agents.NotInstalled {
		// Folded away below the rest: one dim line each.
		ui.Row(c).Gap(10).Padding(4, 8).Margin(0, -8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(7, 7).Radius(3.5).Background(k.line).Shrink(0)
			ui.Text(c, st.Name).FontSize(12).TextColor(k.faint).Grow(1).SingleLine()
		})
		return
	}
	col, sub := k.line, "未安装"
	action, on := "", true
	where := a.shortHome(st.File)
	if st.Via != "" {
		where = "通过 " + strings.TrimSuffix(st.Via, " CLI") + " 命令 · " + where
	}
	nameCol := k.text
	if st.State == agents.NotInstalled {
		nameCol = k.faint
	}
	switch st.State {
	case agents.Connected:
		col, sub, action, on = k.green, "已连接 · "+where, "断开", false
	case agents.Outdated:
		col, sub, action = k.gold, "指向旧的程序路径，需要更新 · "+where, "更新"
	case agents.NotConnected:
		col, sub, action = k.faint, "未连接 · "+where, "连接"
	}
	if e := a.agentErr[st.ID]; e != "" {
		sub = e
	} else if st.Err != "" {
		sub = st.Err
	}
	row := ui.Row(c).Gap(10).Padding(7, 8).Margin(0, -8).Radius(7).AlignItems(ui.Center)
	if row.Hovered() {
		row.Background(k.hover)
	}
	row.Children(func() {
		ui.Box(c).Size(7, 7).Radius(3.5).Background(col).Shrink(0)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, st.Name).FontSize(13).FontWeight(550).TextColor(nameCol)
			subCol := k.faint
			if a.agentErr[st.ID] != "" || st.Err != "" {
				subCol = k.danger
			}
			ui.Text(c, sub).FontSize(11).TextColor(subCol).SingleLine().Shrink(1).MinWidth(0)
		})
		if action != "" {
			fg := k.link
			if !on {
				fg = k.muted
			}
			linkButton(c.Key("act"), k, action, fg, func() { a.connectAgent(st.Agent, on) })
		}
	})
}

// linkButton is a plain-text action, as Raycast's: no border, a hover.
func linkButton(c *ui.Context, k ink, label string, col ui.Color, fn func()) {
	b := ui.Box(c).Padding(3, 8).Radius(6).Cursor(ui.CursorPointer).Label(label).OnClick(fn)
	if b.Hovered() {
		b.Background(k.hover)
	}
	b.Children(func() { ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(col).SingleLine() })
}

// shortHome writes the home folder as ~.
func (a *app) shortHome(p string) string {
	home, err := os.UserHomeDir()
	if a.agents != nil && a.agents.Home != "" {
		home, err = a.agents.Home, nil
	}
	if err == nil && home != "" && strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}

func (a *app) tabAbout(c *ui.Context, k ink) {
	section(c, k, "KeyBar "+version, "密钥的值只存在"+storeName(goos)+"里；KeyBar 自己的文件里只有名字、平台、日期这些元数据，以及访问记录。", func() {
		field(c, k, "数据目录", a.v.Dir)
		field(c, k, "值", storeShort()+" · 服务名 "+vault.Service)
		field(c, k, "源代码", "github.com/horizon0514/keybar · MIT")
	})
	section(c, k, "安全边界", "批准机制挡住的是通过 MCP 和非交互命令行来的请求；以你的身份运行的任意程序仍可能直接读钥匙串（macOS 会按签名询问）。别给不信任的代码完整的 shell 权限。", func() {})
	ui.Row(c.Key("quit")).Children(func() {
		ui.Button(c.Key("q"), "退出 KeyBar  "+hk("⌘Q")).OnClick(a.quit)
	})
}

func (a *app) cheatsheet(c *ui.Context, k ink) {
	rows := [][3]string{
		{"列表", "直接打字", "搜索名称、说明、平台、项目"},
		{"", "↑ ↓", "选择"},
		{"", "↵  ⌘C", "复制值（30 秒后清除剪贴板）"},
		{"", "⌘N", "添加密钥"},
		{"", "", ""},
		{"详情", "⌘R  ⌘C", "轮换 / 复制值"},
		{"", "⌘⌫", "删除"},
		{"", "esc", "回到列表"},
		{"批准", "↵", "允许一次"},
		{"", "⌘↵", fmt.Sprintf("允许 %d 分钟", a.settings.GrantMinutes)},
		{"", "esc", "拒绝"},
		{"通用", keysLabel(a.settings.Shortcut), "唤起 / 隐藏窗口"},
		{"", "⌘,", "设置"},
		{"", "⌘Q", "退出"},
	}
	ui.Column(c).Fill().Children(func() {
		a.grip(ui.Row(c.Key("head"))).Padding(14, 20).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "键盘快捷键").FontSize(15).FontWeight(650).Grow(1)
			ui.Text(c, "全键盘操作，鼠标可选").FontSize(12).TextColor(k.faint)
		})
		ui.Divider(c)
		ui.Scroll(c.Key("cheat")).Grow(1).Padding(10, 24, 14, 24).Gap(1).Children(func() {
			for i, g := range rows {
				if g[0] != "" {
					ui.Text(c.Key(fmt.Sprint("h", i)), g[0]).FontSize(11).FontWeight(600).TextColor(k.faint).Margin(10, 0, 3, 0)
				}
				ui.Row(c.Key(i)).Gap(14).Padding(3, 0).AlignItems(ui.Center).Children(func() {
					ui.Text(c, hk(g[1])).FontSize(12).FontWeight(600).TextColor(k.link).Width(150).SingleLine()
					ui.Text(c, g[2]).FontSize(12).Grow(1).SingleLine()
				})
			}
		})
		ui.Divider(c)
		a.hintBar(c, k, keysLabel(a.settings.Shortcut)+" 随时唤起", hint("esc", "关闭"))
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
