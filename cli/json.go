package cli

import (
	"encoding/json"
	"io"
	"runtime"
	"time"

	"golang.org/x/term"
	"os"

	"keybar/vault"
)

var goos = runtime.GOOS

func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

type jsonKey struct {
	vault.Key
	Status  string    `json:"status"`
	Due     time.Time `json:"due,omitzero"`
	Pending []string  `json:"pending,omitempty"`
}

func writeJSON(w io.Writer, inv *vault.Inventory, keys []vault.Key, now time.Time) error {
	out := []jsonKey{}
	for _, k := range keys {
		st := inv.StatusOf(k, now)
		jk := jsonKey{Key: k, Status: st.State.String(), Due: st.Due}
		for _, p := range k.Pending() {
			jk.Pending = append(jk.Pending, p.ID)
		}
		out = append(out, jk)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// Terminal tells whether stdin is a terminal.
func Terminal() bool { return isTerminal(os.Stdin) }

// Password reads a line from the terminal without echo, prompting on
// stderr.
func Password(prompt string) ([]byte, error) {
	os.Stderr.WriteString(prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	os.Stderr.WriteString("\n")
	return b, err
}
