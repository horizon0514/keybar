package approval

import (
	"errors"
	"time"
)

// Gate decides an agent's request: a grant of before, else the app, else a
// system dialog; without either, no.
type Gate struct {
	Dir    string
	Grants *Grants
	App    interface {
		Approve(Request) (Decision, error)
	} // Client{Dir} by default
	Dialog interface {
		Approve(Request) (Decision, error)
	} // Dialog{} by default; set NoDialog to go without
	NoDialog bool
	Now      func() time.Time
}

func (g *Gate) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// Ask decides r, waiting for the user if it must.
func (g *Gate) Ask(r Request) Decision {
	if g.Grants == nil {
		g.Grants = &Grants{}
	}
	if g.Grants.Covers(r.Client, r.Keys, g.now()) {
		return Decision{Allow: true, Granted: true}
	}
	app := g.App
	if app == nil {
		app = Client{Dir: g.Dir}
	}
	d, err := app.Approve(r)
	if errors.Is(err, ErrNoApp) && !g.NoDialog {
		dlg := g.Dialog
		if dlg == nil {
			dlg = Dialog{}
		}
		d, err = dlg.Approve(r)
	}
	if err != nil {
		why := "no-approver"
		if !errors.Is(err, ErrNoApp) && !errors.Is(err, ErrNoDialog) {
			why = "error: " + err.Error()
		}
		return Decision{Why: why}
	}
	if d.Allow && d.For > 0 && !d.Granted {
		g.Grants.Add(r.Client, r.Keys, g.now().Add(d.For))
	}
	return d
}
