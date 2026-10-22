package agents

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// indentOf is the file's indentation: its first indented line's.
func indentOf(b []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		line := sc.Text()
		if trimmed := strings.TrimLeft(line, " \t"); trimmed != line && trimmed != "" {
			return line[:len(line)-len(trimmed)]
		}
	}
	return "  "
}

// serverOf reads a server entry: {"command": "x", "args": [...]}, or
// opencode's {"command": ["x", ...]}.
func serverOf(v any) (cmd string, args []string, ok bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return "", nil, false
	}
	strs := func(v any) []string {
		var out []string
		l, _ := v.([]any)
		for _, x := range l {
			s, _ := x.(string)
			out = append(out, s)
		}
		return out
	}
	switch c := m["command"].(type) {
	case string:
		return c, strs(m["args"]), true
	case []any:
		l := strs(c)
		if len(l) > 0 {
			return l[0], l[1:], true
		}
	}
	return "", nil, true
}

func jsonServer(b []byte, a Agent) (cmd string, args []string, found bool, err error) {
	doc, err := decodeJSONC(string(b))
	if err != nil {
		return "", nil, false, fmt.Errorf("无法解析：%w", err)
	}
	cur := doc
	for _, k := range append(slices.Clone(a.Key), ServerName) {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", nil, false, nil
		}
		if cur, ok = m[k]; !ok {
			return "", nil, false, nil
		}
	}
	cmd, args, _ = serverOf(cur)
	return cmd, args, true, nil
}

func setJSONServer(old []byte, a Agent, exe string, args []string, add bool) ([]byte, error) {
	text := string(old)
	blank := strings.TrimSpace(text) == ""
	if blank {
		if !add {
			return old, nil
		}
		text = "{}"
	}
	before, err := decodeJSONC(text)
	if err != nil {
		return nil, fmt.Errorf("无法解析（%v）", err)
	}
	if _, ok := before.(map[string]any); !ok {
		return nil, errors.New("不是 JSON 对象")
	}
	unit := indentOf(old)
	entry := append(slices.Clone(a.Key), ServerName)
	want := before
	if add {
		fields := a.Entry(exe, args)
		_, _, found, _ := jsonServer([]byte(text), a)
		if found {
			// Update what is there, keeping what the user added (env…).
			for _, f := range fields {
				p := append(slices.Clone(entry), f.Key)
				if text, err = setPath(text, p, f.Val, unit); err != nil {
					return nil, fmt.Errorf("无法修改（%v）", err)
				}
				want = goSet(want, p, f.Val)
			}
		} else {
			if text, err = setPath(text, entry, ordered(fields), unit); err != nil {
				return nil, fmt.Errorf("无法修改（%v）", err)
			}
			want = goSet(want, entry, ordered(fields))
		}
	} else {
		if text, err = removePath(text, entry); err != nil {
			return nil, fmt.Errorf("无法修改（%v）", err)
		}
		want = goDel(want, entry)
	}
	if blank {
		text += "\n"
	}
	after, err := decodeJSONC(text)
	if err != nil || !sameJSON(after, want) {
		return nil, errors.New("改完后内容对不上，请手动修改")
	}
	return []byte(text), nil
}

// TOML settings (Codex): a [mcp_servers.keybar] table, edited as text so
// that the rest of the file, comments included, stays as it was.

var (
	headerRE = regexp.MustCompile(`^\s*\[`)
	ourRE    = regexp.MustCompile(`^\s*\[\s*mcp_servers\s*\.\s*(?:keybar|"keybar"|'keybar')\s*(\.[^\]]*)?\]\s*(?:#.*)?$`)
	setRE    = regexp.MustCompile(`^\s*(command|args)\s*=`)
)

type codexServer struct {
	MCPServers map[string]struct {
		Command string   `toml:"command"`
		Args    []string `toml:"args"`
	} `toml:"mcp_servers"`
}

func tomlServer(b []byte) (cmd string, args []string, found bool, err error) {
	var c codexServer
	if _, err := toml.Decode(string(b), &c); err != nil {
		return "", nil, false, fmt.Errorf("无法解析：%w", err)
	}
	s, ok := c.MCPServers[ServerName]
	return s.Command, s.Args, ok, nil
}

func tomlString(s string) string {
	b, _ := json.Marshal(s) // a TOML basic string is a JSON string
	return string(b)
}

func setTOMLServer(old []byte, exe string, args []string, add bool) ([]byte, error) {
	if _, _, _, err := tomlServer(old); err != nil {
		return nil, err
	}
	text := string(old)
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var quoted []string
	for _, a := range args {
		quoted = append(quoted, tomlString(a))
	}
	ours := []string{"command = " + tomlString(exe), "args = [" + strings.Join(quoted, ", ") + "]"}
	var out []string
	inserted := false
	for i := 0; i < len(lines); i++ {
		m := ourRE.FindStringSubmatch(lines[i])
		if m == nil {
			out = append(out, lines[i])
			continue
		}
		sub := m[1] != ""
		if !add || !sub {
			if add && !inserted {
				out = append(out, lines[i])
				out = append(out, ours...)
				inserted = true
			}
		} else {
			out = append(out, lines[i]) // keep [mcp_servers.keybar.env] and the like
		}
		// The table's body, up to the next header.
		for i+1 < len(lines) && !headerRE.MatchString(lines[i+1]) {
			i++
			line := lines[i]
			if !add {
				continue
			}
			if !sub && setRE.MatchString(line) {
				// command / args: ours replace them, arrays over several lines too.
				depth := strings.Count(line, "[") - strings.Count(line, "]")
				for depth > 0 && i+1 < len(lines) {
					i++
					depth += strings.Count(lines[i], "[") - strings.Count(lines[i], "]")
				}
				continue
			}
			out = append(out, line)
		}
	}
	if add && !inserted {
		for len(out) > 0 && out[len(out)-1] == "" {
			out = out[:len(out)-1]
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, "[mcp_servers."+ServerName+"]")
		out = append(out, ours...)
		out = append(out, "")
	}
	if !add {
		// Blank lines left where the table was.
		var clean []string
		for i, l := range out {
			if l == "" && i > 0 && out[i-1] == "" {
				continue
			}
			clean = append(clean, l)
		}
		out = clean
	}
	res := []byte(strings.Join(out, nl))
	cmd, gotArgs, found, err := tomlServer(res)
	switch {
	case err != nil:
		return nil, fmt.Errorf("改完后无法解析（%v），请手动修改", err)
	case add && (!found || cmd != exe || !slices.Equal(gotArgs, args)):
		return nil, errors.New("keybar 的写法不常见（比如写在 [mcp_servers] 里），请手动修改")
	case !add && found:
		return nil, errors.New("keybar 的写法不常见，请手动删除")
	}
	return res, nil
}
