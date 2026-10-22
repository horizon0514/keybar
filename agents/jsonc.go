package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// JSON settings, comments and trailing commas allowed (JSONC: VS Code,
// Zed, opencode…), changed by splicing text in place: every other byte of
// the file, comments included, stays as it was. Each change is checked by
// decoding the file before and after: if anything but KeyBar's entry
// differs, nothing is written.

type jval struct {
	kind       byte // '{', '[', 's' string, 'v' other
	start, end int  // the value's text: s[start:end]
	members    []jmem
}

type jmem struct {
	key    string
	kstart int
	v      *jval
}

type jparser struct {
	s string
	i int
}

func (p *jparser) errf(format string, a ...any) error {
	line := strings.Count(p.s[:min(p.i, len(p.s))], "\n") + 1
	return fmt.Errorf("第 %d 行："+format, append([]any{line}, a...)...)
}

// ws skips white space and comments.
func (p *jparser) ws() error {
	for p.i < len(p.s) {
		switch c := p.s[p.i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.i++
		case strings.HasPrefix(p.s[p.i:], "\ufeff") && p.i == 0:
			p.i += 3
		case strings.HasPrefix(p.s[p.i:], "//"):
			for p.i < len(p.s) && p.s[p.i] != '\n' {
				p.i++
			}
		case strings.HasPrefix(p.s[p.i:], "/*"):
			end := strings.Index(p.s[p.i+2:], "*/")
			if end < 0 {
				return p.errf("注释没有结束")
			}
			p.i += 2 + end + 2
		default:
			return nil
		}
	}
	return nil
}

func (p *jparser) str() (string, error) {
	start := p.i
	p.i++
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case '\\':
			p.i += 2
			continue
		case '\n':
			return "", p.errf("字符串没有结束")
		case '"':
			p.i++
			var out string
			if err := json.Unmarshal([]byte(p.s[start:p.i]), &out); err != nil {
				return "", p.errf("字符串无法解析")
			}
			return out, nil
		}
		p.i++
	}
	return "", p.errf("字符串没有结束")
}

func (p *jparser) value(depth int) (*jval, error) {
	if depth > 200 {
		return nil, p.errf("嵌套太深")
	}
	if err := p.ws(); err != nil {
		return nil, err
	}
	if p.i >= len(p.s) {
		return nil, p.errf("内容不完整")
	}
	v := &jval{start: p.i}
	switch c := p.s[p.i]; c {
	case '"':
		v.kind = 's'
		if _, err := p.str(); err != nil {
			return nil, err
		}
	case '{', '[':
		v.kind = c
		closer := byte('}')
		if c == '[' {
			closer = ']'
		}
		p.i++
		for {
			if err := p.ws(); err != nil {
				return nil, err
			}
			if p.i < len(p.s) && p.s[p.i] == closer {
				p.i++
				break
			}
			if c == '{' {
				if p.i >= len(p.s) || p.s[p.i] != '"' {
					return nil, p.errf("这里应该是名字")
				}
				kstart := p.i
				key, err := p.str()
				if err != nil {
					return nil, err
				}
				if err := p.ws(); err != nil {
					return nil, err
				}
				if p.i >= len(p.s) || p.s[p.i] != ':' {
					return nil, p.errf("名字后面应该是冒号")
				}
				p.i++
				val, err := p.value(depth + 1)
				if err != nil {
					return nil, err
				}
				v.members = append(v.members, jmem{key, kstart, val})
			} else if _, err := p.value(depth + 1); err != nil {
				return nil, err
			}
			if err := p.ws(); err != nil {
				return nil, err
			}
			if p.i < len(p.s) && p.s[p.i] == ',' {
				p.i++
				continue
			}
			if p.i < len(p.s) && p.s[p.i] == closer {
				p.i++
				break
			}
			return nil, p.errf("这里应该是逗号或 %c", closer)
		}
	default:
		v.kind = 'v'
		for p.i < len(p.s) && !strings.ContainsRune(",]} \t\r\n/", rune(p.s[p.i])) {
			p.i++
		}
		if !json.Valid([]byte(p.s[v.start:p.i])) {
			return nil, p.errf("无法解析 %q", p.s[v.start:p.i])
		}
	}
	v.end = p.i
	return v, nil
}

// parseJSONC reads a whole JSONC document.
func parseJSONC(s string) (*jval, error) {
	p := &jparser{s: s}
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	if err := p.ws(); err != nil {
		return nil, err
	}
	if p.i != len(s) {
		return nil, p.errf("后面还有内容")
	}
	return v, nil
}

// cleanJSONC is s as plain JSON: comments and trailing commas dropped.
func cleanJSONC(s string) string {
	var b strings.Builder
	p := &jparser{s: s}
	for p.i < len(s) {
		c := s[p.i]
		switch {
		case c == '"':
			start := p.i
			if _, err := p.str(); err != nil {
				b.WriteString(s[start:])
				return b.String()
			}
			b.WriteString(s[start:p.i])
		case c == '/' || (c == 0xef && p.i == 0):
			start := p.i
			_ = p.ws()
			if p.i == start {
				b.WriteByte(c)
				p.i++
			}
		case c == ',':
			p.i++
			q := &jparser{s: s, i: p.i}
			_ = q.ws()
			if q.i < len(s) && (s[q.i] == '}' || s[q.i] == ']') {
				continue
			}
			b.WriteByte(',')
		default:
			b.WriteByte(c)
			p.i++
		}
	}
	return b.String()
}

func decodeJSONC(s string) (any, error) {
	if strings.TrimSpace(s) == "" {
		return map[string]any{}, nil
	}
	if _, err := parseJSONC(s); err != nil {
		return nil, err
	}
	var v any
	d := json.NewDecoder(strings.NewReader(cleanJSONC(s)))
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *jval) member(key string) (*jmem, error) {
	var found *jmem
	for i := range v.members {
		if v.members[i].key == key {
			if found != nil {
				return nil, fmt.Errorf("%q 出现了两次", key)
			}
			found = &v.members[i]
		}
	}
	return found, nil
}

func lineStart(s string, i int) int { return strings.LastIndexByte(s[:i], '\n') + 1 }

func lineEnd(s string, i int) int {
	if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
		j += i
		if j > 0 && s[j-1] == '\r' {
			j--
		}
		return j
	}
	return len(s)
}

// indentAt is the white space that begins i's line.
func indentAt(s string, i int) string {
	ls := lineStart(s, i)
	j := ls
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	return s[ls:j]
}

func ownLine(s string, i int) bool { return strings.TrimLeft(s[lineStart(s, i):i], " \t") == "" }

// field is a member of an object written by KeyBar, in its order.
type field struct {
	Key string
	Val any
}

type ordered []field

func (o ordered) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(f.Key)
		b.Write(k)
		b.WriteByte(':')
		v, err := json.Marshal(f.Val)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func format(v any, prefix, unit string, compact bool) string {
	raw, _ := json.Marshal(v)
	if compact {
		return string(raw)
	}
	var b bytes.Buffer
	_ = json.Indent(&b, raw, prefix, unit)
	return b.String()
}

func splice(s string, at, end int, text string) string { return s[:at] + text + s[end:] }

// setMember sets key in the object obj of s to v: in place when it is
// there, else as the object's last member, in the object's style.
func setMember(s string, obj *jval, key string, v any, unit string) (string, error) {
	m, err := obj.member(key)
	if err != nil {
		return "", err
	}
	k, _ := json.Marshal(key)
	if m != nil {
		old := s[m.v.start:m.v.end]
		// A one-line array or object stays on one line.
		compact := (m.v.kind == '{' || m.v.kind == '[') && !strings.Contains(old, "\n")
		return splice(s, m.v.start, m.v.end, format(v, indentAt(s, m.kstart), unit, compact)), nil
	}
	objIndent := indentAt(s, obj.start)
	if len(obj.members) == 0 {
		in := objIndent + unit
		text := "\n" + in + string(k) + ": " + format(v, in, unit, false)
		inner := s[obj.start+1 : obj.end-1]
		if strings.TrimSpace(inner) == "" {
			return splice(s, obj.start+1, obj.end-1, text+"\n"+objIndent), nil
		}
		return splice(s, obj.start+1, obj.start+1, text), nil
	}
	first, last := obj.members[0], obj.members[len(obj.members)-1]
	// Is there a comma after the last member already?
	q := &jparser{s: s, i: last.v.end}
	_ = q.ws()
	trailing := q.i < len(s) && s[q.i] == ','
	if !ownLine(s, first.kstart) || lineStart(s, first.kstart) == lineStart(s, obj.start) {
		// One line: {"a": 1, "b": 2}
		item := string(k) + ": " + format(v, "", unit, true)
		if trailing {
			return splice(s, q.i+1, q.i+1, " "+item+","), nil
		}
		return splice(s, last.v.end, last.v.end, ", "+item), nil
	}
	in := indentAt(s, first.kstart)
	item := "\n" + in + string(k) + ": " + format(v, in, unit, false)
	pos := last.v.end
	if trailing {
		pos = q.i + 1
		item += ","
	}
	// After the line's end comment, if the rest of the line is only that.
	at := pos
	if rest := strings.TrimLeft(s[pos:lineEnd(s, pos)], " \t"); rest == "" || strings.HasPrefix(rest, "//") {
		at = lineEnd(s, pos)
	}
	s = splice(s, at, at, item)
	if !trailing {
		s = splice(s, last.v.end, last.v.end, ",")
	}
	return s, nil
}

// removeMember removes obj's member key from s.
func removeMember(s string, obj *jval, key string) (string, error) {
	idx := -1
	for i, m := range obj.members {
		if m.key == key {
			if idx >= 0 {
				return "", fmt.Errorf("%q 出现了两次", key)
			}
			idx = i
		}
	}
	if idx < 0 {
		return s, nil
	}
	m := obj.members[idx]
	a := m.kstart
	whole := ownLine(s, a)
	if whole {
		a = lineStart(s, a)
	}
	b := m.v.end
	for b < len(s) && (s[b] == ' ' || s[b] == '\t') {
		b++
	}
	comma := b < len(s) && s[b] == ','
	if comma {
		b++
		for b < len(s) && (s[b] == ' ' || s[b] == '\t') {
			b++
		}
	}
	if whole {
		if strings.HasPrefix(s[b:], "//") {
			b = lineEnd(s, b)
		}
		if strings.HasPrefix(s[b:], "\r\n") {
			b += 2
		} else if strings.HasPrefix(s[b:], "\n") {
			b++
		}
		// Nothing but this member on its lines: take them whole.
	}
	out := splice(s, a, b, "")
	if !comma && idx > 0 {
		// It was the last: the comma before it goes.
		q := &jparser{s: out, i: obj.members[idx-1].v.end}
		_ = q.ws()
		if q.i < len(out) && out[q.i] == ',' {
			out = splice(out, q.i, q.i+1, "")
		}
	}
	return out, nil
}

// at walks path from the document's root; nil when a step is missing.
func at(root *jval, path []string) (*jval, error) {
	cur := root
	for _, k := range path {
		if cur.kind != '{' {
			return nil, errors.New("不是 JSON 对象")
		}
		m, err := cur.member(k)
		if err != nil || m == nil {
			return nil, err
		}
		cur = m.v
	}
	return cur, nil
}

// setPath sets path in s to v, making the objects on the way.
func setPath(s string, path []string, v any, unit string) (string, error) {
	root, err := parseJSONC(s)
	if err != nil {
		return "", err
	}
	if root.kind != '{' {
		return "", errors.New("不是 JSON 对象")
	}
	cur := root
	for i, k := range path[:len(path)-1] {
		m, err := cur.member(k)
		if err != nil {
			return "", err
		}
		if m == nil || m.v.kind == 'v' && s[m.v.start:m.v.end] == "null" {
			for j := len(path) - 1; j > i; j-- {
				v = ordered{{path[j], v}}
			}
			return setMember(s, cur, k, v, unit)
		}
		if m.v.kind != '{' {
			return "", fmt.Errorf("%q 不是对象", k)
		}
		cur = m.v
	}
	return setMember(s, cur, path[len(path)-1], v, unit)
}

func removePath(s string, path []string) (string, error) {
	root, err := parseJSONC(s)
	if err != nil {
		return "", err
	}
	parent, err := at(root, path[:len(path)-1])
	if err != nil || parent == nil {
		return s, err
	}
	if parent.kind != '{' {
		return "", errors.New("不是 JSON 对象")
	}
	out, err := removeMember(s, parent, path[len(path)-1])
	if err != nil {
		return "", err
	}
	// An object left with only white space: {}.
	if root, err = parseJSONC(out); err == nil {
		if p, _ := at(root, path[:len(path)-1]); p != nil && p.kind == '{' && len(p.members) == 0 && strings.TrimSpace(out[p.start+1:p.end-1]) == "" {
			out = splice(out, p.start, p.end, "{}")
		}
	}
	return out, nil
}

// goSet and goDel do to decoded JSON what setPath and removePath do to
// its text, to check them.
func goSet(doc any, path []string, v any) any {
	raw, _ := json.Marshal(v)
	var val any
	_ = json.Unmarshal(raw, &val)
	m, ok := doc.(map[string]any)
	if !ok {
		m = map[string]any{}
	}
	cur := m
	for _, k := range path[:len(path)-1] {
		next, ok := cur[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
	cur[path[len(path)-1]] = val
	return m
}

func goDel(doc any, path []string) any {
	cur, ok := doc.(map[string]any)
	for _, k := range path[:len(path)-1] {
		if !ok {
			return doc
		}
		cur, ok = cur[k].(map[string]any)
	}
	if ok {
		delete(cur, path[len(path)-1])
	}
	return doc
}

func sameJSON(a, b any) bool { return reflect.DeepEqual(a, b) }
