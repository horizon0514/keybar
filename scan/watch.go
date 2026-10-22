package scan

import (
	"os"
	"slices"
	"time"
)

// Watcher notices .env files that are new or changed under the roots, by
// looking now and then (no file events: cheap and the same everywhere).
// A change is told once the file has stayed the same for Settle, so that
// an editor saving in steps is one change.
type Watcher struct {
	Opts   Options
	Settle time.Duration
	Now    func() time.Time

	started bool
	known   map[string]time.Time
	moving  map[string]change
}

type change struct {
	mod  time.Time
	seen time.Time
}

// NewWatcher is a watcher over the options' roots (no rc files).
func NewWatcher(o Options) *Watcher {
	o.NoRC = true
	return &Watcher{Opts: o, Settle: 3 * time.Second, Now: time.Now}
}

// Tick looks once: the first time it only learns what is there; after,
// it returns the files new or changed that have settled.
func (w *Watcher) Tick() []string {
	now := w.Now()
	files := EnvFiles(w.Opts)
	if !w.started {
		w.started = true
		w.known, w.moving = map[string]time.Time{}, map[string]change{}
		for _, p := range files {
			if st, err := os.Stat(p); err == nil {
				w.known[p] = st.ModTime()
			}
		}
		return nil
	}
	var ready []string
	for _, p := range files {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		mod := st.ModTime()
		if old, ok := w.known[p]; ok && old.Equal(mod) {
			delete(w.moving, p)
			continue
		}
		c, ok := w.moving[p]
		if !ok || !c.mod.Equal(mod) {
			w.moving[p] = change{mod: mod, seen: now}
			continue
		}
		if now.Sub(c.seen) >= w.Settle {
			delete(w.moving, p)
			w.known[p] = mod
			ready = append(ready, p)
		}
	}
	for p := range w.known {
		if !slices.Contains(files, p) {
			delete(w.known, p)
		}
	}
	return ready
}

// Ignore stops telling about a file.
func (w *Watcher) Ignore(p string) {
	if !slices.Contains(w.Opts.Ignored, p) {
		w.Opts.Ignored = append(w.Opts.Ignored, p)
	}
	delete(w.moving, p)
}
