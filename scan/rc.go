package scan

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"

	"keybar/project"
)

var (
	exportRE = regexp.MustCompile(`^\s*export\s+([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
	fishRE   = regexp.MustCompile(`^\s*set\s+(?:-[a-zA-Z]+\s+)*?-[a-zA-Z]*x[a-zA-Z]*\s+(?:-[a-zA-Z]+\s+)*([A-Za-z_][A-Za-z0-9_]*)\s+(.*)$`)
)

// ParseRC reads `export NAME=value` lines (fish: `set -gx NAME value`) whose
// value is literal: no $VAR, $(…) or `…` to work out.
func ParseRC(b []byte, fish bool) []project.Var {
	var out []project.Var
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		re := exportRE
		if fish {
			re = fishRE
		}
		m := re.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		val, ok := literal(m[2])
		if !ok {
			continue
		}
		out = append(out, project.Var{Name: m[1], Value: val, Line: n})
	}
	return out
}

// literal reads a shell word: 'single', "double" with no expansions, or
// bare up to a comment or ;.
func literal(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	switch s[0] {
	case '\'':
		end := strings.IndexByte(s[1:], '\'')
		if end < 0 {
			return "", false
		}
		return s[1 : 1+end], rest(s[2+end:])
	case '"':
		end := strings.IndexByte(s[1:], '"')
		if end < 0 {
			return "", false
		}
		v := s[1 : 1+end]
		if strings.ContainsAny(v, "$`\\") {
			return "", false
		}
		return v, rest(s[2+end:])
	}
	end := len(s)
	if i := strings.IndexAny(s, " \t;&|"); i >= 0 {
		end = i
	}
	v := s[:end]
	if strings.ContainsAny(v, "$`\\\"'(){}") {
		return "", false
	}
	return v, rest(s[end:])
}

// rest: after the value only a comment or the end of the command.
func rest(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || s[0] == '#' || s[0] == ';'
}
