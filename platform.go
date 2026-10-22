package main

import (
	"regexp"
	"runtime"
	"strings"
)

// macHints spells keys as macOS does (⌘, ⌥) on macOS; tests set it.
var macHints = runtime.GOOS == "darwin"

func cmdKey() string {
	if macHints {
		return "⌘"
	}
	return "Ctrl+"
}

var modGlyphs = regexp.MustCompile(`[⌃⌥⇧⌘]+`)

// hk spells keys written as the Mac does (⌘R, ⇥, ↵) as the system does:
// Ctrl+R, Tab, Enter on Windows and Linux.
func hk(s string) string {
	if macHints {
		return s
	}
	s = strings.NewReplacer("⇥", "Tab", "↵", "Enter", "⌫", "Backspace").Replace(s)
	return modGlyphs.ReplaceAllStringFunc(s, func(m string) string {
		var b strings.Builder
		for _, p := range []struct{ g, name string }{{"⌘", "Ctrl+"}, {"⌃", "Ctrl+"}, {"⌥", "Alt+"}, {"⇧", "Shift+"}} {
			if strings.Contains(m, p.g) && !strings.Contains(b.String(), p.name) {
				b.WriteString(p.name)
			}
		}
		return b.String()
	})
}

// hint is the hint bar's text for keys and what they do.
func hint(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if b.Len() > 0 {
			b.WriteString("   ")
		}
		b.WriteString(hk(pairs[i]) + " " + pairs[i+1])
	}
	return b.String()
}

// shortcutsFor are the global shortcut that shows the panel, and the one
// to try when another app has it: ⌥K on the Mac; Alt+Shift+K elsewhere,
// where Alt+letter is the menus' access keys.
func shortcutsFor(goos string) (def, fallback string) {
	if goos == "darwin" {
		return "Alt+K", "Alt+Shift+K"
	}
	return "Alt+Shift+K", "Ctrl+Alt+K"
}

var defaultShortcut, fallbackShortcut = shortcutsFor(runtime.GOOS)

// keysLabel spells a global shortcut as the system does.
func keysLabel(s string) string {
	if !macHints {
		return s
	}
	return strings.NewReplacer("CmdOrCtrl+", "⌘", "Cmd+", "⌘", "Ctrl+", "⌃", "Alt+", "⌥", "Shift+", "⇧").Replace(s)
}

// storeName is the system's credential store.
func storeName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS 钥匙串"
	case "windows":
		return "Windows 凭据管理器"
	}
	return "系统密钥环（Secret Service）"
}

var goos = runtime.GOOS

func setPlatform(g string) {
	goos = g
	macHints = g == "darwin"
	defaultShortcut, fallbackShortcut = shortcutsFor(g)
}
