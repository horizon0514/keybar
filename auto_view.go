package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"keybar/importer"
	"keybar/scan"

	"github.com/egoist/mygo/ui"
)

// The notices on top of the list: a key copied, a new .env. One line
// each, as Spotlight's: no box, a dot, the words, the actions as links.

func (a *app) autoNotices(c *ui.Context, k ink) {
	f := a.auto.found
	notes := a.auto.envNotes
	if f == nil && len(notes) == 0 {
		return
	}
	ui.Column(c.Key("notices")).Children(func() {
		if f != nil {
			ui.Row(c.Key("clip")).Gap(8).Padding(8, 16).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconKey).FontSize(14).TextColor(k.gold)
				ui.Text(c, "发现 "+f.provider+" key · 保存为").FontSize(13).SingleLine().Shrink(0)
				in := ui.TextInputBase(c.Key("name"), &f.name).FontSize(13).Font("monospace").FontWeight(600).
					Grow(1).MinWidth(120).Label("保存的名字")
				if in.Focused() {
					// the list's and the detail's keys wait while the name is edited
					a.typing, a.auto.typing = true, true
				}
				if in.Submitted() {
					a.saveClip()
				}
				ui.Text(c, f.masked).FontSize(11).TextColor(k.faint).SingleLine().Shrink(0)
				linkButton(c.Key("save"), k, "保存  "+hk("⌘S"), k.link, a.saveClip)
				linkButton(c.Key("no"), k, "忽略", k.muted, a.dismissClip)
			})
			if c.Shortcut(ui.Cmd, ui.KeyS) {
				a.saveClip()
			}
			ui.Divider(c)
		}
		for i, n := range notes {
			if i >= 2 {
				ui.Text(c.Key("more"), fmt.Sprintf("还有 %d 个 .env 有新 key", len(notes)-2)).FontSize(11).TextColor(k.faint).Padding(4, 16)
				ui.Divider(c)
				break
			}
			ui.Row(c.Key(n.path)).Gap(8).Padding(8, 16).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconFile).FontSize(14).TextColor(k.gold)
				ui.Text(c, fmt.Sprintf("%s 有 %d 个新 key", envLabel(n.path), n.n)).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
				ui.Text(c, a.shortHome(n.path)).FontSize(11).TextColor(k.faint).SingleLine().Shrink(1).MinWidth(0)
				ui.Spacer(c)
				linkButton(c.Key("go"), k, "导入", k.link, func() { a.openScan(n.path) })
				linkButton(c.Key("later"), k, "稍后", k.muted, func() { a.dropEnvNote(n.path) })
				linkButton(c.Key("ignore"), k, "忽略此文件", k.muted, func() { a.ignoreEnv(n.path) })
			})
			ui.Divider(c)
		}
	})
}

// The scan page: what was found, file by file, the likely keys checked.

var cleanNames = []string{"注释掉", "只留变量名", "删除整行"}

func (a *app) scanView(c *ui.Context, k ink) {
	s := &a.auto.scan
	typing := s.typing
	s.typing = false
	if c.Shortcut(0, ui.KeyEscape) && !typing {
		a.back()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyEnter) {
		a.importScan()
		return
	}
	title := "扫描本机"
	if s.file != "" {
		title = "导入 " + envLabel(s.file)
	}
	files, keys := scan.Checked(s.groups)
	ui.Column(c).Fill().Children(func() {
		a.head(c, k, title, func() {
			if !s.running && len(s.groups) > 0 {
				ui.Text(c, fmt.Sprintf("%d 个文件 · 勾选 %d", len(s.groups), keys)).FontSize(11).TextColor(k.faint).SingleLine()
			}
		})
		pageBody(c, "scan", 18, func() {
			switch {
			case s.running:
				ui.Text(c.Key("wait"), "正在看 "+a.rootsLabel()+" 和 shell 配置…").FontSize(13).TextColor(k.muted)
				return
			case len(s.groups) == 0:
				ui.Text(c.Key("none"), "没找到带值的 .env，shell 配置里也没有 export 的密钥。扫描的位置在 设置 › 自动录入 里改。").
					FontSize(13).TextColor(k.muted).LineHeight(1.5)
				return
			}
			intro := "勾选的进钥匙串；像密钥的已勾选。值只显示前缀和长度。"
			if s.file == "" {
				intro = "扫描了 " + a.rootsLabel() + " 和 shell 配置。" + intro
			}
			ui.Text(c.Key("intro"), intro).FontSize(12).TextColor(k.muted).LineHeight(1.45)
			for _, g := range s.groups {
				a.scanGroup(c.Key(g.Plan.Path), k, g)
			}
			if s.err != "" {
				ui.Text(c.Key("err"), s.err).FontSize(12).TextColor(k.danger)
			}
			ui.Row(c.Key("btns")).Gap(8).Children(func() {
				ui.Spacer(c)
				ui.Button(c.Key("cancel"), "取消").OnClick(a.back)
				label := fmt.Sprintf("导入 %d 个  %s", keys, hk("⌘↵"))
				if files > 1 {
					label = fmt.Sprintf("从 %d 个文件导入 %d 个  %s", files, keys, hk("⌘↵"))
				}
				ui.PrimaryButton(c.Key("go"), label).Disabled(keys == 0).OnClick(a.importScan)
			})
		})
		ui.Divider(c)
		a.hintBar(c, k, "值不显示，只用来比较", hint("⌘↵", "导入勾选的", "esc", "返回"))
	})
}

func (a *app) rootsLabel() string {
	roots := a.settings.ScanRoots
	if len(roots) == 0 {
		return "~/code、~/projects 等常用目录"
	}
	var out []string
	for _, r := range roots {
		out = append(out, a.shortHome(expandHome(r)))
	}
	if len(out) > 3 {
		return strings.Join(out[:3], "、") + " 等"
	}
	return strings.Join(out, "、")
}

func (a *app) scanGroup(c *ui.Context, k ink, g scan.Group) {
	s := &a.auto.scan
	p := g.Plan
	note := p.Project
	if g.Found.Kind == scan.ShellRC {
		note = "shell 配置 · export"
	}
	block(c, k, a.shortHome(p.Path), note, func() {
		for _, it := range p.Items {
			a.scanItem(c.Key(it.Var), k, p, it)
		}
		if !anyChecked(g) {
			return
		}
		on := s.clean[p.Path]
		ui.Row(c.Key("clean")).Gap(8).Padding(6, 0, 0, 0).AlignItems(ui.Center).Children(func() {
			if ui.Checkbox(c.Key("cb"), &on, "").Label("导入后从 " + a.shortHome(p.Path) + " 移除明文").Changed() {
				s.clean[p.Path] = on
			}
			ui.Text(c, "导入后移除明文").FontSize(12).TextColor(k.muted).SingleLine()
			if on && g.Found.Kind == scan.DotEnv {
				for j, name := range cleanNames {
					fg, w := k.faint, 450
					if int(s.mode[p.Path]) == j {
						fg, w = k.link, 650
					}
					ui.Box(c.Key(name)).Padding(2, 5).Radius(5).Cursor(ui.CursorPointer).Label(name).
						OnClick(func() { s.mode[p.Path] = scan.Mode(j) }).Children(func() {
						ui.Text(c, name).FontSize(12).FontWeight(w).TextColor(fg).SingleLine()
					})
				}
			}
			if on && g.Found.Kind == scan.ShellRC {
				ui.Text(c, "export 行改为注释").FontSize(12).TextColor(k.faint).SingleLine()
			}
			ui.Spacer(c)
			if on {
				ui.Text(c, "先备份为 "+scan.BackupSuffix).FontSize(11).TextColor(k.faint).SingleLine()
			}
		})
	})
}

func (a *app) scanItem(c *ui.Context, k ink, p *importer.Plan, it *importer.Item) {
	s := &a.auto.scan
	on := !it.Skip && it.State != importer.Empty
	text, col := "新增", k.green
	switch it.State {
	case importer.Empty:
		text, col = "空值", k.faint
	case importer.Same:
		text, col = "已有，值相同", k.faint
		if it.Elsewhere != "" {
			text = "同 " + envLabel(it.Elsewhere)
		}
	case importer.Conflict:
		text, col = "同名不同值", k.gold
		if it.Elsewhere != "" {
			text = "与 " + envLabel(it.Elsewhere) + " 不同"
		}
	}
	if !on && it.State != importer.Empty {
		col = k.faint
	}
	ui.Column(c).Gap(2).Children(func() {
		ui.Row(c.Key("row")).Gap(10).Padding(4, 0).AlignItems(ui.Center).Children(func() {
			cb := ui.Checkbox(c.Key("cb"), &on, "").Label(it.Var)
			if it.State == importer.Empty {
				cb.Disabled(true)
			}
			if cb.Changed() {
				it.Skip = !on
			}
			ui.Text(c, it.Var).FontSize(13).Font("monospace").FontWeight(550).SingleLine().Shrink(1).MinWidth(0)
			ui.Text(c, it.Masked).FontSize(11).TextColor(k.faint).SingleLine().Shrink(0)
			if !it.Likely && it.State != importer.Empty {
				ui.Text(c, "不像密钥").FontSize(11).TextColor(k.faint).SingleLine()
			}
			ui.Spacer(c)
			ui.Text(c, text).FontSize(11).TextColor(col).SingleLine()
		})
		if !on || it.State != importer.Conflict {
			return
		}
		ui.Row(c.Key("choice")).Gap(4).Padding(0, 0, 2, 28).AlignItems(ui.Center).Children(func() {
			names := choiceNames
			if it.Elsewhere != "" {
				names = []string{"不导入", "覆盖", "改名"}
			}
			for j, name := range names {
				fg, w := k.faint, 450
				if int(it.Choice) == j {
					fg, w = k.link, 650
				}
				ui.Box(c.Key(name)).Padding(2, 5).Radius(5).Cursor(ui.CursorPointer).Label(it.Var + " " + name).
					OnClick(func() { it.Choice = importer.Choice(j) }).Children(func() {
					ui.Text(c, name).FontSize(12).FontWeight(w).TextColor(fg).SingleLine()
				})
			}
			if it.Choice == importer.Rename {
				ui.Text(c, "为").FontSize(11).TextColor(k.faint)
				in := ui.TextInputBase(c.Key("name"), &it.NewName).FontSize(12).Font("monospace").Grow(1).
					Label(it.Var + " 的新名字").Placeholder(it.Key + "_PROJECT")
				if in.Focused() {
					s.typing = true
				}
			}
		})
	})
}

// The settings' 自动录入 tab.

func (a *app) tabAuto(c *ui.Context, k ink) {
	section(c, k, "剪贴板", "默认关闭。打开后，复制了一个认得出的 key（OpenAI、Anthropic、GitHub…）时在面板顶上提示保存。只看格式，不记录剪贴板；密码管理器标记为隐藏的内容不看。", func() {
		on := a.settings.Clipboard
		option(c, k, "检测复制的 key", "", &on, func() {
			a.settings.Clipboard = on
			if !on {
				a.dismissClip()
			}
			a.saveSettings(a.settings)
		})
	})
	section(c, k, "新的 .env", "默认关闭。打开后，扫描目录里出现新的或改过的 .env、且有 KeyBar 里没有的密钥时，提醒导入。", func() {
		on := a.settings.WatchEnv
		option(c, k, "提醒新的 .env", "", &on, func() {
			a.settings.WatchEnv = on
			if !on {
				a.auto.envNotes = nil
				a.trayChanged()
			}
			a.saveSettings(a.settings)
		})
	})
	section(c, k, "扫描的位置", "逗号分隔；留空用常用目录：~/code、~/projects、~/dev、~/src、~/workspace、~/Documents/GitHub 和主目录一层。跳过 node_modules、.git、vendor、build、Library 等。", func() {
		if !a.auto.rootsSet {
			a.auto.editRoots, a.auto.rootsSet = strings.Join(a.settings.ScanRoots, ", "), true
		}
		apply := func() {
			var roots []string
			for _, r := range strings.Split(a.auto.editRoots, ",") {
				if r = strings.TrimSpace(r); r != "" && !slices.Contains(roots, r) {
					roots = append(roots, r)
				}
			}
			a.settings.ScanRoots = roots
			a.auto.editRoots = strings.Join(roots, ", ")
			a.saveSettings(a.settings)
			a.say("扫描位置已保存")
		}
		ui.Column(c.Key("roots")).Gap(6).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconSearch).FontSize(14).TextColor(k.muted)
				in := ui.TextInputBase(c.Key("in"), &a.auto.editRoots).Grow(1).FontSize(13).Font("monospace").
					Placeholder("~/code, ~/projects, ~/work").Label("扫描的位置")
				if in.Submitted() {
					apply()
				}
				if strings.Join(a.settings.ScanRoots, ", ") != a.auto.editRoots {
					linkButton(c.Key("save"), k, "保存", k.link, apply)
				}
			})
			ui.Divider(c)
		})
		ui.Row(c.Key("act")).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Button(c.Key("scan"), "扫描本机…").OnClick(func() { a.openScan("") })
			ui.Text(c, "找出 .env 和 shell 配置里的密钥，勾选导入").FontSize(12).TextColor(k.muted)
		})
	})
	if len(a.settings.IgnoredFiles) > 0 {
		section(c, k, "不再提醒的文件", "", func() {
			for _, p := range a.settings.IgnoredFiles {
				ui.Row(c.Key(p)).Gap(8).Padding(2, 0).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.shortHome(p)).FontSize(12).Font("monospace").Grow(1).Shrink(1).MinWidth(0).SingleLine()
					linkButton(c.Key("rm"), k, "恢复", k.link, func() {
						a.settings.IgnoredFiles = slices.DeleteFunc(slices.Clone(a.settings.IgnoredFiles), func(x string) bool { return x == p })
						a.saveSettings(a.settings)
					})
				})
			}
		})
	}
	if n := len(a.settings.NeverAsk); n > 0 {
		section(c, k, "运行时不再询问", "keys run 和代理缺这些变量时，不再提示补上。", func() {
			var dirs []string
			for d := range a.settings.NeverAsk {
				dirs = append(dirs, d)
			}
			slices.Sort(dirs)
			for _, d := range dirs {
				ui.Row(c.Key(d)).Gap(8).Padding(2, 0).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.shortHome(d)).FontSize(12).TextColor(k.muted).Width(160).Shrink(0).SingleLine()
					ui.Text(c, strings.Join(a.settings.NeverAsk[d], "、")).FontSize(12).Font("monospace").Grow(1).Shrink(1).MinWidth(0).SingleLine()
					linkButton(c.Key("rm"), k, "恢复", k.link, func() {
						delete(a.settings.NeverAsk, d)
						a.saveSettings(a.settings)
					})
				})
			}
		})
	}
}

// fillRows: in an approval, the names the project needs that KeyBar
// lacks, each with a field to paste its value into (hidden).
func (a *app) fillRows(c *ui.Context, k ink, p *pending) (typing bool) {
	r := p.req
	if len(r.Missing) == 0 {
		return false
	}
	first := true
	block(c, k, fmt.Sprintf("KeyBar 里没有 %d", len(r.Missing)), "粘贴值就存进钥匙串并一起注入；留空跳过", func() {
		for _, n := range r.Missing {
			never := p.never[n]
			ui.Row(c.Key(n)).Gap(10).Padding(4, 0).AlignItems(ui.Center).Children(func() {
				col := k.text
				if never {
					col = k.faint
				}
				ui.Text(c, n).FontSize(13).Font("monospace").FontWeight(550).TextColor(col).Width(190).Shrink(0).SingleLine()
				if never {
					ui.Text(c, "这个项目里不再询问").FontSize(12).TextColor(k.faint).Grow(1)
					linkButton(c.Key("undo"), k, "撤销", k.link, func() { p.never[n] = false })
					return
				}
				in := ui.TextInputBase(c.Key("v"), p.fill[n]).Password().FontSize(13).Font("monospace").Grow(1).
					Placeholder("粘贴值保存，留空跳过").Label(n + " 的值")
				if first {
					// keys typed as the panel appears land here, not on 允许
					in.AutoFocus()
					first = false
				}
				if in.Focused() {
					typing = true
				}
				if in.Submitted() && a.now().Sub(p.at) > 600*time.Millisecond {
					a.decide(true, false)
				}
				if r.Project != "" {
					linkButton(c.Key("never"), k, "不再询问", k.muted, func() { p.never[n] = true; *p.fill[n] = "" })
				}
			})
			ui.Divider(c.Key(n + "-line"))
		}
	})
	return typing
}
