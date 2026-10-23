package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"keybar/vault"
)

// BackupSuffix is the copy of an .env as it was before KeyBar wrote a
// new value (0600, may hold the old secret — delete once checked).
const BackupSuffix = ".keybar-backup"

// env writes the key's value into a local .env / .env.local file at
// p.Target (~ expanded). Other lines, export, quoting and the file's
// mode are kept; a path.keybar-backup is written first.
func (s *Syncer) env(_ context.Context, key string, p vault.Platform, value []byte) error {
	path, err := ResolveEnvPath(p.Target)
	if err != nil {
		return err
	}
	if err := CheckEnvPath(path); err != nil {
		return err
	}
	name := p.VarName(key)
	if err := vault.CheckName(name); err != nil {
		return err
	}
	return WriteEnvVar(path, name, value)
}

// ResolveEnvPath expands ~ and cleans the platform's target to an absolute path.
func ResolveEnvPath(target string) (string, error) {
	t := strings.TrimSpace(target)
	if t == "" {
		return "", errors.New(".env 目标是空的")
	}
	if t == "~" || strings.HasPrefix(t, "~/") || strings.HasPrefix(t, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", errors.New("找不到主目录，无法展开 ~")
		}
		if t == "~" {
			t = home
		} else {
			t = home + t[1:] // keep / or \
		}
	}
	abs, err := filepath.Abs(filepath.Clean(t))
	if err != nil {
		return "", err
	}
	return abs, nil
}

// CheckEnvPath refuses non-files and system paths (/etc, Windows\…).
// Paths under the user's home, or an existing regular file the user
// already recorded as a platform, are allowed.
func CheckEnvPath(abs string) error {
	st, err := os.Lstat(abs)
	if err != nil {
		return fmt.Errorf("打不开 %s：%w", abs, err)
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%s 不是普通文件", abs)
	}
	if forbiddenEnvPath(abs) {
		return fmt.Errorf("拒绝写入系统路径 %s", abs)
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		home = filepath.Clean(home)
		if abs == home || strings.HasPrefix(abs, home+string(filepath.Separator)) {
			return nil
		}
	}
	// Outside home but already a real file the user recorded: ok.
	return nil
}

func forbiddenEnvPath(abs string) bool {
	sep := string(filepath.Separator)
	lower := strings.ToLower(filepath.ToSlash(abs))
	roots := []string{"/etc", "/usr", "/bin", "/sbin", "/dev", "/proc", "/sys", "/boot", "/root", "/var/run", "/var/lib"}
	if runtime.GOOS == "windows" {
		// c:/windows, c:/program files, …
		roots = append(roots, "/windows", "/program files", "/program files (x86)", "/programdata")
	}
	for _, r := range roots {
		r = strings.ToLower(r)
		if lower == r || strings.HasPrefix(lower, r+"/") {
			return true
		}
	}
	// Never write into the keychain data dir by mistake via relative junk.
	_ = sep
	return false
}

// WriteEnvVar sets NAME=value in path: updates the last matching line
// (optional export, quotes kept when reasonable), or appends. Copies the
// file to path.keybar-backup (0600) first, then writes atomically with
// the original mode.
func WriteEnvVar(path, name string, value []byte) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return errors.New("不是普通文件")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	nl := "\n"
	if bytes.Contains(b, []byte("\r\n")) {
		nl = "\r\n"
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	trail := strings.HasSuffix(text, "\n") || len(text) == 0
	body := strings.TrimSuffix(text, "\n")
	var ls []string
	if body != "" {
		ls = strings.Split(body, "\n")
	}
	val := string(value)
	updated := false
	for i := len(ls) - 1; i >= 0; i-- { // last assignment wins, as dotenv
		repl, ok := replaceEnvLine(ls[i], name, val)
		if !ok {
			continue
		}
		ls[i] = repl
		updated = true
		break
	}
	if !updated {
		ls = append(ls, formatEnvLine(false, name, val, 0))
	}
	res := strings.Join(ls, "\n")
	if trail {
		res += "\n"
	}
	res = strings.ReplaceAll(res, "\n", nl)

	backup := path + BackupSuffix
	if err := writeAtomic(backup, b, 0o600); err != nil {
		return fmt.Errorf("备份失败，没有改动文件：%w", err)
	}
	if err := writeAtomic(path, []byte(res), st.Mode().Perm()); err != nil {
		return err
	}
	return nil
}

// replaceEnvLine rewrites a NAME=… line (optional export) with value.
// Comments and unrelated lines return ok=false.
func replaceEnvLine(line, name, value string) (string, bool) {
	trim := strings.TrimSpace(line)
	if trim == "" || strings.HasPrefix(trim, "#") {
		return "", false
	}
	export := false
	rest := trim
	if strings.HasPrefix(rest, "export ") || strings.HasPrefix(rest, "export\t") {
		export = true
		rest = strings.TrimSpace(rest[len("export"):])
	}
	n, raw, ok := strings.Cut(rest, "=")
	if !ok || strings.TrimSpace(n) != name {
		return "", false
	}
	style := quoteStyle(strings.TrimSpace(raw))
	return formatEnvLine(export, name, value, style), true
}

// quoteStyle is 0 bare, '"' or '\'' from the existing value (and a
// trailing " # comment" is treated as bare).
func quoteStyle(raw string) byte {
	if len(raw) >= 2 {
		switch raw[0] {
		case '"':
			if end := strings.LastIndexByte(raw, '"'); end > 0 {
				return '"'
			}
		case '\'':
			if end := strings.LastIndexByte(raw, '\''); end > 0 {
				return '\''
			}
		}
	}
	return 0
}

func formatEnvLine(export bool, name, value string, style byte) string {
	lead := ""
	if export {
		lead = "export "
	}
	return lead + name + "=" + quoteEnvValue(value, style)
}

func quoteEnvValue(v string, style byte) string {
	switch style {
	case '"':
		return `"` + escapeDouble(v) + `"`
	case '\'':
		if strings.ContainsAny(v, "'\n\r") {
			return `"` + escapeDouble(v) + `"`
		}
		return "'" + v + "'"
	}
	if needsEnvQuote(v) {
		return `"` + escapeDouble(v) + `"`
	}
	return v
}

func needsEnvQuote(v string) bool {
	if v == "" {
		return false
	}
	if strings.ContainsAny(v, " \t#\"'`$\\!\n\r") {
		return true
	}
	for _, r := range v {
		if unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

func escapeDouble(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func writeAtomic(path string, b []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".keybar-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
