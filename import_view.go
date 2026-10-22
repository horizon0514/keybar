package main

import (
	"fmt"
	"strings"

	"keybar/importer"

	"github.com/egoist/mygo/ui"
)

// The import page: a .env file's path, then its variables, the names
// KeyBar already has with another value to decide one by one.

func (a *app) importView(c *ui.Context, k ink) {
	im := &a.imp
	typing := im.typing
	im.typing = false
	if c.Shortcut(0, ui.KeyEscape) {
		a.back()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyEnter) {
		a.applyImport()
		return
	}
	if im.plan != nil && !typing {
		n := len(im.plan.Conflicts())
		switch {
		case c.Shortcut(0, ui.KeyDown):
			im.chosen = min(im.chosen+1, max(0, n-1))
		case c.Shortcut(0, ui.KeyUp):
			im.chosen = max(im.chosen-1, 0)
		case c.Shortcut(0, ui.KeyRight):
			a.importChoose(1)
		case c.Shortcut(0, ui.KeyLeft):
			a.importChoose(-1)
		}
	}
	title, right := "导入 .env", ""
	if im.plan != nil {
		right = a.shortHome(im.plan.Path)
	}
	ui.Column(c).Fill().Children(func() {
		a.head(c, k, title, func() {
			if right != "" {
				ui.Text(c, right).FontSize(11).TextColor(k.faint).SingleLine().Shrink(1)
			}
		})
		pageBody(c, "import", 16, func() {
			if im.plan == nil {
				a.importPath(c, k)
			} else {
				a.importPlan(c, k)
			}
			if im.err != "" {
				ui.Text(c.Key("err"), im.err).FontSize(12).TextColor(k.danger).LineHeight(1.4)
			}
		})
		ui.Divider(c)
		if im.plan == nil {
			a.hintBar(c, k, "只读变量名和值，值不会显示", hint("↵", "读取", "esc", "返回"))
		} else if len(im.plan.Conflicts()) > 0 {
			a.hintBar(c, k, "值只用来比较，不显示", hint("↑↓", "选择", "←→", "保留 / 覆盖 / 改名", "⌘↵", "导入", "esc", "返回"))
		} else {
			a.hintBar(c, k, "值只用来比较，不显示", hint("⌘↵", "导入", "esc", "返回"))
		}
	})
}

// importPath asks for the file.
func (a *app) importPath(c *ui.Context, k ink) {
	im := &a.imp
	ui.Text(c.Key("intro"), "把项目 .env 里的密钥收进钥匙串：变量名就是密钥名，之后 keys run 和 AI 代理按名字取用，不用再写配置。KeyBar 里已有的同名密钥，下一步逐个决定。").
		FontSize(12).TextColor(k.muted).LineHeight(1.5)
	ui.Column(c.Key("path")).Gap(6).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconFile).FontSize(16).TextColor(k.muted)
			in := ui.TextInputBase(c.Key("in"), &im.path).Grow(1).FontSize(15).Font("monospace").
				Placeholder("~/code/my-app/.env").Label(".env 文件").AutoFocus()
			if in.Submitted() {
				a.readImport()
			}
			if in.Changed() {
				im.err = ""
			}
			if a.pickFile != nil {
				linkButton(c.Key("pick"), k, "选择文件…", k.link, func() {
					a.pickFile(func(p string) {
						im.path = a.shortHome(p)
						a.readImport()
					})
				})
			}
		})
		ui.Divider(c)
	})
	ui.Row(c.Key("btns")).Gap(8).Children(func() {
		ui.Spacer(c)
		ui.Button(c.Key("cancel"), "取消").OnClick(a.back)
		ui.PrimaryButton(c.Key("read"), "读取  "+hk("↵")).OnClick(a.readImport)
	})
}

// importPlan is the file's variables: the conflicts first, each with its
// three choices as words, then the rest, each a dot and what happens.
func (a *app) importPlan(c *ui.Context, k ink) {
	im := &a.imp
	p := im.plan
	conflicts := p.Conflicts()
	var counts [4]int
	for _, it := range p.Items {
		counts[it.State]++
	}
	var parts []string
	for _, x := range []struct {
		n    int
		what string
	}{{counts[importer.New], "新增"}, {counts[importer.Conflict], "同名不同值"}, {counts[importer.Same], "已有相同值"}, {counts[importer.Empty], "空值"}} {
		if x.n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", x.what, x.n))
		}
	}
	ui.Text(c.Key("sum"), fmt.Sprintf("%s · %d 个变量：%s", p.Project, len(p.Items), strings.Join(parts, " · "))).FontSize(12).TextColor(k.muted)
	if len(conflicts) > 0 {
		block(c, k, fmt.Sprintf("同名但值不同 %d", len(conflicts)), "保留现有 · 覆盖（旧值记为一次轮换）· 改名导入", func() {
			for i, it := range conflicts {
				a.conflictRow(c.Key(it.Var), k, i, it)
			}
		})
	}
	var rest []*importer.Item
	for _, it := range p.Items {
		if it.State != importer.Conflict {
			rest = append(rest, it)
		}
	}
	if len(rest) > 0 {
		title := "其余 " + fmt.Sprint(len(rest))
		if len(conflicts) == 0 {
			title = "变量 " + fmt.Sprint(len(rest))
		}
		block(c, k, title, "", func() {
			for _, it := range rest {
				text, col := "新增", k.green
				switch it.State {
				case importer.Same:
					text, col = "已有，值相同 · 跳过", k.faint
				case importer.Empty:
					text, col = "值是空的 · 跳过", k.faint
				}
				ui.Row(c.Key(it.Var)).Gap(10).Padding(5, 0).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(6, 6).Radius(3).Background(col).Shrink(0)
					ui.Text(c, it.Key).FontSize(13).Font("monospace").SingleLine().Shrink(1).MinWidth(0)
					repeatNote(c, k, it)
					ui.Spacer(c)
					ui.Text(c, text).FontSize(11).TextColor(col).SingleLine()
				})
			}
		})
	}
	ui.Row(c.Key("shred")).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Checkbox(c.Key("cb"), &im.shred, "").Label("导入后删除这个 .env")
		ui.Text(c, "导入后覆盖并删除这个 .env（值都进了钥匙串才删）").FontSize(12).TextColor(k.muted)
	})
	n := 0
	for _, it := range p.Items {
		if it.Target() != "" && it.State != importer.Same {
			n++
		}
	}
	ui.Row(c.Key("btns")).Gap(8).Children(func() {
		ui.Spacer(c)
		ui.Button(c.Key("back"), "换个文件").OnClick(a.back)
		label := fmt.Sprintf("导入 %d 个  %s", n, hk("⌘↵"))
		if n == 0 {
			label = "完成  " + hk("⌘↵")
		}
		ui.PrimaryButton(c.Key("go"), label).OnClick(a.applyImport)
	})
}

func repeatNote(c *ui.Context, k ink, it *importer.Item) {
	if it.Repeated() {
		ui.Text(c, fmt.Sprintf("出现 %d 次，用第 %d 行", len(it.Lines), it.Line)).FontSize(11).TextColor(k.gold).SingleLine()
	}
}

var choiceNames = []string{"保留现有", "覆盖", "改名"}

// conflictRow is a name KeyBar has with another value: the name, the
// choices as words (the chosen one in the accent color), and under it
// what the choice does, or the new name to edit.
func (a *app) conflictRow(c *ui.Context, k ink, i int, it *importer.Item) {
	im := &a.imp
	chosen := i == im.chosen
	col := ui.Column(c).Gap(4).Padding(7, 10).Margin(0, -10).Radius(8)
	if chosen {
		col.Background(k.chosen)
	} else if col.Hovered() {
		col.Background(k.hover)
	}
	col.OnClick(func() { im.chosen = i })
	col.Children(func() {
		ui.Row(c.Key("top")).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(k.gold).Shrink(0)
			ui.Text(c, it.Key).FontSize(13).FontWeight(550).Font("monospace").SingleLine().Shrink(1).MinWidth(0)
			repeatNote(c, k, it)
			ui.Spacer(c)
			for j, name := range choiceNames {
				on := int(it.Choice) == j
				fg, w := k.muted, 450
				if on {
					fg, w = k.link, 650
				}
				ui.Box(c.Key(name)).Padding(2, 6).Radius(5).Cursor(ui.CursorPointer).Label(it.Key + " " + name).
					OnClick(func() { it.Choice, im.chosen = importer.Choice(j), i }).Children(func() {
					ui.Text(c, name).FontSize(12).FontWeight(w).TextColor(fg).SingleLine()
				})
			}
		})
		ui.Row(c.Key("sub")).Gap(8).Padding(0, 0, 0, 16).AlignItems(ui.Center).Children(func() {
			switch it.Choice {
			case importer.Keep:
				ui.Text(c, "KeyBar 里的值不变，文件里的这个不导入").FontSize(11).TextColor(k.faint)
			case importer.Overwrite:
				note := "换成文件里的值，旧值记为一次轮换"
				if key, ok := a.key(it.Key); ok && len(key.Platforms) > 0 {
					note += fmt.Sprintf("；%d 个平台之后要更新", len(key.Platforms))
				}
				ui.Text(c, note).FontSize(11).TextColor(k.gold)
			case importer.Rename:
				ui.Text(c, "导入为").FontSize(11).TextColor(k.faint)
				in := ui.TextInputBase(c.Key("name"), &it.NewName).FontSize(12).Font("monospace").Grow(1).
					Label(it.Key + " 的新名字").Placeholder(it.Key + "_PROJECT")
				if in.Focused() {
					im.typing = true
					im.chosen = i
				}
				if in.Submitted() {
					a.applyImport()
				}
				ui.Text(c, "变量名不变，记在 keys.toml").FontSize(11).TextColor(k.faint).SingleLine()
			}
		})
	})
}
