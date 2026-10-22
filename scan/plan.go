package scan

import (
	"keybar/importer"
	"keybar/vault"
)

// Group is a file found and what importing it would do; the likely
// secrets are checked, the rest left out until checked.
type Group struct {
	Found Found
	Plan  *importer.Plan
}

// Plans are the import plans of a scan. A name found in two files: the
// same value is one key (the second Same); another is imported from the
// later file as NAME_PROJECT unless chosen otherwise.
func Plans(v *vault.Vault, found []Found) ([]Group, error) {
	pend, from := importer.Pending{}, map[string]string{}
	var out []Group
	for _, f := range found {
		p, err := importer.FromVars(v, f.Path, f.Project, "", f.Vars)
		if err != nil {
			continue // a name KeyBar cannot keep: not this file
		}
		for _, it := range p.Items {
			it.Skip = !it.Likely || it.State == importer.Empty
		}
		p.Against(pend, from, v)
		out = append(out, Group{Found: f, Plan: p})
	}
	return out, nil
}

// Unmanaged counts the likely secrets of a plan that KeyBar does not
// have yet (new, or with another value).
func Unmanaged(p *importer.Plan) int {
	n := 0
	for _, it := range p.Items {
		if it.Likely && (it.State == importer.New || it.State == importer.Conflict) {
			n++
		}
	}
	return n
}

// Checked counts the items to import.
func Checked(gs []Group) (files, keys int) {
	for _, g := range gs {
		k := 0
		for _, it := range g.Plan.Items {
			if !it.Skip && it.State != importer.Empty {
				k++
			}
		}
		if k > 0 {
			files++
		}
		keys += k
	}
	return
}
