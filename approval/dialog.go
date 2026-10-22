package approval

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Dialog asks with a system dialog, where KeyBar does not run: osascript
// on macOS, zenity or kdialog on Linux, PowerShell on Windows. Allow for
// lasts Minutes.
type Dialog struct {
	Minutes int
	// run runs a dialog program, for tests: its output and exit code.
	run func(ctx context.Context, name string, args ...string) (string, int, error)
}

func (d Dialog) minutes() int {
	if d.Minutes <= 0 {
		return 15
	}
	return d.Minutes
}

func defaultRun(ctx context.Context, name string, args ...string) (string, int, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), ee.ExitCode(), nil
	}
	if err != nil {
		return "", -1, err
	}
	return string(out), 0, nil
}

// ErrNoDialog is a system without a dialog program.
var ErrNoDialog = errors.New("没有可用的系统对话框（Linux 需要 zenity 或 kdialog）")

// Approve shows the dialog: 允许一次, 允许 N 分钟, 拒绝 (the default).
func (d Dialog) Approve(r Request) (Decision, error) {
	run := d.run
	if run == nil {
		run = defaultRun
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	once, timed, deny := "允许一次", fmt.Sprintf("允许 %d 分钟", d.minutes()), "拒绝"
	msg := r.Summary()
	if r.Cwd != "" {
		msg += "\n目录：" + r.Cwd
	}
	if r.Reason != "" {
		msg += "\n理由：" + r.Reason
	}
	msg += "\n\n（KeyBar 没有在运行，所以用系统对话框询问。）"
	pick := func(answer string) (Decision, error) {
		switch strings.TrimSpace(answer) {
		case once:
			return Decision{Allow: true}, nil
		case timed:
			return Decision{Allow: true, For: time.Duration(d.minutes()) * time.Minute}, nil
		}
		return Decision{Why: "denied"}, nil
	}
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`display dialog %s with title "KeyBar" buttons {%s, %s, %s} default button %s cancel button %s with icon caution giving up after %d
if gave up of result then return "timeout"
return button returned of result`, quote(msg), quote(deny), quote(timed), quote(once), quote(deny), quote(deny), int(Timeout.Seconds()))
		out, code, err := run(ctx, "osascript", "-e", script)
		if err != nil {
			return Decision{}, err
		}
		if code != 0 || strings.TrimSpace(out) == "timeout" {
			if strings.TrimSpace(out) == "timeout" {
				return Decision{Why: "timeout"}, nil
			}
			return Decision{Why: "denied"}, nil
		}
		return pick(out)
	case "windows":
		ps := fmt.Sprintf(`Add-Type -AssemblyName PresentationFramework; $r = [System.Windows.MessageBox]::Show(%s, 'KeyBar', 'YesNoCancel', 'Warning', 'Cancel'); Write-Output $r`,
			psQuote(msg+"\n\n是：允许一次    否："+timed+"    取消：拒绝"))
		out, _, err := run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
		if err != nil {
			return Decision{}, err
		}
		switch strings.TrimSpace(out) {
		case "Yes":
			return pick(once)
		case "No":
			return pick(timed)
		}
		return Decision{Why: "denied"}, nil
	default:
		if _, err := exec.LookPath("zenity"); err == nil || d.run != nil {
			out, code, err := run(ctx, "zenity", "--question", "--title=KeyBar", "--icon-name=dialog-warning", "--no-wrap",
				"--text="+msg, "--ok-label="+once, "--cancel-label="+deny, "--extra-button="+timed, fmt.Sprintf("--timeout=%d", int(Timeout.Seconds())))
			if err != nil {
				return Decision{}, err
			}
			switch {
			case code == 0:
				return pick(once)
			case strings.TrimSpace(out) == timed:
				return pick(timed)
			case code == 5:
				return Decision{Why: "timeout"}, nil
			}
			return Decision{Why: "denied"}, nil
		}
		if _, err := exec.LookPath("kdialog"); err == nil {
			_, code, err := run(ctx, "kdialog", "--title", "KeyBar", "--warningyesnocancel", msg, "--yes-label", once, "--no-label", timed, "--cancel-label", deny)
			if err != nil {
				return Decision{}, err
			}
			switch code {
			case 0:
				return pick(once)
			case 1:
				return pick(timed)
			}
			return Decision{Why: "denied"}, nil
		}
		return Decision{}, ErrNoDialog
	}
}

// quote is an AppleScript string.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// psQuote is a PowerShell single-quoted string.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
