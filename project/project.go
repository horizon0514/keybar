// Package project reads keys.toml, a project's list of the keys it needs:
// environment variable names and the KeyBar keys they get, never values,
// so it is safe to commit. And .env files, to import them.
package project

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// FileName is the project file's name.
const FileName = "keys.toml"

// File is keys.toml:
//
//	# keys.toml: environment variable = KeyBar key. No values: safe to commit.
//	project = "my-app"
//	[env]
//	OPENAI_API_KEY = "OPENAI_API_KEY"
//	DATABASE_URL = "TIDB_PROD_URL"
type File struct {
	Path    string            `toml:"-"`
	Project string            `toml:"project,omitempty"`
	Env     map[string]string `toml:"env"`
}

var envRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Load reads a project file.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f := &File{Path: path}
	if _, err := toml.Decode(string(b), f); err != nil {
		return nil, fmt.Errorf("%s：%w", path, err)
	}
	for env, key := range f.Env {
		if !envRE.MatchString(env) {
			return nil, fmt.Errorf("%s：环境变量名 %q 不合法", path, env)
		}
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("%s：%s 没写对应的密钥名", path, env)
		}
		if looksLikeValue(key) {
			return nil, fmt.Errorf("%s：%s 的值看起来像真实密钥，keys.toml 只能写 KeyBar 里的密钥名", path, env)
		}
	}
	if f.Project == "" {
		f.Project = filepath.Base(filepath.Dir(path))
	}
	return f, nil
}

// looksLikeValue guards against a value pasted where a name belongs.
func looksLikeValue(s string) bool {
	return strings.ContainsAny(s, " /+=:@") || strings.HasPrefix(s, "sk-") || strings.HasPrefix(s, "ghp_") || strings.HasPrefix(s, "github_pat_")
}

// Find looks for keys.toml in dir and its parents.
func Find(dir string) (string, bool) {
	dir, _ = filepath.Abs(dir)
	for {
		p := filepath.Join(dir, FileName)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Keys are the keys it needs, sorted, once each.
func (f *File) Keys() []string {
	var out []string
	for _, k := range f.Env {
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// Write writes a project file for the mapping.
func Write(path, project string, env map[string]string) error {
	var b bytes.Buffer
	b.WriteString("# keys.toml: 环境变量 = KeyBar 里的密钥名。不含任何值，可以提交到仓库。\n")
	b.WriteString("# 运行：keys run -- <命令>\n")
	if project != "" {
		fmt.Fprintf(&b, "project = %q\n", project)
	}
	b.WriteString("\n[env]\n")
	names := make([]string, 0, len(env))
	for k := range env {
		names = append(names, k)
	}
	slices.Sort(names)
	for _, k := range names {
		fmt.Fprintf(&b, "%s = %q\n", k, env[k])
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

// Var is one variable of a .env file.
type Var struct {
	Name, Value string
	Line        int
}

// ParseDotenv reads a .env file: NAME=value lines, with export, quotes and
// comments as dotenv takes them.
func ParseDotenv(b []byte) ([]Var, error) { return parseDotenv(b, true) }

// ParseDotenvLoose is ParseDotenv passing over the lines it cannot read,
// for files found by a scan.
func ParseDotenvLoose(b []byte) []Var {
	out, _ := parseDotenv(b, false)
	return out
}

func parseDotenv(b []byte, strict bool) ([]Var, error) {
	var out []Var
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		name, val, ok := strings.Cut(line, "=")
		if !ok {
			if !strict {
				continue
			}
			return nil, fmt.Errorf("第 %d 行不是 NAME=value", n)
		}
		name = strings.TrimSpace(name)
		if !envRE.MatchString(name) {
			if !strict {
				continue
			}
			return nil, fmt.Errorf("第 %d 行：变量名 %q 不合法", n, name)
		}
		val = strings.TrimSpace(val)
		switch {
		case len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"':
			val = strings.NewReplacer(`\n`, "\n", `\"`, `"`, `\\`, `\`).Replace(val[1 : len(val)-1])
		case len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'':
			val = val[1 : len(val)-1]
		default:
			if i := strings.Index(val, " #"); i >= 0 {
				val = strings.TrimSpace(val[:i])
			}
		}
		out = append(out, Var{Name: name, Value: val, Line: n})
	}
	return out, sc.Err()
}

// Shred overwrites the file with zeros, syncs, and removes it: as much as
// a program can do; SSDs and snapshots (Time Machine, APFS) may still keep
// the old blocks, so rotate what it held if it leaked.
func Shred(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return errors.New("不是普通文件")
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	zero := make([]byte, 4096)
	for left := st.Size(); left > 0; left -= int64(len(zero)) {
		n := min(left, int64(len(zero)))
		if _, err := f.Write(zero[:n]); err != nil {
			f.Close()
			return err
		}
	}
	_ = f.Sync()
	f.Close()
	return os.Remove(path)
}

// skipDirs are not searched for .env files.
var skipDirs = map[string]bool{"node_modules": true, ".git": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, "dist": true, "build": true, ".next": true, "target": true, "Library": true, ".cache": true, ".Trash": true}

// IsDotenv tells whether a file name is a .env file holding values: .env,
// .env.local, .env.production…, not .env.example and the like.
func IsDotenv(name string) bool {
	if name != ".env" && !strings.HasPrefix(name, ".env.") {
		return false
	}
	for _, s := range []string{"example", "sample", "template", "dist", "defaults"} {
		if strings.HasSuffix(name, "."+s) {
			return false
		}
	}
	return true
}

// FindDotenvs lists the .env files under root, maxDepth folders deep.
func FindDotenvs(root string, maxDepth int) []string {
	var out []string
	root = filepath.Clean(root)
	base := strings.Count(root, string(filepath.Separator))
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != root && (skipDirs[d.Name()] || strings.Count(p, string(filepath.Separator))-base >= maxDepth) {
				return filepath.SkipDir
			}
			return nil
		}
		if IsDotenv(d.Name()) && d.Type().IsRegular() {
			out = append(out, p)
		}
		return nil
	})
	return out
}
