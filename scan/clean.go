package scan

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Mode of taking the plain text out of a file.
type Mode int

const (
	// Comment: the line becomes a note with no value (rc files always).
	Comment Mode = iota
	// Names: NAME= stays, the value goes (.env).
	Names
	// Delete: the line goes.
	Delete
)

// ParseMode reads comment / names / delete.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "", "comment":
		return Comment, nil
	case "names":
		return Names, nil
	case "delete":
		return Delete, nil
	}
	return 0, fmt.Errorf("--clean 只能是 comment、names 或 delete，不是 %q", s)
}

// BackupSuffix is added to the name of the copy made before a change.
const BackupSuffix = ".keybar-backup"

// Note is what a line becomes in Comment mode.
func Note(kind Kind, name string) string {
	if kind == ShellRC {
		return "# moved to KeyBar: " + name + "（值在钥匙串里，用 keys run 注入）"
	}
	return "# " + name + " moved to KeyBar（keys run 注入）"
}

// RemovePlaintext takes the values of the given lines (1-based: the
// variables imported) out of the file: first copying it to
// path.keybar-backup (0600, the old one replaced), then writing the new
// text in place with the file's mode kept. An rc file's export is always
// commented out. Returns the backup's path.
func RemovePlaintext(path string, kind Kind, lines map[int]string, mode Mode) (string, error) {
	if len(lines) == 0 {
		return "", errors.New("没有要移除的行")
	}
	st, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("不是普通文件")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if kind == ShellRC {
		mode = Comment
	}
	nl := "\n"
	if bytes.Contains(b, []byte("\r\n")) {
		nl = "\r\n"
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	trail := strings.HasSuffix(text, "\n")
	ls := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	var out []string
	for i, l := range ls {
		name, ok := lines[i+1]
		if !ok {
			out = append(out, l)
			continue
		}
		if !strings.Contains(l, name) {
			return "", fmt.Errorf("第 %d 行已经变了，没有改动文件", i+1)
		}
		switch mode {
		case Comment:
			out = append(out, Note(kind, name))
		case Names:
			lead := ""
			if strings.HasPrefix(strings.TrimSpace(l), "export ") {
				lead = "export "
			}
			out = append(out, lead+name+"=")
		case Delete:
		}
	}
	for n := range lines {
		if n < 1 || n > len(ls) {
			return "", fmt.Errorf("没有第 %d 行", n)
		}
	}
	res := strings.Join(out, "\n")
	if trail && len(out) > 0 {
		res += "\n"
	}
	res = strings.ReplaceAll(res, "\n", nl)

	backup := path + BackupSuffix
	if err := writeAtomic(backup, b, 0o600); err != nil {
		return "", fmt.Errorf("备份失败，没有改动文件：%w", err)
	}
	if err := writeAtomic(path, []byte(res), st.Mode().Perm()); err != nil {
		return backup, err
	}
	return backup, nil
}

func writeAtomic(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".keybar-*")
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

// LinesOf are the lines of the given names in a found file.
func LinesOf(f Found, names []string) map[int]string {
	out := map[int]string{}
	for _, v := range f.Vars {
		if slices.Contains(names, v.Name) {
			out[v.Line] = v.Name
		}
	}
	return out
}
