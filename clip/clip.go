// Package clip reads the system clipboard for the app's key detection:
// a cheap change counter where the system has one, and the text, unless
// a password manager marked it concealed or transient. Nothing here
// keeps or logs what it reads.
package clip

import (
	"crypto/sha256"
	"strings"
)

// Board is a clipboard.
type Board interface {
	// Seq changes whenever the clipboard does; ok false: the system has no
	// counter, so the text is read each time and compared by its hash.
	Seq() (n uint64, ok bool)
	// Read is the clipboard's text; skip: marked as not to be looked at
	// (concealed, transient, a password manager's). Text longer than Max
	// comes back empty: no key is that long.
	Read() (text string, skip bool)
}

// Max is the most read: a key fits many times over.
const Max = 4096

// System is this computer's clipboard; ok false: none to read (Linux
// without wl-paste or xclip). main: call it on the UI thread.
func System() (b Board, main, ok bool) { return system() }

// Watch tells, of each look at a board, whether there is new text to
// look at; only a hash of the last is kept.
type Watch struct {
	B       Board
	started bool
	seq     uint64
	last    [32]byte
}

// Next reads the board if it changed since last time: the text, or ""
// when unchanged, skipped or empty. The first call only learns what is
// there (a key copied before KeyBar started is not offered).
func (w *Watch) Next() string {
	if n, ok := w.B.Seq(); ok {
		if w.started && n == w.seq {
			return ""
		}
		w.seq = n
	}
	text, skip := w.B.Read()
	text = strings.TrimSpace(text)
	h := sha256.Sum256([]byte(text))
	first := !w.started
	w.started = true
	if h == w.last {
		return ""
	}
	w.last = h
	if first || skip || len(text) > Max {
		return ""
	}
	return text
}

// Fake is a board for tests and the demo.
type Fake struct {
	N    uint64
	Text string
	Skip bool
	// NoSeq: like Linux, no counter.
	NoSeq bool
	Reads int
}

func (f *Fake) Seq() (uint64, bool) { return f.N, !f.NoSeq }
func (f *Fake) Read() (string, bool) {
	f.Reads++
	return f.Text, f.Skip
}

// Set changes the fake's text.
func (f *Fake) Set(text string, skip bool) { f.N++; f.Text, f.Skip = text, skip }
