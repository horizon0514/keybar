package vault

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Event is one access to keys, in audit.jsonl: who asked for what and what
// was decided; never a value.
type Event struct {
	Time     time.Time `json:"time"`
	Client   string    `json:"client"`             // cli, app, mcp:claude-code…
	Action   string    `json:"action"`             // get, run, reveal, copy, rotate, add, rm, sync, import
	Keys     []string  `json:"keys,omitempty"`     // the keys' names
	Decision string    `json:"decision,omitempty"` // allowed, allowed-once, allowed-15m, denied, timeout, error, ok
	Detail   string    `json:"detail,omitempty"`   // the command run (its program), the platform…
}

const auditFile = "audit.jsonl"

// maxAudit is the log's size before it moves to audit.1.jsonl.
const maxAudit = 4 << 20

var auditMu sync.Mutex

// Log appends e to the access log.
func (v *Vault) Log(e Event) error {
	if e.Time.IsZero() {
		e.Time = v.now()
	}
	return AppendLog(v.Dir, e)
}

// AppendLog appends e to dir's access log.
func AppendLog(dir string, e Event) error {
	auditMu.Lock()
	defer auditMu.Unlock()
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, auditFile)
	if st, err := os.Stat(path); err == nil && st.Size() > maxAudit {
		_ = os.Rename(path, filepath.Join(dir, "audit.1.jsonl"))
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// ReadLog returns the last n events (all, n <= 0), oldest first, those
// matching keep (all, nil).
func ReadLog(dir string, n int, keep func(Event) bool) []Event {
	var out []Event
	for _, name := range []string{"audit.1.jsonl", auditFile} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var e Event
			if json.Unmarshal(sc.Bytes(), &e) == nil && (keep == nil || keep(e)) {
				out = append(out, e)
			}
		}
		f.Close()
	}
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// ForKey keeps the events about the key.
func ForKey(name string) func(Event) bool {
	return func(e Event) bool { return slices.Contains(e.Keys, name) }
}

// DecisionZh is a decision in Chinese.
func DecisionZh(d string) string {
	switch {
	case d == "denied":
		return "已拒绝"
	case d == "timeout":
		return "超时未批准"
	case d == "error":
		return "出错"
	case d == "allowed-once":
		return "允许一次"
	case strings.HasPrefix(d, "allowed-"):
		return "允许 " + strings.TrimSuffix(strings.TrimPrefix(d, "allowed-"), "m") + " 分钟"
	case d == "allowed-grant":
		return "已授权（时限内）"
	case d == "ok", d == "allowed", d == "":
		return "完成"
	}
	return d
}
