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

// JSON settings (Claude Code, Cursor): {"mcpServers": {"keybar": {…}}},
// edited keeping every other member, in its order.

type member struct {
	key string
	val json.RawMessage
}

func parseObject(b []byte) ([]member, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	if t != json.Delim('{') {
		return nil, errors.New("不是 JSON 对象")
	}
	var out []member
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		k, _ := t.(string)
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, member{k, v})
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err == nil {
		return nil, errors.New("JSON 后面还有内容")
	}
	return out, nil
}

func encodeObject(ms []member) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(m.key)
		b.Write(k)
		b.WriteByte(':')
		var c bytes.Buffer
		if json.Compact(&c, m.val) == nil {
			b.Write(c.Bytes())
		} else {
			b.Write(m.val)
		}
	}
	b.WriteByte('}')
	return b.Bytes()
}

func find(ms []member, k string) int {
	return slices.IndexFunc(ms, func(m member) bool { return m.key == k })
}

func set(ms []member, k string, v json.RawMessage) []member {
	if i := find(ms, k); i >= 0 {
		ms[i].val = v
		return ms
	}
	return append(ms, member{k, v})
}

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

func jsonServer(b []byte) (cmd string, args []string, found bool, err error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return "", nil, false, nil
	}
	top, err := parseObject(b)
	if err != nil {
		return "", nil, false, fmt.Errorf("无法解析：%w", err)
	}
	i := find(top, "mcpServers")
	if i < 0 || string(top[i].val) == "null" {
		return "", nil, false, nil
	}
	var servers map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(top[i].val, &servers); err != nil {
		return "", nil, false, fmt.Errorf("mcpServers 无法解析：%w", err)
	}
	s, ok := servers[ServerName]
	return s.Command, s.Args, ok, nil
}

func setJSONServer(old []byte, exe string, args []string, typed, add bool) ([]byte, error) {
	var top []member
	if len(bytes.TrimSpace(old)) > 0 {
		var err error
		if top, err = parseObject(old); err != nil {
			return nil, fmt.Errorf("无法解析（%v）", err)
		}
	}
	var servers []member
	if i := find(top, "mcpServers"); i >= 0 && string(top[i].val) != "null" {
		var err error
		if servers, err = parseObject(top[i].val); err != nil {
			return nil, fmt.Errorf("mcpServers 无法解析（%v）", err)
		}
	} else if !add {
		return old, nil
	}
	if add {
		var entry []member
		if i := find(servers, ServerName); i >= 0 {
			entry, _ = parseObject(servers[i].val) // keep what the user added (env…)
		}
		if typed && find(entry, "type") < 0 {
			entry = append([]member{{"type", json.RawMessage(`"stdio"`)}}, entry...)
		}
		c, _ := json.Marshal(exe)
		a, _ := json.Marshal(args)
		entry = set(set(entry, "command", c), "args", a)
		servers = set(servers, ServerName, encodeObject(entry))
	} else {
		i := find(servers, ServerName)
		if i < 0 {
			return old, nil
		}
		servers = slices.Delete(servers, i, i+1)
	}
	top = set(top, "mcpServers", encodeObject(servers))
	var out bytes.Buffer
	if err := json.Indent(&out, encodeObject(top), "", indentOf(old)); err != nil {
		return nil, err
	}
	if len(old) == 0 || bytes.HasSuffix(old, []byte("\n")) {
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
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
