package scan

import (
	"fmt"
	"slices"
	"strings"

	"keybar/importer"
	"keybar/vault"
)

// Outcome of importing one file of a scan.
type Outcome struct {
	Path     string
	Result   importer.Result
	Mapping  string // the keys.toml written for renamed names
	Backup   string // the copy made before taking plain text out
	Cleaned  int    // lines taken out
	Err      error  // nothing imported
	CleanErr error  // imported, the file left as it was
}

// Import brings a file's checked items into KeyBar (brought up to date
// with what earlier files added), then, when asked, takes the values
// now in KeyBar out of the file, with a backup first.
func Import(v *vault.Vault, g Group, clean bool, mode Mode) Outcome {
	p := g.Plan
	o := Outcome{Path: p.Path}
	if err := p.Recheck(v); err != nil {
		o.Err = err
		return o
	}
	for _, it := range p.Conflicts() {
		if !it.Skip && it.Choice == importer.Rename {
			if err := p.CheckName(v, it, strings.TrimSpace(it.NewName)); err != nil {
				o.Err = fmt.Errorf("%s：%w", it.Var, err)
				return o
			}
		}
	}
	o.Result = p.Apply(v)
	if g.Found.Kind == DotEnv {
		importer.AttachEnvPlatforms(v, p.Path, o.Result)
		o.Mapping, _ = p.WriteMapping(o.Result)
	}
	if !clean || len(o.Result.Env) == 0 {
		return o
	}
	var names []string
	for name := range o.Result.Env { // in KeyBar now: safe to take out
		names = append(names, name)
	}
	lines := LinesOf(g.Found, names)
	if len(lines) == 0 {
		return o
	}
	o.Backup, o.CleanErr = RemovePlaintext(p.Path, g.Found.Kind, lines, mode)
	if o.CleanErr == nil {
		o.Cleaned = len(lines)
	}
	return o
}

// Keys are the names an outcome put in KeyBar or found there.
func (o Outcome) Keys() []string {
	var out []string
	for _, k := range o.Result.Env {
		out = append(out, k)
	}
	out = vault.Clean(out)
	slices.Sort(out)
	return out
}
