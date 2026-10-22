// Package redact hides secret values in a program's output: every
// occurrence of a value, as it is and in its common encodings, becomes
// [REDACTED:NAME].
package redact

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/url"
	"sort"
)

// MinLen is the shortest value redacted: shorter ones would hide common
// text, and are no secrets.
const MinLen = 4

type needle struct {
	b    []byte
	repl []byte
}

// Redactor replaces the values it was given.
type Redactor struct {
	needles []needle
	maxLen  int
}

// New makes a redactor for the values, by key name.
func New(values map[string][]byte) *Redactor {
	r := &Redactor{}
	seen := map[string]bool{}
	add := func(b []byte, name string) {
		if len(b) < MinLen || seen[string(b)] {
			return
		}
		seen[string(b)] = true
		r.needles = append(r.needles, needle{b: b, repl: []byte("[REDACTED:" + name + "]")})
		r.maxLen = max(r.maxLen, len(b))
	}
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v := values[n]
		trimmed := bytes.TrimSpace(v)
		add(trimmed, n)
		add(v, n)
		add([]byte(base64.StdEncoding.EncodeToString(trimmed)), n)
		add([]byte(base64.RawStdEncoding.EncodeToString(trimmed)), n)
		add([]byte(base64.URLEncoding.EncodeToString(trimmed)), n)
		add([]byte(base64.RawURLEncoding.EncodeToString(trimmed)), n)
		add([]byte(url.QueryEscape(string(trimmed))), n)
	}
	// The longest first, so that a value inside another's encoding goes
	// with the longer.
	sort.SliceStable(r.needles, func(i, j int) bool { return len(r.needles[i].b) > len(r.needles[j].b) })
	return r
}

// Bytes redacts b.
func (r *Redactor) Bytes(b []byte) []byte {
	if len(r.needles) == 0 {
		return b
	}
	var out bytes.Buffer
	for i := 0; i < len(b); {
		if n, ok := r.match(b[i:]); ok {
			out.Write(n.repl)
			i += len(n.b)
			continue
		}
		out.WriteByte(b[i])
		i++
	}
	return out.Bytes()
}

// String redacts s.
func (r *Redactor) String(s string) string { return string(r.Bytes([]byte(s))) }

func (r *Redactor) match(b []byte) (needle, bool) {
	for _, n := range r.needles {
		if bytes.HasPrefix(b, n.b) {
			return n, true
		}
	}
	return needle{}, false
}

// Writer redacts what is written to w, as it streams: it holds back the
// end of each write that could begin a value, until the next write or
// Close.
func (r *Redactor) Writer(w io.Writer) io.WriteCloser { return &writer{r: r, w: w} }

type writer struct {
	r   *Redactor
	w   io.Writer
	buf []byte
}

func (w *writer) Write(p []byte) (int, error) {
	if len(w.r.needles) == 0 {
		return w.w.Write(p)
	}
	w.buf = append(w.buf, p...)
	// Everything before the last maxLen-1 bytes is safe to redact and
	// send, unless a value starts there and runs on into the tail.
	var out bytes.Buffer
	i := 0
	for i < len(w.buf) {
		rest := w.buf[i:]
		if n, ok := w.r.match(rest); ok {
			out.Write(n.repl)
			i += len(n.b)
			continue
		}
		if len(rest) < w.r.maxLen && w.r.prefixOfAny(rest) {
			break // may be the start of a value: wait for more
		}
		out.WriteByte(w.buf[i])
		i++
	}
	w.buf = append(w.buf[:0], w.buf[i:]...)
	if out.Len() > 0 {
		if _, err := w.w.Write(out.Bytes()); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (r *Redactor) prefixOfAny(b []byte) bool {
	for _, n := range r.needles {
		if len(b) < len(n.b) && bytes.HasPrefix(n.b, b) {
			return true
		}
	}
	return false
}

// Close flushes what was held back.
func (w *writer) Close() error {
	if len(w.buf) == 0 {
		return nil
	}
	_, err := w.w.Write(w.r.Bytes(w.buf))
	w.buf = nil
	return err
}
