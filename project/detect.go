package project

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"slices"

	"keybar/secret"
	"strings"
)

// NameFiles are the files whose variable names tell what a project needs:
// templates first, then the files with values (only their names are read).
var NameFiles = []string{".env.example", ".env.sample", ".env.template", ".env", ".env.local"}

// Dirs are dir and its parents up to the git repository's root (the
// folder with .git); dir alone outside a repository.
// Root is the project's folder for dir: the git root, else dir.
func Root(dir string) string {
	ds := Dirs(dir)
	return ds[len(ds)-1]
}

func Dirs(dir string) []string {
	dir, _ = filepath.Abs(dir)
	var out []string
	for d := dir; ; {
		out = append(out, d)
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return out
		}
		parent := filepath.Dir(d)
		if parent == d {
			return []string{dir}
		}
		d = parent
	}
}

// Names reads a .env-style file's variable names, never its values;
// lines that are not NAME=… are passed over.
func Names(b []byte) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		name, _, ok := strings.Cut(line, "=")
		if name = strings.TrimSpace(name); ok && envRE.MatchString(name) && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// Detect lists the variable names the project at dir declares in its
// NameFiles, from dir up to the repository's root, and the files read.
func Detect(dir string) (names, files []string) {
	for _, d := range Dirs(dir) {
		for _, f := range NameFiles {
			p := filepath.Join(d, f)
			st, err := os.Stat(p)
			if err != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			files = append(files, p)
			for _, n := range Names(b) {
				if !slices.Contains(names, n) {
					names = append(names, n)
				}
			}
		}
	}
	return names, files
}

// Resolution is what a command run in a folder gets: environment variable
// → KeyBar key.
type Resolution struct {
	Env     map[string]string
	Toml    string   // the keys.toml used, if any
	Files   []string // the .env-style files the names came from
	Missing []string // names found there that KeyBar does not hold: skipped
	// TomlMissing are keys keys.toml names that KeyBar does not hold.
	TomlMissing []string
}

// Keys are the keys, sorted, once each.
func (r *Resolution) Keys() []string {
	var out []string
	for _, k := range r.Env {
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// Resolve works out the keys for dir with no configuration: each
// variable named in the project's .env.example / .env.sample / .env that
// KeyBar already holds (same name) is injected. Names KeyBar lacks are
// only offered to fill when they look like secrets (*KEY* / *TOKEN* /
// *SECRET* / *PASSWORD*…) — PORT and the like are skipped (pass -e).
// A keys.toml, if there is one, maps any names and wins.
func Resolve(dir string, has func(string) bool) (*Resolution, error) {
	r := &Resolution{Env: map[string]string{}}
	if p, ok := Find(dir); ok {
		f, err := Load(p)
		if err != nil {
			return nil, err
		}
		r.Toml = p
		for v, k := range f.Env {
			r.Env[v] = k
			if !has(k) && !slices.Contains(r.TomlMissing, k) {
				r.TomlMissing = append(r.TomlMissing, k)
			}
		}
		slices.Sort(r.TomlMissing)
	}
	names, files := Detect(dir)
	r.Files = files
	for _, n := range names {
		if _, ok := r.Env[n]; ok {
			continue
		}
		if has(n) {
			r.Env[n] = n
			continue
		}
		if secret.SecretName(n) {
			r.Missing = append(r.Missing, n)
		}
	}
	slices.Sort(r.Missing)
	return r, nil
}
