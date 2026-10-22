//go:build !darwin && !windows

package clip

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Linux and the like: wl-paste (Wayland) or xclip (X11) when there.
// No change counter: the text is read each time.

type cmdBoard struct {
	types, text []string
}

func system() (Board, bool, bool) {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if _, err := exec.LookPath("wl-paste"); err == nil {
			return cmdBoard{types: []string{"wl-paste", "--list-types"}, text: []string{"wl-paste", "--no-newline", "--type", "text/plain"}}, false, true
		}
	}
	if os.Getenv("DISPLAY") != "" {
		if _, err := exec.LookPath("xclip"); err == nil {
			return cmdBoard{types: []string{"xclip", "-selection", "clipboard", "-o", "-t", "TARGETS"}, text: []string{"xclip", "-selection", "clipboard", "-o", "-t", "UTF8_STRING"}}, false, true
		}
	}
	return nil, false, false
}

func (cmdBoard) Seq() (uint64, bool) { return 0, false }

// Password managers on Linux (KeePassXC, KDE) mark their copies so.
var skipTypes = []string{"x-kde-passwordManagerHint", "application/x-nspasteboard-concealed-type", "x-kde-passwordmanagerhint"}

func (b cmdBoard) Read() (string, bool) {
	types := run(b.types, 64<<10)
	for _, t := range skipTypes {
		if strings.Contains(strings.ToLower(types), strings.ToLower(t)) {
			return "", true
		}
	}
	if types != "" && !strings.Contains(types, "text") && !strings.Contains(types, "STRING") {
		return "", false // an image, files…
	}
	return run(b.text, Max+1), false
}

func run(argv []string, max int64) string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return ""
	}
	if cmd.Start() != nil {
		return ""
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, io.LimitReader(out, max))
	_, _ = io.Copy(io.Discard, out)
	if cmd.Wait() != nil {
		return ""
	}
	return buf.String()
}
