// Package scan finds the keys written out in plain text on this machine:
// projects' .env files and the shell's startup files, so that they can
// be brought into KeyBar, and watches for new ones.
package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"keybar/project"
)

// Kind of a file found.
type Kind int

const (
	DotEnv Kind = iota
	ShellRC
)

// Found is a file with variables in it.
type Found struct {
	Path string
	Kind Kind
	// Project: the folder's name for a .env, the shell for an rc file.
	Project string
	Vars    []project.Var
}

// Options of a scan.
type Options struct {
	Home string
	// Roots to look under; none: DefaultRoots.
	Roots []string
	// Depth: how many folders down under a root (home: HomeDepth).
	Depth, HomeDepth int
	// NoRC leaves out the shell's files.
	NoRC bool
	// Ignored files are passed over.
	Ignored []string
	// Limit on the entries looked at (a runaway home folder).
	Limit int
}

// DefaultRoots are the usual places for code, and home itself (shallow).
func DefaultRoots(home string) []string {
	var out []string
	for _, r := range []string{"code", "projects", "dev", "src", "workspace", "Documents/GitHub"} {
		out = append(out, filepath.Join(home, filepath.FromSlash(r)))
	}
	return append(out, home)
}

// RCFiles are the shell startup files looked at for exports.
func RCFiles(home string) []string {
	var out []string
	for _, r := range []string{".zshrc", ".zshenv", ".zprofile", ".bashrc", ".bash_profile", ".profile", ".config/fish/config.fish"} {
		out = append(out, filepath.Join(home, filepath.FromSlash(r)))
	}
	return out
}

// skipDirs are never gone into.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "build": true, "dist": true, "target": true, "out": true,
	"Library": true, "Applications": true, "Pictures": true, "Movies": true, "Music": true, "Downloads": true,
	"Pods": true, "DerivedData": true, "__pycache__": true, "venv": true, "env": true, "site-packages": true,
	"coverage": true, "bower_components": true, "jspm_packages": true, "go": true, "snap": true,
	"AppData": true, "OneDrive": true, "iCloud Drive": true,
}

// EnvFile tells whether the name is a .env with values: .env, .env.local,
// .env.production …, not .env.example / .sample / .template.
func EnvFile(name string) bool {
	if name == ".env" {
		return true
	}
	if !strings.HasPrefix(name, ".env.") {
		return false
	}
	rest := strings.ToLower(strings.TrimPrefix(name, ".env."))
	for _, w := range []string{"example", "sample", "template", "tmpl", "tpl", "dist", "defaults", "default", "schema", "vault", "keys", "bak", "backup", "keybar-backup", "swp", "orig"} {
		if rest == w || strings.HasSuffix(rest, "."+w) || strings.HasPrefix(rest, w+".") {
			return false
		}
	}
	return !strings.ContainsAny(rest, "~/\\")
}

// EnvFiles lists the .env files under the roots.
func EnvFiles(o Options) []string {
	roots := o.Roots
	if len(roots) == 0 {
		roots = DefaultRoots(o.Home)
	}
	depth, homeDepth, limit := o.Depth, o.HomeDepth, o.Limit
	if depth == 0 {
		depth = 5
	}
	if homeDepth == 0 {
		homeDepth = 1
	}
	if limit == 0 {
		limit = 200000
	}
	seen := map[string]bool{}
	var out []string
	n := 0
	for _, root := range roots {
		root = expand(root, o.Home)
		max := depth
		if filepath.Clean(root) == filepath.Clean(o.Home) {
			max = homeDepth
		}
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if n++; n > limit {
				return fs.SkipAll
			}
			if d.IsDir() {
				if p == root {
					return nil
				}
				name := d.Name()
				if strings.HasPrefix(name, ".") || skipDirs[name] || levels(root, p) > max {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || !EnvFile(d.Name()) || seen[p] || slices.Contains(o.Ignored, p) {
				return nil
			}
			seen[p] = true
			out = append(out, p)
			return nil
		})
	}
	slices.Sort(out)
	return out
}

func levels(root, p string) int {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return 1 << 20
	}
	return strings.Count(filepath.ToSlash(rel), "/") + 1
}

func expand(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") || filepath.Separator == '\\' && strings.HasPrefix(p, `~\`) { // ~\ too on Windows
		return filepath.Join(home, p[2:])
	}
	return p
}

// Run is the scan: every .env under the roots and the shell's files,
// with the variables that have a value.
func Run(o Options) []Found {
	var out []Found
	for _, p := range EnvFiles(o) {
		if f, ok := ReadEnv(p); ok {
			out = append(out, f)
		}
	}
	if !o.NoRC {
		for _, p := range RCFiles(o.Home) {
			if slices.Contains(o.Ignored, p) {
				continue
			}
			if f, ok := ReadRC(p); ok {
				out = append(out, f)
			}
		}
	}
	return out
}

const maxFile = 1 << 20

func readSmall(p string) ([]byte, bool) {
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxFile {
		return nil, false
	}
	b, err := os.ReadFile(p)
	return b, err == nil
}

// ReadEnv reads one .env.
func ReadEnv(p string) (Found, bool) {
	b, ok := readSmall(p)
	if !ok {
		return Found{}, false
	}
	vars := valued(project.ParseDotenvLoose(b))
	if len(vars) == 0 {
		return Found{}, false
	}
	return Found{Path: p, Kind: DotEnv, Project: filepath.Base(filepath.Dir(p)), Vars: vars}, true
}

// ReadRC reads the literal exports of a shell file.
func ReadRC(p string) (Found, bool) {
	b, ok := readSmall(p)
	if !ok {
		return Found{}, false
	}
	vars := valued(ParseRC(b, strings.HasSuffix(p, ".fish")))
	if len(vars) == 0 {
		return Found{}, false
	}
	shell := "shell"
	switch {
	case strings.HasSuffix(p, ".fish"):
		shell = "fish"
	case strings.Contains(filepath.Base(p), "zsh") || strings.HasPrefix(filepath.Base(p), ".z"):
		shell = "zsh"
	case strings.Contains(filepath.Base(p), "bash"):
		shell = "bash"
	}
	return Found{Path: p, Kind: ShellRC, Project: shell, Vars: vars}, true
}

func valued(vs []project.Var) []project.Var {
	var out []project.Var
	for _, v := range vs {
		if v.Value != "" {
			out = append(out, v)
		}
	}
	return out
}
