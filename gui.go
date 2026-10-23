package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"keybar/clip"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"keybar/approval"
	"keybar/vault"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// shell is the app on the system: the panel, the tray icon, the global
// shortcut, the IPC server for agents' requests.
type shell struct {
	a        *app
	panel    *mygo.Window
	tray     *mygo.Tray
	ipc      *approval.Server
	shortcut string
	quitting bool
	picking  bool // a file dialog is open: the panel stays

	title, tip, menuSig string

	mu         sync.Mutex
	dragFrom   mygo.Rectangle
	dragCursor mygo.Point
	dragging   bool
	pos        *mygo.Point // where the panel was moved to
}

func guiMain() {
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	s := &shell{}
	mygo.App.SetName("KeyBar")
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnSecondInstance(func([]string, string) { s.panel.Update(s.show) })
	mygo.App.WhenReady(s.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func (s *shell) ready() {
	demo := os.Getenv("KEYBAR_DEMO") == "1"
	var v *vault.Vault
	if demo {
		v = demoVault()
	} else {
		v = newVault("app")
	}
	a := newApp(v)
	a.demo = demo
	if exe, err := os.Executable(); err == nil {
		a.exe = exe
	}
	a.agents = agentEnv()
	if demo {
		a.agents = demoAgents(a.agents.Exe)
	}
	if a.settings.Shortcut == "" {
		a.settings.Shortcut = defaultShortcut
	}
	s.a = a
	// Use a standard, titled window (with traffic lights) instead of a borderless panel.
	// Keep LSUIElement (menubar-only) so there is no Dock icon; closing hides the window.
	s.panel = mygo.NewWindow(mygo.WindowOptions{
		Title: "KeyBar", Panel: false, Hidden: true, Frameless: false, Transparent: false,
		Width: panelWidth, Height: panelHeight, MinWidth: minWidth, MinHeight: minHeight,
		Content: ui.View(a.view),
	})
	s.panel.OnClose(func(e *mygo.CloseEvent) {
		if !s.quitting {
			e.PreventDefault()
			s.panel.Hide()
		}
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { s.quitting = true })
	// Do not auto-hide on blur: standard app window behavior.
	a.post = func(fn func()) { s.panel.Update(fn) }
	a.spawn = func(fn func()) { go fn() }
	a.later = func(d time.Duration, fn func()) { time.AfterFunc(d, func() { s.panel.Update(fn) }) }
	a.show = s.show
	a.hide = s.panel.Hide
	a.quit = s.quit
	a.notify = func(id, title, body string) {
		go func() {
			n := mygo.NewNotification(mygo.NotificationOptions{ID: id, Title: title, Body: body, Group: "keybar"})
			if err := n.Show(); err != nil {
				log.Print("notify: ", err)
			}
		}()
	}
	a.copy = func(t string) {
		if err := mygo.Clipboard.Write(transfer.TextData(t)); err != nil {
			log.Print("copy: ", err)
		}
	}
	a.loginItem = mygo.App.OpenAtLogin
	a.setLoginItem = mygo.App.SetOpenAtLogin
	a.installCLI = func() (string, error) { return installCLI(a.exe) }
	a.openURL = func(u string) { _ = mygo.Shell.OpenExternal(u) }
	a.trayChanged = s.trayChanged
	a.pickFile = func(done func(string)) {
		s.picking = true
		go func() {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "选择要导入的 .env 文件", ButtonLabel: "读取", ShowHiddenFiles: true})
			s.panel.Update(func() {
				s.picking = false
				if err != nil {
					log.Print("open dialog: ", err)
				}
				if len(paths) > 0 {
					done(paths[0])
				}
				s.show()
			})
		}()
	}
	// Disable custom drag handling; standard windows are moved by the title bar.
	a.winDrag = nil
	mygo.App.OnNotificationClick(func(string) {
		s.panel.Update(func() { a.openDueRotate(); s.show() })
	})

	if err := s.setShortcut(a.settings.Shortcut); err != nil {
		log.Printf("shortcut %s: %v", a.settings.Shortcut, err)
		if s.setShortcut(fallbackShortcut) == nil {
			a.settings.Shortcut = fallbackShortcut
		}
	}
	srv, err := approval.Listen(v.Dir, a.request)
	if err != nil {
		log.Print("approvals: ", err)
	}
	s.ipc = srv
	s.setUpTray()
	if demo {
		demoScanHome(a.agents.Home)
		a.startAuto(&clip.Fake{}, true) // never the real clipboard
	} else if b, onMain, ok := clip.System(); ok {
		a.startAuto(b, onMain)
	} else {
		a.startAuto(nil, false)
	}
	s.every(time.Minute, func() {
		a.reload()
		a.remind()
		s.trayChanged()
	})
	a.later(3*time.Second, a.remind)
	if demo && os.Getenv("KEYBAR_DEMO_REQUEST") == "1" {
		go a.request(demoRequest())
	}
	if !mygo.App.WasOpenedAtLogin() {
		s.show()
	}
}

func (s *shell) every(d time.Duration, fn func()) {
	go func() {
		for range time.Tick(d) {
			if s.quitting {
				return
			}
			s.panel.Update(fn)
		}
	}()
}

func (s *shell) quit() {
	if s.quitting {
		return
	}
	s.quitting = true
	if s.ipc != nil {
		_ = s.ipc.Close()
	}
	for _, p := range s.a.pending { // nobody will answer them
		p.answer <- approval.Decision{Why: "denied"}
	}
	mygo.GlobalShortcut.UnregisterAll()
	go func() {
		time.Sleep(3 * time.Second)
		os.Exit(0)
	}()
	mygo.App.Quit()
}

func (s *shell) setShortcut(acc string) error {
	if err := mygo.GlobalShortcut.Register(acc, s.toggle); err != nil {
		return err
	}
	if s.shortcut != "" && s.shortcut != acc {
		mygo.GlobalShortcut.Unregister(s.shortcut)
	}
	s.shortcut = acc
	return nil
}

func (s *shell) toggle() {
	if s.panel.IsVisible() {
		s.panel.Hide()
	} else {
		s.show()
	}
}

// show shows the panel on the display with the pointer, where it was
// moved to, or a fifth of the way down.
func (s *shell) show() {
	d := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	w, h := panelWidth, min(panelHeight, d.WorkArea.Height-40)
	x, y := d.WorkArea.X+(d.WorkArea.Width-w)/2, d.WorkArea.Y+(d.WorkArea.Height-h)/5
	if s.pos != nil {
		x, y = s.pos.X, s.pos.Y
		x = max(d.WorkArea.X, min(x, d.WorkArea.X+d.WorkArea.Width-w))
		y = max(d.WorkArea.Y, min(y, d.WorkArea.Y+d.WorkArea.Height-h))
	} else if runtime.GOOS == "windows" && s.tray != nil {
		if b := s.tray.Bounds(); b.Width > 0 {
			x, y = d.WorkArea.X+d.WorkArea.Width-w-12, d.WorkArea.Y+d.WorkArea.Height-h-12
		}
	}
	s.panel.SetBounds(mygo.Rectangle{X: x, Y: y, Width: w, Height: h})
	s.a.shown()
	s.panel.Show()
	s.panel.Focus()
}

func (s *shell) drag(phase int) {
	switch phase {
	case dragStart:
		s.dragFrom, s.dragCursor, s.dragging = s.panel.Bounds(), mygo.Screen.CursorScreenPoint(), true
	case dragMove:
		if s.dragging {
			p := mygo.Screen.CursorScreenPoint()
			s.panel.SetPosition(s.dragFrom.X+p.X-s.dragCursor.X, s.dragFrom.Y+p.Y-s.dragCursor.Y)
		}
	case dragEnd:
		s.dragging = false
		b := s.panel.Bounds()
		s.pos = &mygo.Point{X: b.X, Y: b.Y}
	case dragReset:
		s.pos = nil
		s.show()
	}
}

func (s *shell) setUpTray() {
	template := runtime.GOOS != "linux"
	var err error
	s.tray, err = mygo.NewTray(mygo.TrayOptions{ID: "keybar", Icon: trayIcon(!template), IconIsTemplate: template, ToolTip: "KeyBar · 密钥管理"})
	if err != nil {
		log.Print("no tray icon: ", err)
		return
	}
	s.trayChanged()
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		s.quitting = true
		mygo.GlobalShortcut.UnregisterAll()
		s.tray.Destroy()
	})
}

// trayChanged updates the icon's text (the count of what needs the user),
// its hover text and its menu.
func (s *shell) trayChanged() {
	if s.tray == nil {
		return
	}
	a := s.a
	if t := a.trayTitle(); t != s.title {
		s.title = t
		if runtime.GOOS != "windows" {
			s.tray.SetTitle(t)
		}
	}
	tip := "KeyBar · " + a.trayLine()
	if n := len(a.pending); n > 0 {
		tip = fmt.Sprintf("KeyBar · %d 个请求待批准", n)
	}
	if tip != s.tip {
		s.tip = tip
		s.tray.SetToolTip(tip)
	}
	sig := tip + "|" + s.shortcut + "|" + a.autoSig()
	if sig == s.menuSig {
		return
	}
	s.menuSig = sig
	items := []*mygo.MenuItem{{Label: a.trayLine(), Disabled: true}}
	if n := len(a.pending); n > 0 {
		items = append(items, &mygo.MenuItem{Label: fmt.Sprintf("⏳ %d 个代理请求待批准…", n), Click: func(*mygo.MenuItem, *mygo.Window) { s.show() }})
	}
	if f := a.auto.found; f != nil {
		items = append(items, &mygo.MenuItem{Label: "保存剪贴板里的 " + f.provider + " key…", Click: func(*mygo.MenuItem, *mygo.Window) { a.page = pageList; s.show() }})
	}
	for _, n := range a.auto.envNotes {
		path := n.path
		items = append(items, &mygo.MenuItem{Label: fmt.Sprintf("%s 有 %d 个新 key · 导入…", envLabel(path), n.n), Click: func(*mygo.MenuItem, *mygo.Window) { a.openScan(path); s.show() }})
	}
	over, soon := a.dueNames()
	for _, name := range append(append([]string{}, over...), soon...) {
		n := name
		label := "自动轮换 " + n + "…"
		items = append(items, &mygo.MenuItem{Label: label, Click: func(*mygo.MenuItem, *mygo.Window) { a.openRotate(n); s.show() }})
	}
	items = append(items, mygo.Separator(),
		&mygo.MenuItem{Label: "打开 KeyBar", Accelerator: s.shortcut, Click: func(*mygo.MenuItem, *mygo.Window) { a.page = pageList; s.show() }},
		&mygo.MenuItem{Label: "添加密钥…", Click: func(*mygo.MenuItem, *mygo.Window) { a.openAdd(); s.show() }},
		&mygo.MenuItem{Label: "导入 .env…", Click: func(*mygo.MenuItem, *mygo.Window) { a.openImport(); s.show() }},
		&mygo.MenuItem{Label: "扫描本机的密钥…", Click: func(*mygo.MenuItem, *mygo.Window) { a.openScan(""); s.show() }},
		&mygo.MenuItem{Label: "连接 AI 代理…", Click: func(*mygo.MenuItem, *mygo.Window) { a.openTab(tabAgents); s.show() }},
		&mygo.MenuItem{Label: "访问记录", Click: func(*mygo.MenuItem, *mygo.Window) { a.openLog(); s.show() }},
		&mygo.MenuItem{Label: "设置…", Click: func(*mygo.MenuItem, *mygo.Window) { a.openSettings(); s.show() }},
		mygo.Separator(),
		&mygo.MenuItem{Label: "退出 KeyBar", Click: func(*mygo.MenuItem, *mygo.Window) { s.quit() }},
	)
	s.tray.SetMenu(mygo.NewMenu(items))
}

// installCLI makes `keys` run this program: a link in a folder on PATH
// (Homebrew's, /usr/local/bin, ~/.local/bin), or on Windows a keys.cmd in
// the user's WindowsApps, which is on PATH.
func installCLI(exe string) (string, error) {
	if exe == "" {
		return "", fmt.Errorf("找不到程序路径")
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if runtime.GOOS == "windows" {
		dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WindowsApps")
		p := filepath.Join(dir, "keys.cmd")
		return p, os.WriteFile(p, []byte("@\""+exe+"\" %*\r\n"), 0o644)
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".local", "bin")} {
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			if dir != filepath.Join(home, ".local", "bin") {
				continue
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				continue
			}
		}
		p := filepath.Join(dir, "keys")
		if cur, err := os.Readlink(p); err == nil && cur == exe {
			return p, nil
		}
		if _, err := os.Lstat(p); err == nil {
			if cur, err := os.Readlink(p); err != nil || filepath.Base(filepath.Dir(cur)) != "MacOS" && filepath.Base(cur) != filepath.Base(exe) {
				continue // someone else's keys
			}
			_ = os.Remove(p)
		}
		if err := os.Symlink(exe, p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("没有可写的 PATH 目录；可以手动：ln -s %q /usr/local/bin/keys", exe)
}

// trayIcon draws a key: black on transparent for macOS and Windows to
// tint, or in color, outlined, for Linux panels light and dark.
func trayIcon(colored bool) []byte {
	return png36(keyImage(36, colored))
}

func png36(img image.Image) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// keyImage draws a key, its bow a ring at the left, its blade to the
// right with two teeth, n pixels square.
func keyImage(n int, colored bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	ink := color.NRGBA{A: 255}
	if colored {
		ink = color.NRGBA{R: 0xe8, G: 0xa3, B: 0x17, A: 255}
	}
	s := float64(n) / 36
	cx, cy, ro, ri := 11*s, 18*s, 8.5*s, 4*s
	in := func(x, y float64) bool {
		d := math.Hypot(x-cx, y-cy)
		if d <= ro && d >= ri {
			return true
		}
		switch {
		case x >= cx+ro-1*s && x <= 33*s && y >= cy-2*s && y <= cy+2*s: // the blade
			return true
		case x >= 25*s && x <= 28*s && y >= cy && y <= cy+7*s: // a tooth
			return true
		case x >= 30*s && x <= 33*s && y >= cy && y <= cy+5*s:
			return true
		}
		return false
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			// 4× supersampling, for smooth edges.
			hit := 0
			for _, o := range [][2]float64{{.25, .25}, {.75, .25}, {.25, .75}, {.75, .75}} {
				if in(float64(x)+o[0], float64(y)+o[1]) {
					hit++
				}
			}
			if hit > 0 {
				c := ink
				c.A = uint8(int(c.A) * hit / 4)
				img.SetNRGBA(x, y, c)
			}
		}
	}
	return img
}
