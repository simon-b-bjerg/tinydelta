// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"fmt"
	"strings"
)

// A small, strict reader for the YAML subset rule files use (rules.go), so
// tinydelta needs no YAML dependency: block mappings and sequences by
// indentation, single-line flow collections ({a: 1, b: [x, y]}), plain,
// 'single-' and "double-quoted" scalars, and # comments. Anything else
// (anchors, tags, multi-line scalars, several documents, tabs) is an error
// naming the line, never silently read some other way.

// yamlNode is a parsed value: a mapping (keys in order), a sequence or a
// scalar.
type yamlNode struct {
	line   int
	keys   []string
	values map[string]*yamlNode
	items  []*yamlNode
	scalar string
	quoted bool
	kind   byte // 'm' mapping, 's' sequence, 'v' scalar
}

type yamlLine struct {
	n      int // 1-based
	indent int
	text   string // without indentation and comment
}

func yamlError(line int, format string, args ...any) error {
	return fmt.Errorf("line %d: %s", line, fmt.Sprintf(format, args...))
}

// parseYAMLSubset parses a document into its root node.
func parseYAMLSubset(src []byte) (*yamlNode, error) {
	var lines []yamlLine
	for i, raw := range strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n") {
		n := i + 1
		if strings.Contains(raw, "\t") {
			return nil, yamlError(n, "tab character: indent with spaces")
		}
		text, err := yamlStripComment(raw, n)
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimLeft(text, " ")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		if trimmed == "---" || trimmed == "..." || strings.HasPrefix(trimmed, "%") {
			return nil, yamlError(n, "document markers and directives are not supported")
		}
		lines = append(lines, yamlLine{n, len(text) - len(trimmed), strings.TrimRight(trimmed, " ")})
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty document")
	}
	p := &yamlParser{lines: lines}
	root, err := p.block(lines[0].indent)
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.lines) {
		return nil, yamlError(p.lines[p.pos].n, "unexpected indentation")
	}
	return root, nil
}

// yamlStripComment removes a # comment: one at the start or after a space,
// outside quotes.
func yamlStripComment(s string, line int) (string, error) {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote != 0:
			if c == quote {
				if quote == '\'' && i+1 < len(s) && s[i+1] == '\'' {
					i++
				} else {
					quote = 0
				}
			}
		case c == '"' || c == '\'':
			if i == 0 || strings.ContainsRune(" [{,:-", rune(s[i-1])) {
				quote = c
			}
		case c == '#' && (i == 0 || s[i-1] == ' '):
			return s[:i], nil
		}
	}
	if quote != 0 {
		return "", yamlError(line, "unterminated quoted string (multi-line strings are not supported)")
	}
	return s, nil
}

type yamlParser struct {
	lines []yamlLine
	pos   int
}

// block parses the mapping or sequence whose entries start at indent.
func (p *yamlParser) block(indent int) (*yamlNode, error) {
	l := p.lines[p.pos]
	if l.text == "-" || strings.HasPrefix(l.text, "- ") {
		return p.sequence(indent)
	}
	return p.mapping(indent, nil)
}

func (p *yamlParser) sequence(indent int) (*yamlNode, error) {
	seq := &yamlNode{kind: 's', line: p.lines[p.pos].n}
	for p.pos < len(p.lines) {
		l := p.lines[p.pos]
		if l.indent < indent {
			break
		}
		if l.indent > indent || !(l.text == "-" || strings.HasPrefix(l.text, "- ")) {
			return nil, yamlError(l.n, "expected a sequence item \"- \" at column %d", indent+1)
		}
		rest := strings.TrimLeft(strings.TrimPrefix(l.text, "-"), " ")
		var item *yamlNode
		var err error
		switch {
		case rest == "":
			p.pos++
			if p.pos >= len(p.lines) || p.lines[p.pos].indent <= indent {
				item = &yamlNode{kind: 'v', line: l.n}
			} else {
				item, err = p.block(p.lines[p.pos].indent)
			}
		case yamlKeyEnd(rest) >= 0:
			// A mapping that starts on the dash's line: its keys are at
			// the column after "- ".
			col := l.indent + len(l.text) - len(rest)
			p.lines[p.pos] = yamlLine{l.n, col, rest}
			item, err = p.mapping(col, nil)
		default:
			item, err = yamlInline(rest, l.n)
			p.pos++
		}
		if err != nil {
			return nil, err
		}
		seq.items = append(seq.items, item)
	}
	return seq, nil
}

func (p *yamlParser) mapping(indent int, m *yamlNode) (*yamlNode, error) {
	if m == nil {
		m = &yamlNode{kind: 'm', line: p.lines[p.pos].n, values: map[string]*yamlNode{}}
	}
	for p.pos < len(p.lines) {
		l := p.lines[p.pos]
		if l.indent < indent {
			break
		}
		if l.indent > indent {
			return nil, yamlError(l.n, "unexpected indentation")
		}
		end := yamlKeyEnd(l.text)
		if end < 0 {
			return nil, yamlError(l.n, "expected \"key: value\"")
		}
		key, err := yamlKey(l.text[:end], l.n)
		if err != nil {
			return nil, err
		}
		if _, dup := m.values[key]; dup {
			return nil, yamlError(l.n, "duplicate key %q", key)
		}
		rest := strings.TrimLeft(l.text[end+1:], " ")
		p.pos++
		var v *yamlNode
		switch {
		case rest != "":
			v, err = yamlInline(rest, l.n)
		case p.pos < len(p.lines) && p.lines[p.pos].indent > indent:
			v, err = p.block(p.lines[p.pos].indent)
		case p.pos < len(p.lines) && p.lines[p.pos].indent == indent && (p.lines[p.pos].text == "-" || strings.HasPrefix(p.lines[p.pos].text, "- ")):
			v, err = p.sequence(indent) // "key:" then items at the same column
		default:
			v = &yamlNode{kind: 'v', line: l.n}
		}
		if err != nil {
			return nil, err
		}
		m.keys = append(m.keys, key)
		m.values[key] = v
	}
	return m, nil
}

// yamlKeyEnd is the index of the ':' ending a "key: value" (or "key:")
// at the start of s, outside quotes and brackets; -1 if s isn't one.
func yamlKeyEnd(s string) int {
	if s == "" || strings.ContainsRune("[{\"'", rune(s[0])) {
		if s != "" && (s[0] == '"' || s[0] == '\'') { // a quoted key
			q := s[0]
			for i := 1; i < len(s); i++ {
				if s[i] == q {
					if i+1 < len(s) && s[i+1] == ':' && (i+2 == len(s) || s[i+2] == ' ') {
						return i + 1
					}
					return -1
				}
			}
		}
		return -1
	}
	for i := 0; i < len(s); i++ {
		if s[i] == ':' && (i+1 == len(s) || s[i+1] == ' ') {
			return i
		}
		if s[i] == ' ' && i+1 < len(s) && s[i+1] == '#' {
			return -1
		}
	}
	return -1
}

func yamlKey(s string, line int) (string, error) {
	n, err := yamlScalar(strings.TrimSpace(s), line)
	if err != nil {
		return "", err
	}
	return n.scalar, nil
}

// yamlInline parses a value on one line: a flow collection or a scalar.
func yamlInline(s string, line int) (*yamlNode, error) {
	r := &yamlFlow{s: s, line: line}
	v, err := r.value()
	if err != nil {
		return nil, err
	}
	r.space()
	if r.i != len(r.s) {
		return nil, yamlError(line, "unexpected %q after the value", r.s[r.i:])
	}
	return v, nil
}

type yamlFlow struct {
	s    string
	i    int
	line int
}

func (r *yamlFlow) space() {
	for r.i < len(r.s) && r.s[r.i] == ' ' {
		r.i++
	}
}

func (r *yamlFlow) value() (*yamlNode, error) {
	r.space()
	if r.i >= len(r.s) {
		return nil, yamlError(r.line, "missing value")
	}
	switch r.s[r.i] {
	case '[':
		r.i++
		seq := &yamlNode{kind: 's', line: r.line}
		r.space()
		if r.i < len(r.s) && r.s[r.i] == ']' {
			r.i++
			return seq, nil
		}
		for {
			v, err := r.value()
			if err != nil {
				return nil, err
			}
			seq.items = append(seq.items, v)
			r.space()
			if r.i >= len(r.s) {
				return nil, yamlError(r.line, "unterminated [ (flow collections must end on their line)")
			}
			if r.s[r.i] == ']' {
				r.i++
				return seq, nil
			}
			if r.s[r.i] != ',' {
				return nil, yamlError(r.line, "expected , or ] in a flow sequence")
			}
			r.i++
		}
	case '{':
		r.i++
		m := &yamlNode{kind: 'm', line: r.line, values: map[string]*yamlNode{}}
		r.space()
		if r.i < len(r.s) && r.s[r.i] == '}' {
			r.i++
			return m, nil
		}
		for {
			r.space()
			k, err := r.scalar(":")
			if err != nil {
				return nil, err
			}
			if r.i >= len(r.s) || r.s[r.i] != ':' {
				return nil, yamlError(r.line, "expected \"key: value\" in a flow mapping")
			}
			r.i++
			if _, dup := m.values[k.scalar]; dup {
				return nil, yamlError(r.line, "duplicate key %q", k.scalar)
			}
			v, err := r.value()
			if err != nil {
				return nil, err
			}
			m.keys = append(m.keys, k.scalar)
			m.values[k.scalar] = v
			r.space()
			if r.i >= len(r.s) {
				return nil, yamlError(r.line, "unterminated { (flow collections must end on their line)")
			}
			if r.s[r.i] == '}' {
				r.i++
				return m, nil
			}
			if r.s[r.i] != ',' {
				return nil, yamlError(r.line, "expected , or } in a flow mapping")
			}
			r.i++
		}
	}
	return r.scalar("")
}

// scalar reads a quoted scalar, or a plain one up to a flow delimiter (or
// one of stops), trimmed.
func (r *yamlFlow) scalar(stops string) (*yamlNode, error) {
	r.space()
	if r.i < len(r.s) && (r.s[r.i] == '"' || r.s[r.i] == '\'') {
		q := r.s[r.i]
		var b strings.Builder
		for j := r.i + 1; j < len(r.s); j++ {
			c := r.s[j]
			switch {
			case q == '"' && c == '\\' && j+1 < len(r.s):
				j++
				switch r.s[j] {
				case '"', '\\', '/':
					b.WriteByte(r.s[j])
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				default:
					return nil, yamlError(r.line, "unsupported escape \\%c", r.s[j])
				}
			case c == q && q == '\'' && j+1 < len(r.s) && r.s[j+1] == '\'':
				b.WriteByte('\'')
				j++
			case c == q:
				r.i = j + 1
				return &yamlNode{kind: 'v', line: r.line, scalar: b.String(), quoted: true}, nil
			default:
				b.WriteByte(c)
			}
		}
		return nil, yamlError(r.line, "unterminated quoted string")
	}
	start := r.i
	depthStops := ",]}" + stops
	for r.i < len(r.s) && !strings.ContainsRune(depthStops, rune(r.s[r.i])) {
		if r.s[r.i] == ':' && stops == "" && r.i+1 < len(r.s) && r.s[r.i+1] == ' ' {
			return nil, yamlError(r.line, "unexpected \": \" in a value (quote it)")
		}
		r.i++
	}
	return yamlScalar(strings.TrimSpace(r.s[start:r.i]), r.line)
}

// yamlScalar checks a plain scalar for the YAML features this reader
// doesn't support.
func yamlScalar(s string, line int) (*yamlNode, error) {
	if s == "" {
		return nil, yamlError(line, "missing value")
	}
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		r := &yamlFlow{s: s, line: line}
		return r.scalar("")
	}
	if strings.ContainsRune("&*!|>@`%", rune(s[0])) {
		return nil, yamlError(line, "%q: anchors, aliases, tags and block scalars are not supported (quote the value if it is text)", s)
	}
	return &yamlNode{kind: 'v', line: line, scalar: s}, nil
}
