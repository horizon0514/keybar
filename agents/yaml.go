package agents

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// YAML settings, edited as text so the rest of the file, comments
// included, stays as it was; checked by decoding before and after.

func yamlLines(b []byte) (lines []string, nl string) {
	text := string(b)
	nl = "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.Split(text, "\n"), nl
}

func strList(v any) []string {
	var out []string
	l, _ := v.([]any)
	for _, x := range l {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

func quote(s string) string { return tomlString(s) } // a JSON string is a YAML double-quoted one

func quoteList(l []string) string {
	q := make([]string, len(l))
	for i, s := range l {
		q[i] = quote(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

func indentLen(line string) int { return len(line) - len(strings.TrimLeft(line, " \t")) }

func blankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

// Goose: ~/.config/goose/config.yaml, extensions: {keybar: {type: stdio,
// cmd, args, enabled, …}}.

func gooseDoc(b []byte) (map[string]any, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("无法解析：%w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func gooseServer(b []byte) (cmd string, args []string, found bool, err error) {
	doc, err := gooseDoc(b)
	if err != nil {
		return "", nil, false, err
	}
	ext, _ := doc["extensions"].(map[string]any)
	kb, ok := ext[ServerName].(map[string]any)
	if !ok {
		return "", nil, ext[ServerName] != nil, nil
	}
	cmd, _ = kb["cmd"].(string)
	return cmd, strList(kb["args"]), true, nil
}

var (
	extRE      = regexp.MustCompile(`^extensions:\s*(#.*)?$`)
	extEmptyRE = regexp.MustCompile(`^extensions:\s*(\{\s*\}|null|~)?\s*(#.*)?$`)
)

func setGooseServer(old []byte, exe string, args []string, add bool) ([]byte, error) {
	before, err := gooseDoc(old)
	if err != nil {
		return nil, err
	}
	lines, nl := yamlLines(old)
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	idx := slices.IndexFunc(lines, func(l string) bool { return extEmptyRE.MatchString(l) })
	if idx < 0 && !add {
		return old, nil
	}
	child := "  "
	if idx >= 0 {
		if !extRE.MatchString(lines[idx]) {
			lines[idx] = "extensions:" // {} or null: a block from now on
		}
		for _, l := range lines[idx+1:] {
			if blankOrComment(l) {
				continue
			}
			if n := indentLen(l); n > 0 {
				child = l[:n]
			}
			break
		}
	}
	block := []string{
		child + ServerName + ":",
		child + "  name: " + ServerName,
		child + "  type: stdio",
		child + "  cmd: " + quote(exe),
		child + "  args: " + quoteList(args),
		child + "  enabled: true",
		child + "  timeout: 300",
		child + "  description: " + quote("KeyBar: run commands with secrets, each use approved by you"),
	}
	var out []string
	if idx < 0 {
		out = append(lines, "extensions:")
		out = append(out, block...)
	} else {
		out = append(out, lines[:idx+1]...)
		rest := lines[idx+1:]
		// Our entry, if there: from "  keybar:" to the next line as far left.
		ours := regexp.MustCompile(`^` + regexp.QuoteMeta(child) + `(keybar|"keybar"|'keybar'):\s*(#.*)?$`)
		kept := []string{}
		found := false
		for i := 0; i < len(rest); i++ {
			if indentLen(rest[i]) == 0 && !blankOrComment(rest[i]) {
				kept = append(kept, rest[i:]...)
				break
			}
			if ours.MatchString(rest[i]) {
				found = true
				for i+1 < len(rest) && (strings.TrimSpace(rest[i+1]) == "" || indentLen(rest[i+1]) > len(child)) {
					i++
				}
				if add {
					kept = append(kept, block...)
				}
				continue
			}
			kept = append(kept, rest[i])
		}
		if add && !found {
			out = append(out, block...)
		}
		out = append(out, kept...)
		if !add {
			// extensions: with nothing under it left: {}.
			empty := true
			for _, l := range kept {
				if indentLen(l) == 0 && !blankOrComment(l) {
					break
				}
				if !blankOrComment(l) {
					empty = false
					break
				}
			}
			if empty {
				out[idx] = "extensions: {}"
			}
		}
	}
	res := []byte(strings.Join(out, nl) + nl)
	after, err := gooseDoc(res)
	if err != nil {
		return nil, fmt.Errorf("改完后无法解析（%v），请手动修改", err)
	}
	cmd, got, found, _ := gooseServer(res)
	switch {
	case add && (!found || cmd != exe || !slices.Equal(got, args)):
		return nil, errors.New("keybar 的写法不常见，请手动修改")
	case !add && found:
		return nil, errors.New("keybar 的写法不常见，请手动删除")
	}
	strip := func(d map[string]any) map[string]any {
		if ext, ok := d["extensions"].(map[string]any); ok {
			delete(ext, ServerName)
			if len(ext) == 0 {
				delete(d, "extensions")
			}
		} else if d["extensions"] == nil {
			delete(d, "extensions")
		}
		return d
	}
	if !reflect.DeepEqual(strip(before), strip(after)) {
		return nil, errors.New("改完后其他设置对不上，请手动修改")
	}
	return res, nil
}

// DeepSeek Harness (dsh): $DSH_HOME/cordis.patch.yml, a list of Cordis
// patches; KeyBar's is one insert of the MCP client plugin, between
// marker comments.

const (
	dshPlugin = "@deepseek-ai/dsh-mcp-client"
	dshBegin  = "# keybar:begin — added by KeyBar (keys disconnect removes it)"
	dshEnd    = "# keybar:end"
)

func dshItems(b []byte) ([]any, error) {
	var doc any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("无法解析：%w", err)
	}
	if doc == nil {
		return nil, nil
	}
	l, ok := doc.([]any)
	if !ok {
		return nil, errors.New("不是补丁列表")
	}
	return l, nil
}

// dshOurs tells whether a patch inserts KeyBar's MCP client, and with
// which command.
func dshOurs(item any) (cmd string, args []string, ok bool) {
	m, _ := item.(map[string]any)
	ins, _ := m["insert"].([]any)
	for _, p := range ins {
		pm, _ := p.(map[string]any)
		cfg, _ := pm["config"].(map[string]any)
		if pm["name"] == dshPlugin && cfg["serverName"] == ServerName {
			cmd, _ = cfg["command"].(string)
			return cmd, strList(cfg["args"]), true
		}
	}
	return "", nil, false
}

func dshServer(b []byte) (cmd string, args []string, found bool, err error) {
	items, err := dshItems(b)
	if err != nil {
		return "", nil, false, err
	}
	for _, it := range items {
		if c, a, ok := dshOurs(it); ok {
			return c, a, true, nil
		}
	}
	return "", nil, false, nil
}

func setDshServer(old []byte, exe string, args []string, add bool) ([]byte, error) {
	before, err := dshItems(old)
	if err != nil {
		return nil, err
	}
	lines, nl := yamlLines(old)
	var rest []string
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "# keybar:begin") {
			for i < len(lines) && !strings.HasPrefix(lines[i], dshEnd) {
				i++
			}
			continue
		}
		rest = append(rest, lines[i])
	}
	for len(rest) > 0 && strings.TrimSpace(rest[len(rest)-1]) == "" {
		rest = rest[:len(rest)-1]
	}
	var content []int
	for i, l := range rest {
		if !blankOrComment(l) {
			content = append(content, i)
		}
	}
	if len(content) == 1 && strings.TrimSpace(rest[content[0]]) == "[]" {
		rest = slices.Delete(rest, content[0], content[0]+1)
		content = nil
	}
	if len(content) > 0 && !strings.HasPrefix(rest[content[0]], "-") {
		return nil, errors.New("不是常见的补丁列表写法，请手动修改")
	}
	out := rest
	if add {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, dshBegin,
			"- insert:",
			"    - id: mcp-keybar",
			"      name: '"+dshPlugin+"'",
			"      config:",
			"        serverName: "+ServerName,
			"        transport: stdio",
			"        command: "+quote(exe),
			"        args: "+quoteList(args),
			dshEnd)
	} else if len(content) == 0 {
		out = append(out, "[]")
	}
	res := []byte(strings.Join(out, nl) + nl)
	after, err := dshItems(res)
	if err != nil {
		return nil, fmt.Errorf("改完后无法解析（%v），请手动修改", err)
	}
	others := func(items []any) (l []any, n int, cmd string, args []string) {
		for _, it := range items {
			if c, a, ok := dshOurs(it); ok {
				n++
				cmd, args = c, a
				continue
			}
			l = append(l, it)
		}
		return
	}
	ob, _, _, _ := others(before)
	oa, n, cmd, got := others(after)
	switch {
	case add && (n != 1 || cmd != exe || !slices.Equal(got, args)):
		return nil, errors.New("keybar 的写法不常见（不在 KeyBar 的标记之间），请手动修改")
	case !add && n != 0:
		return nil, errors.New("keybar 的写法不常见（不在 KeyBar 的标记之间），请手动删除")
	case !reflect.DeepEqual(ob, oa):
		return nil, errors.New("改完后其他补丁对不上，请手动修改")
	}
	return res, nil
}
