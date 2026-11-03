// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Relocation rule tables (docs/relocation-rules.md): which bytes of a
// firmware image hold addresses, as data. A YAML rule file is parsed and
// compiled to a binary table; the engine below applies a table (the C
// decoder's engine applies the same bytes). Everything the engine decides
// at a position depends only on the raw old image near it, the map and
// the table.

//go:embed rules/*.yaml
var builtinRuleFiles embed.FS

// Rules is a compiled relocation rule table (ParseRules, BuiltinRules).
type Rules struct {
	name      string
	table     []byte // the binary table, as shipped and as the C engine reads it
	crc       uint16 // CRC-16/CCITT of table
	unit      int    // bytes between positions
	wordBytes int
	big       bool
	sync      int
	lists     [4][]rule // rulePrefixes, ruleLengths, ruleCode, ruleData
	reach     int       // bytes a rule can reach beyond its position, less one
	builtin   Relocation
}

const (
	rulePrefixes = iota
	ruleLengths
	ruleCode
	ruleData
)

var ruleListNames = [4]string{"prefixes", "lengths", "code", "data"}

// Address spaces a rule's addresses are looked up in.
const (
	spaceAuto = iota // the image if the address points there, else the RAM ranges
	spaceCode        // the image only
	spaceData        // the RAM ranges only
)

type ruleCond struct {
	word        int
	neg         bool
	value, mask uint32
}

// ruleSlice is one part of an address: bits hi..lo of word, or, if fixed,
// hi bits that are always lo's low bits.
type ruleSlice struct {
	word   int
	hi, lo int
	fixed  bool
}

func (s ruleSlice) bits() int {
	if s.fixed {
		return s.hi
	}
	return s.hi - s.lo + 1
}

type ruleUnless struct {
	rule int
	at   []int
}

type rule struct {
	name     string
	conds    []ruleCond
	n        int           // prefixes: bytes to skip; lengths: length in bytes
	addrs    [][]ruleSlice // 0-2 addresses, slices most significant first
	size     int           // bytes that must be in the image for its addresses to move
	condSize int           // bytes its conditions read
	align    int
	relative bool
	relBase  int
	signExt  int
	require  bool // the (first) address must point into the image
	space    int
	unless   []ruleUnless
}

// Limits (docs/relocation-rules.md, "Limits and validation").
const (
	rulesMaxRules     = 64
	rulesMaxConds     = 4
	rulesMaxWords     = 8
	rulesMaxSize      = 16
	rulesMaxSlices    = 6
	rulesMaxUnless    = 2
	rulesMaxUnlessAt  = 4
	rulesMaxUnlessOff = 16
	rulesMaxSync      = 16
	rulesMaxSkip      = 4
	rulesMaxLength    = 16
	rulesVersion      = 1
)

// BuiltinRules returns a rule table built into tinydelta: "thumb" or
// "msp430x" (rules/*.yaml).
func BuiltinRules(name string) (*Rules, error) {
	src, err := builtinRuleFiles.ReadFile("rules/" + name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("tinydelta: no built-in rules %q (thumb, msp430x)", name)
	}
	r, err := ParseRules(src)
	if err != nil {
		return nil, fmt.Errorf("tinydelta: built-in rules %s: %w", name, err)
	}
	switch name {
	case "thumb":
		r.builtin = RelocationThumb
	case "msp430x":
		r.builtin = RelocationMSP430X
	}
	return r, nil
}

func mustBuiltin(name string) *Rules {
	r, err := BuiltinRules(name)
	if err != nil {
		panic(err)
	}
	return r
}

// rulesFor is the built-in table of a Relocation; nil for none.
func rulesFor(rel Relocation) *Rules {
	switch rel {
	case RelocationThumb:
		return builtinThumb
	case RelocationMSP430X:
		return builtinMSP430X
	}
	return nil
}

var builtinThumb, builtinMSP430X = mustBuiltin("thumb"), mustBuiltin("msp430x")

// Name is the rule file's name.
func (r *Rules) Name() string { return r.name }

// Table returns the binary table (docs/relocation-rules.md, "Binary
// table"), as a patch carries it.
func (r *Rules) Table() []byte { return append([]byte{}, r.table...) }

// CRC is the CRC-16/CCITT of Table, as a patch names a built-in table.
func (r *Rules) CRC() uint16 { return r.crc }

// ---- YAML to rules -------------------------------------------------------

// ParseRules parses and compiles a YAML rule file.
func ParseRules(src []byte) (*Rules, error) {
	root, err := parseYAMLSubset(src)
	if err != nil {
		return nil, err
	}
	r, err := rulesFromYAML(root)
	if err != nil {
		return nil, err
	}
	table := r.compile()
	parsed, err := ParseRulesTable(table)
	if err != nil {
		return nil, fmt.Errorf("compiled table: %w", err) // the YAML checks should have caught it
	}
	for l := range parsed.lists {
		for i := range parsed.lists[l] {
			parsed.lists[l][i].name = r.lists[l][i].name
		}
	}
	parsed.name = r.name
	return parsed, nil
}

type yamlFields struct {
	node *yamlNode
	used map[string]bool
}

func (f *yamlFields) get(key string) *yamlNode {
	f.used[key] = true
	return f.node.values[key]
}

func (f *yamlFields) unknown(what string) error {
	for _, k := range f.node.keys {
		if !f.used[k] {
			return yamlError(f.node.values[k].line, "%s: unknown key %q", what, k)
		}
	}
	return nil
}

func fields(n *yamlNode, what string) (*yamlFields, error) {
	if n == nil || n.kind != 'm' {
		line := 0
		if n != nil {
			line = n.line
		}
		return nil, yamlError(line, "%s: expected a mapping", what)
	}
	return &yamlFields{node: n, used: map[string]bool{}}, nil
}

func yamlInt(n *yamlNode, what string, lo, hi int) (int, error) {
	if n == nil || n.kind != 'v' || n.quoted {
		return 0, fmt.Errorf("%s: expected a number", what)
	}
	v, err := strconv.ParseInt(n.scalar, 0, 64)
	if err != nil || v < int64(lo) || v > int64(hi) {
		return 0, yamlError(n.line, "%s: expected a number from %d to %d, not %q", what, lo, hi, n.scalar)
	}
	return int(v), nil
}

func yamlString(n *yamlNode, what string) (string, error) {
	if n == nil || n.kind != 'v' || n.scalar == "" {
		line := 0
		if n != nil {
			line = n.line
		}
		return "", yamlError(line, "%s: expected text", what)
	}
	return n.scalar, nil
}

func yamlList(n *yamlNode, what string) ([]*yamlNode, error) {
	if n == nil {
		return nil, nil
	}
	if n.kind != 's' {
		return nil, yamlError(n.line, "%s: expected a list", what)
	}
	return n.items, nil
}

func rulesFromYAML(root *yamlNode) (*Rules, error) {
	f, err := fields(root, "rule file")
	if err != nil {
		return nil, err
	}
	r := &Rules{}
	version, err := yamlInt(f.get("version"), "version", rulesVersion, rulesVersion)
	if err != nil {
		return nil, err
	}
	_ = version
	if r.name, err = yamlString(f.get("name"), "name"); err != nil {
		return nil, err
	}
	if r.unit, err = yamlInt(f.get("unit"), "unit", 1, 4); err != nil {
		return nil, err
	}
	if r.unit == 3 {
		return nil, yamlError(f.node.values["unit"].line, "unit: 1, 2 or 4")
	}
	bits, err := yamlInt(f.get("word"), "word", 8, 32)
	if err != nil {
		return nil, err
	}
	if bits%8 != 0 {
		return nil, yamlError(f.node.values["word"].line, "word: 8, 16, 24 or 32")
	}
	r.wordBytes = bits / 8
	endian, err := yamlString(f.get("endian"), "endian")
	if err != nil {
		return nil, err
	}
	switch endian {
	case "little":
	case "big":
		r.big = true
	default:
		return nil, yamlError(f.node.values["endian"].line, "endian: little or big")
	}
	if r.sync, err = yamlInt(f.get("sync"), "sync", 0, rulesMaxSync); err != nil {
		return nil, err
	}
	for l, name := range ruleListNames {
		items, err := yamlList(f.get(name), name)
		if err != nil {
			return nil, err
		}
		if len(items) > rulesMaxRules {
			return nil, yamlError(f.node.values[name].line, "%s: more than %d rules", name, rulesMaxRules)
		}
		for _, it := range items {
			ru, err := r.ruleFromYAML(it, l)
			if err != nil {
				return nil, err
			}
			r.lists[l] = append(r.lists[l], ru)
		}
	}
	if err := f.unknown("rule file"); err != nil {
		return nil, err
	}
	// unless names, resolved within the rule's own list
	for _, l := range []int{ruleCode, ruleData} {
		items, _ := yamlList(root.values[ruleListNames[l]], "")
		for i, it := range items {
			un, _ := yamlList(it.values["unless"], "")
			for _, u := range un {
				name := u.values["rule"].scalar
				idx := -1
				if name == "self" {
					idx = i
				}
				for j, other := range r.lists[l] {
					if other.name == name {
						idx = j
					}
				}
				if idx < 0 {
					return nil, yamlError(u.line, "unless: no rule %q in %s", name, ruleListNames[l])
				}
				for k := range r.lists[l][i].unless {
					if r.lists[l][i].unless[k].rule == -1 {
						r.lists[l][i].unless[k].rule = idx
						break
					}
				}
			}
		}
	}
	return r, nil
}

func (r *Rules) ruleFromYAML(n *yamlNode, list int) (rule, error) {
	what := ruleListNames[list] + " rule"
	f, err := fields(n, what)
	if err != nil {
		return rule{}, err
	}
	var ru rule
	ru.align = r.unit
	if v := f.get("name"); v != nil {
		if ru.name, err = yamlString(v, what+" name"); err != nil {
			return rule{}, err
		}
		what += " \"" + ru.name + "\""
	} else if list >= ruleCode {
		return rule{}, yamlError(n.line, "%s: needs a name", what)
	}
	f.get("comment")
	matches, err := yamlList(f.get("match"), what+": match")
	if err != nil {
		return rule{}, err
	}
	if len(matches) == 0 || len(matches) > rulesMaxWords {
		return rule{}, yamlError(n.line, "%s: match needs 1 to %d words", what, rulesMaxWords)
	}
	usedWords := len(matches)
	for w, m := range matches {
		s, err := yamlString(m, what+": match")
		if err != nil {
			return rule{}, err
		}
		if s == "-" {
			continue
		}
		for _, part := range strings.Split(s, "&") {
			part = strings.TrimSpace(part)
			c := ruleCond{word: w}
			if strings.HasPrefix(part, "!") {
				c.neg, part = true, strings.TrimSpace(part[1:])
			}
			vs, ms, ok := strings.Cut(part, "/")
			v, err1 := strconv.ParseUint(vs, 16, 32)
			mk, err2 := strconv.ParseUint(ms, 16, 32)
			limit := uint64(1)<<(8*r.wordBytes) - 1
			if !ok || err1 != nil || err2 != nil || len(vs) != 2*r.wordBytes || len(ms) != 2*r.wordBytes || v > limit || mk > limit {
				return rule{}, yamlError(m.line, "%s: %q: expected value/mask in hex, %d digits each", what, part, 2*r.wordBytes)
			}
			if v&^mk != 0 {
				return rule{}, yamlError(m.line, "%s: %q: the value has bits outside the mask", what, part)
			}
			c.value, c.mask = uint32(v), uint32(mk)
			ru.conds = append(ru.conds, c)
		}
	}
	if len(ru.conds) > rulesMaxConds {
		return rule{}, yamlError(n.line, "%s: more than %d conditions", what, rulesMaxConds)
	}
	switch list {
	case rulePrefixes:
		if ru.n, err = yamlInt(f.get("skip"), what+": skip", 1, rulesMaxSkip); err != nil {
			return rule{}, err
		}
		if ru.n%r.unit != 0 {
			return rule{}, yamlError(n.line, "%s: skip must be a multiple of unit", what)
		}
	case ruleLengths:
		if ru.n, err = yamlInt(f.get("length"), what+": length", 1, rulesMaxLength); err != nil {
			return rule{}, err
		}
	default:
		for _, key := range []string{"address", "address2"} {
			v := f.get(key)
			if v == nil {
				continue
			}
			if key == "address2" && len(ru.addrs) == 0 {
				return rule{}, yamlError(v.line, "%s: address2 without address", what)
			}
			slices, err := yamlList(v, what+": "+key)
			if err != nil {
				return rule{}, err
			}
			addr, words, err := r.slicesFromYAML(slices, what+": "+key)
			if err != nil {
				return rule{}, err
			}
			usedWords = max(usedWords, words)
			ru.addrs = append(ru.addrs, addr)
		}
		if v := f.get("size"); v != nil {
			if ru.size, err = yamlInt(v, what+": size", 1, rulesMaxSize); err != nil {
				return rule{}, err
			}
		}
		if v := f.get("align"); v != nil {
			if ru.align, err = yamlInt(v, what+": align", 1, 8); err != nil {
				return rule{}, err
			}
			if ru.align&(ru.align-1) != 0 || ru.align%r.unit != 0 {
				return rule{}, yamlError(v.line, "%s: align must be a power of two and a multiple of unit", what)
			}
		}
		if v := f.get("relative"); v != nil {
			rf, err := fields(v, what+": relative")
			if err != nil {
				return rule{}, err
			}
			ru.relative = true
			if ru.relBase, err = yamlInt(rf.get("base"), what+": relative base", -128, 127); err != nil {
				return rule{}, err
			}
			if ru.signExt, err = yamlInt(rf.get("sign_extend"), what+": relative sign_extend", 1, 32); err != nil {
				return rule{}, err
			}
			if err := rf.unknown(what + ": relative"); err != nil {
				return rule{}, err
			}
		}
		if v := f.get("space"); v != nil {
			s, err := yamlString(v, what+": space")
			if err != nil {
				return rule{}, err
			}
			switch s {
			case "auto":
			case "code":
				ru.space = spaceCode
			case "data":
				ru.space = spaceData
			default:
				return rule{}, yamlError(v.line, "%s: space: auto, code or data", what)
			}
		}
		if v := f.get("require"); v != nil {
			if s, _ := yamlString(v, ""); s != "in-image" {
				return rule{}, yamlError(v.line, "%s: require: in-image", what)
			}
			ru.require = true
		}
		un, err := yamlList(f.get("unless"), what+": unless")
		if err != nil {
			return rule{}, err
		}
		if len(un) > rulesMaxUnless {
			return rule{}, yamlError(n.line, "%s: at most %d unless entries", what, rulesMaxUnless)
		}
		for _, u := range un {
			uf, err := fields(u, what+": unless")
			if err != nil {
				return rule{}, err
			}
			if _, err := yamlString(uf.get("rule"), what+": unless rule"); err != nil {
				return rule{}, err
			}
			offs, err := yamlList(uf.get("at"), what+": unless at")
			if err != nil {
				return rule{}, err
			}
			if len(offs) == 0 || len(offs) > rulesMaxUnlessAt {
				return rule{}, yamlError(u.line, "%s: unless at: 1 to %d offsets", what, rulesMaxUnlessAt)
			}
			e := ruleUnless{rule: -1}
			for _, o := range offs {
				v, err := yamlInt(o, what+": unless at", -rulesMaxUnlessOff, rulesMaxUnlessOff)
				if err != nil {
					return rule{}, err
				}
				if v%r.unit != 0 {
					return rule{}, yamlError(o.line, "%s: unless offsets must be multiples of unit", what)
				}
				e.at = append(e.at, v)
			}
			if err := uf.unknown(what + ": unless"); err != nil {
				return rule{}, err
			}
			ru.unless = append(ru.unless, e)
		}
		if ru.relative && len(ru.addrs) != 1 {
			return rule{}, yamlError(n.line, "%s: a relative rule has exactly one address", what)
		}
		if ru.relative && ru.signExt > totalBits(ru.addrs[0]) {
			return rule{}, yamlError(n.line, "%s: sign_extend is wider than the address", what)
		}
		if ru.require && (len(ru.addrs) == 0 || ru.relative) {
			return rule{}, yamlError(n.line, "%s: require needs an absolute address", what)
		}
	}
	if usedWords > rulesMaxWords {
		return rule{}, yamlError(n.line, "%s: uses more than %d words", what, rulesMaxWords)
	}
	ru.size = max(ru.size, usedWords*r.wordBytes)
	if ru.size > rulesMaxSize {
		return rule{}, yamlError(n.line, "%s: reaches more than %d bytes", what, rulesMaxSize)
	}
	if err := f.unknown(what); err != nil {
		return rule{}, err
	}
	return ru, nil
}

func totalBits(slices []ruleSlice) int {
	n := 0
	for _, s := range slices {
		n += s.bits()
	}
	return n
}

// slicesFromYAML parses "wN[hi:lo]" and fixed bits ("0", "1", "00"...);
// it returns the slices and how many words they use.
func (r *Rules) slicesFromYAML(items []*yamlNode, what string) ([]ruleSlice, int, error) {
	var out []ruleSlice
	words := 0
	for _, it := range items {
		s, err := yamlString(it, what)
		if err != nil {
			return nil, 0, err
		}
		if strings.Trim(s, "01") == "" {
			v, _ := strconv.ParseUint(s, 2, 32)
			out = append(out, ruleSlice{word: 0xFF, hi: len(s), lo: int(v), fixed: true})
			continue
		}
		var w, hi, lo int
		if n, err := fmt.Sscanf(s, "w%d[%d:%d]", &w, &hi, &lo); n != 3 || err != nil || fmt.Sprintf("w%d[%d:%d]", w, hi, lo) != s {
			return nil, 0, yamlError(it.line, "%s: %q: expected wN[hi:lo] or fixed bits like \"0\"", what, s)
		}
		if w < 0 || w >= rulesMaxWords || lo < 0 || hi < lo || hi >= 8*r.wordBytes {
			return nil, 0, yamlError(it.line, "%s: %q: bits outside a %d-bit word, or a word past w%d", what, s, 8*r.wordBytes, rulesMaxWords-1)
		}
		out = append(out, ruleSlice{word: w, hi: hi, lo: lo})
		words = max(words, w+1)
	}
	if len(out) == 0 || len(out) > rulesMaxSlices || totalBits(out) > 32 {
		return nil, 0, fmt.Errorf("%s: 1 to %d slices, at most 32 bits", what, rulesMaxSlices)
	}
	return out, words, nil
}

// ---- binary table --------------------------------------------------------

// Rule flags in the table.
const (
	ruleHasAddr  = 1 << 0
	ruleHasAddr2 = 1 << 1
	ruleRelative = 1 << 2
	ruleRequire  = 1 << 3
	ruleSpace    = 4 // shift of the 2-bit space
)

func (r *Rules) compile() []byte {
	b := []byte{rulesVersion, byte(r.unit), byte(r.wordBytes), 0, byte(r.sync)}
	if r.big {
		b[3] = 1
	}
	for l := range r.lists {
		b = append(b, byte(len(r.lists[l])))
	}
	for l := range r.lists {
		for _, ru := range r.lists[l] {
			flags := byte(ru.space) << ruleSpace
			if len(ru.addrs) > 0 {
				flags |= ruleHasAddr
			}
			if len(ru.addrs) > 1 {
				flags |= ruleHasAddr2
			}
			if ru.relative {
				flags |= ruleRelative
			}
			if ru.require {
				flags |= ruleRequire
			}
			b = append(b, flags, byte(ru.align), byte(ru.size), byte(ru.n), byte(len(ru.conds)))
			for _, c := range ru.conds {
				w := byte(c.word)
				if c.neg {
					w |= 0x80
				}
				b = append(b, w)
				b = r.appendWord(b, c.value)
				b = r.appendWord(b, c.mask)
			}
			for _, a := range ru.addrs {
				b = append(b, byte(len(a)))
				for _, s := range a {
					if s.fixed {
						b = append(b, 0xFF, byte(s.hi), byte(s.lo))
					} else {
						b = append(b, byte(s.word), byte(s.hi), byte(s.lo))
					}
				}
			}
			if ru.relative {
				b = append(b, byte(int8(ru.relBase)), byte(ru.signExt))
			}
			b = append(b, byte(len(ru.unless)))
			for _, u := range ru.unless {
				b = append(b, byte(u.rule), byte(len(u.at)))
				for _, o := range u.at {
					b = append(b, byte(int8(o)))
				}
			}
		}
	}
	return b
}

func (r *Rules) appendWord(b []byte, v uint32) []byte {
	for i := 0; i < r.wordBytes; i++ {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

var errRulesTable = errors.New("tinydelta: bad relocation rule table")

// ParseRulesTable reads and validates a binary table (Table), as the
// decoder does with a table shipped in a patch.
func ParseRulesTable(b []byte) (*Rules, error) {
	pos := 0
	get := func() (int, error) {
		if pos >= len(b) {
			return 0, errRulesTable
		}
		pos++
		return int(b[pos-1]), nil
	}
	bad := func(what string) error { return fmt.Errorf("%w: %s", errRulesTable, what) }
	var hdr [9]int
	for i := range hdr {
		v, err := get()
		if err != nil {
			return nil, err
		}
		hdr[i] = v
	}
	r := &Rules{unit: hdr[1], wordBytes: hdr[2], big: hdr[3]&1 != 0, sync: hdr[4], table: append([]byte{}, b...)}
	switch {
	case hdr[0] != rulesVersion:
		return nil, bad("version")
	case r.unit != 1 && r.unit != 2 && r.unit != 4:
		return nil, bad("unit")
	case r.wordBytes < 1 || r.wordBytes > 4:
		return nil, bad("word size")
	case hdr[3]&^1 != 0:
		return nil, bad("flags")
	case r.sync > rulesMaxSync:
		return nil, bad("sync")
	}
	for l := range r.lists {
		if hdr[5+l] > rulesMaxRules {
			return nil, bad("rule count")
		}
	}
	for l := range r.lists {
		for i := 0; i < hdr[5+l]; i++ {
			var ru rule
			var f [5]int
			for k := range f {
				v, err := get()
				if err != nil {
					return nil, err
				}
				f[k] = v
			}
			flags := f[0]
			ru.align, ru.size, ru.n = f[1], f[2], f[3]
			ru.relative, ru.require, ru.space = flags&ruleRelative != 0, flags&ruleRequire != 0, flags>>ruleSpace&3
			addrs := 0
			if flags&ruleHasAddr != 0 {
				addrs = 1
				if flags&ruleHasAddr2 != 0 {
					addrs = 2
				}
			} else if flags&ruleHasAddr2 != 0 {
				return nil, bad("address2 without address")
			}
			switch {
			case flags&^0x3F != 0 || ru.space > spaceData:
				return nil, bad("rule flags")
			case ru.align < r.unit || ru.align > 8 || ru.align&(ru.align-1) != 0 || ru.align%r.unit != 0:
				return nil, bad("align")
			case ru.size < 1 || ru.size > rulesMaxSize:
				return nil, bad("size")
			case f[4] > rulesMaxConds:
				return nil, bad("condition count")
			case l == rulePrefixes && (ru.n < 1 || ru.n > rulesMaxSkip || ru.n%r.unit != 0 || addrs > 0 || flags != 0):
				return nil, bad("prefix")
			case l == ruleLengths && (ru.n < 1 || ru.n > rulesMaxLength || addrs > 0 || flags != 0):
				return nil, bad("length")
			case l >= ruleCode && ru.n != 0:
				return nil, bad("code or data rule with a length")
			}
			words := 0
			for k := 0; k < f[4]; k++ {
				w, err := get()
				if err != nil {
					return nil, err
				}
				c := ruleCond{word: w & 0x7F, neg: w&0x80 != 0}
				if c.value, err = r.getWord(b, &pos); err != nil {
					return nil, err
				}
				if c.mask, err = r.getWord(b, &pos); err != nil {
					return nil, err
				}
				if c.word >= rulesMaxWords || c.value&^c.mask != 0 {
					return nil, bad("condition")
				}
				words = max(words, c.word+1)
				ru.condSize = max(ru.condSize, (c.word+1)*r.wordBytes)
				ru.conds = append(ru.conds, c)
			}
			for a := 0; a < addrs; a++ {
				n, err := get()
				if err != nil {
					return nil, err
				}
				if n < 1 || n > rulesMaxSlices {
					return nil, bad("slice count")
				}
				var sl []ruleSlice
				for k := 0; k < n; k++ {
					var v [3]int
					for j := range v {
						if v[j], err = get(); err != nil {
							return nil, err
						}
					}
					s := ruleSlice{word: v[0], hi: v[1], lo: v[2]}
					if v[0] == 0xFF {
						s.fixed = true
						if s.hi < 1 || s.hi > 8 || s.lo>>s.hi != 0 {
							return nil, bad("fixed bits")
						}
					} else if s.word >= rulesMaxWords || s.hi < s.lo || s.hi >= 8*r.wordBytes {
						return nil, bad("slice")
					} else {
						words = max(words, s.word+1)
					}
					sl = append(sl, s)
				}
				if totalBits(sl) > 32 {
					return nil, bad("address wider than 32 bits")
				}
				ru.addrs = append(ru.addrs, sl)
			}
			if ru.relative {
				bv, err := get()
				if err != nil {
					return nil, err
				}
				se, err := get()
				if err != nil {
					return nil, err
				}
				ru.relBase, ru.signExt = int(int8(bv)), se
				if addrs != 1 || se < 1 || se > totalBits(ru.addrs[0]) {
					return nil, bad("relative")
				}
			}
			if ru.require && (addrs == 0 || ru.relative) {
				return nil, bad("require")
			}
			nu, err := get()
			if err != nil {
				return nil, err
			}
			if nu > rulesMaxUnless || (nu > 0 && l < ruleCode) {
				return nil, bad("unless count")
			}
			for k := 0; k < nu; k++ {
				ri, err := get()
				if err != nil {
					return nil, err
				}
				na, err := get()
				if err != nil {
					return nil, err
				}
				if ri >= hdr[5+l] || na < 1 || na > rulesMaxUnlessAt {
					return nil, bad("unless")
				}
				u := ruleUnless{rule: ri}
				for j := 0; j < na; j++ {
					o, err := get()
					if err != nil {
						return nil, err
					}
					off := int(int8(o))
					if off < -rulesMaxUnlessOff || off > rulesMaxUnlessOff || off%r.unit != 0 {
						return nil, bad("unless offset")
					}
					u.at = append(u.at, off)
				}
				ru.unless = append(ru.unless, u)
			}
			if words*r.wordBytes > ru.size || words > rulesMaxWords {
				return nil, bad("rule reaches past its size")
			}
			r.reach = max(r.reach, ru.size-1)
			r.lists[l] = append(r.lists[l], ru)
		}
	}
	if pos != len(b) {
		return nil, bad("trailing bytes")
	}
	r.crc = crc16CCITT(b)
	return r, nil
}

func (r *Rules) getWord(b []byte, pos *int) (uint32, error) {
	if *pos+r.wordBytes > len(b) {
		return 0, errRulesTable
	}
	var v uint32
	for i := 0; i < r.wordBytes; i++ {
		v |= uint32(b[*pos+i]) << (8 * i)
	}
	*pos += r.wordBytes
	return v, nil
}

// crc16CCITT is CRC-16/CCITT-FALSE: polynomial 0x1021, initial 0xFFFF.
func crc16CCITT(b []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, c := range b {
		crc ^= uint16(c) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// ---- the engine ----------------------------------------------------------

// word reads word w of a rule at position p (the caller checked the bounds).
func (r *Rules) word(raw []byte, p, w int) uint32 {
	o := p + w*r.wordBytes
	var v uint32
	for i := 0; i < r.wordBytes; i++ {
		if r.big {
			v = v<<8 | uint32(raw[o+i])
		} else {
			v |= uint32(raw[o+i]) << (8 * i)
		}
	}
	return v
}

func (r *Rules) putWord(out []byte, p, w int, v uint32) {
	o := p + w*r.wordBytes
	for i := 0; i < r.wordBytes; i++ {
		if r.big {
			out[o+r.wordBytes-1-i] = byte(v >> (8 * i))
		} else {
			out[o+i] = byte(v >> (8 * i))
		}
	}
}

// join reads an address's slices from the words at p, most significant
// first.
func (r *Rules) join(raw []byte, p int, slices []ruleSlice) uint32 {
	var v uint32
	for _, s := range slices {
		var part uint32
		if s.fixed {
			part = uint32(s.lo)
		} else {
			part = r.word(raw, p, s.word) >> s.lo & (1<<s.bits() - 1)
		}
		v = v<<s.bits() | part
	}
	return v
}

// split writes v into an address's slices at p in out; false (nothing
// written) if v doesn't fit them or their fixed bits.
func (r *Rules) split(out []byte, p int, slices []ruleSlice, v uint64) bool {
	if v>>totalBits(slices) != 0 {
		return false
	}
	rest := v
	for i := len(slices) - 1; i >= 0; i-- { // fixed bits first: check before writing anything
		s := slices[i]
		if s.fixed && uint32(rest)&(1<<s.bits()-1) != uint32(s.lo) {
			return false
		}
		rest >>= s.bits()
	}
	for i := len(slices) - 1; i >= 0; i-- {
		s := slices[i]
		part := uint32(v) & (1<<s.bits() - 1)
		v >>= s.bits()
		if s.fixed {
			continue
		}
		mask := uint32(1<<s.bits()-1) << s.lo
		w := r.word(out, p, s.word)
		r.putWord(out, p, s.word, w&^mask|part<<s.lo)
	}
	return true
}

// matches reports whether ru's alignment and conditions hold at p (the
// words they read inside the image).
func (r *Rules) matches(ru *rule, raw []byte, base uint32, p int) bool {
	if p < 0 || p+ru.condSize > len(raw) || (base+uint32(p))%uint32(ru.align) != 0 {
		return false
	}
	for _, c := range ru.conds {
		if (r.word(raw, p, c.word)&c.mask == c.value) == c.neg {
			return false
		}
	}
	return true
}

func inImage(base uint32, n int, a uint64) bool {
	return a >= uint64(base) && a-uint64(base) < uint64(n)
}

// first is the index of the first rule of list l matching at p, -1 if none.
// A prefix or length rule must fit the image; a rule with require counts
// only if it fits and its address points into the image. A code or data
// rule that matches but doesn't fit (an instruction cut off by the end of
// the image) is still the one chosen; it then moves nothing.
func (r *Rules) first(l int, raw []byte, base uint32, p int) int {
	for i := range r.lists[l] {
		ru := &r.lists[l][i]
		if !r.matches(ru, raw, base, p) {
			continue
		}
		fits := p+ru.size <= len(raw)
		if (l < ruleCode || ru.require) && !fits {
			continue
		}
		if ru.require && !inImage(base, len(raw), uint64(r.join(raw, p, ru.addrs[0]))) {
			continue
		}
		return i
	}
	return -1
}

// length is the instruction length at q for sync: 0 outside the image, at
// a misaligned position or in a data run.
func (r *Rules) length(m *relocMap, raw []byte, q int) int {
	if q < 0 || q+r.wordBytes > len(raw) || (m.base+uint32(q))%uint32(r.unit) != 0 || m.inData(q) {
		return 0
	}
	skip := 0
	if i := r.first(rulePrefixes, raw, m.base, q); i >= 0 && q+r.lists[rulePrefixes][i].n+r.wordBytes <= len(raw) {
		skip = r.lists[rulePrefixes][i].n
	}
	n := r.unit
	if i := r.first(ruleLengths, raw, m.base, q+skip); i >= 0 {
		n = r.lists[ruleLengths][i].n
	}
	return skip + n
}

// starts reports whether p starts an instruction: decoding lengths from
// sync units before lands on it.
func (r *Rules) starts(m *relocMap, raw []byte, p int) bool {
	covered := uint32(0) // bit i: the unit i units on is inside an instruction
	for i := r.sync; i > 0; i-- {
		if covered&1 == 0 && p-i*r.unit >= 0 {
			if n := r.length(m, raw, p-i*r.unit) / r.unit; n > 0 {
				covered |= (1<<n - 1) &^ 1
			}
		}
		covered >>= 1
	}
	return covered&1 == 0
}

// applies is the list and rule that apply at p before unless (-1 if none
// or an exclusion): the first matching rule of the position's list, if it
// has an address and, in code, p starts an instruction.
func (r *Rules) applies(m *relocMap, raw []byte, p int) (int, int) {
	if p < 0 || p >= len(raw) || (m.base+uint32(p))%uint32(r.unit) != 0 {
		return 0, -1
	}
	l := ruleCode
	if m.inData(p) {
		l = ruleData
	}
	i := r.first(l, raw, m.base, p)
	if i < 0 || len(r.lists[l][i].addrs) == 0 || p+r.lists[l][i].size > len(raw) {
		return l, -1
	}
	if l == ruleCode && r.sync > 0 && !r.starts(m, raw, p) {
		return l, -1
	}
	return l, i
}

// ruleAt is the rule that moves addresses at p, after unless; nil if none.
func (r *Rules) ruleAt(m *relocMap, raw []byte, p int) *rule {
	l, i := r.applies(m, raw, p)
	if i < 0 {
		return nil
	}
	ru := &r.lists[l][i]
	for _, u := range ru.unless {
		for _, off := range u.at {
			if l2, i2 := r.applies(m, raw, p+off); l2 == l && i2 == u.rule {
				return nil
			}
		}
	}
	return ru
}

// shiftOf is the shift of address a in space.
func (m *relocMap) shiftOf(a int64, n int, space int) int64 {
	if a < 0 || a > 0xFFFFFFFF {
		return 0
	}
	if inImage(m.base, n, uint64(a)) {
		if space == spaceData {
			return 0
		}
		return m.shiftAt(a - int64(m.base))
	}
	if space == spaceCode {
		return 0
	}
	return entryShift(m.outside, a)
}

// moveAt rewrites in out the addresses of rule ru at p (read from raw).
func (r *Rules) moveAt(m *relocMap, ru *rule, raw, out []byte, p int) {
	n := len(raw)
	if ru.relative {
		bits := totalBits(ru.addrs[0])
		o := int64(int32(r.join(raw, p, ru.addrs[0])<<(32-ru.signExt)) >> (32 - ru.signExt))
		site := int64(m.base) + int64(p)
		next := o + m.shiftOf(site+int64(ru.relBase)+o, n, ru.space) - m.shiftOf(site, n, spaceCode)
		if next == o || next < -(1<<(ru.signExt-1)) || next >= 1<<(ru.signExt-1) {
			return
		}
		r.split(out, p, ru.addrs[0], uint64(next)&(1<<bits-1))
		return
	}
	for _, a := range ru.addrs {
		v := int64(r.join(raw, p, a))
		s := m.shiftOf(v, n, ru.space)
		if s == 0 || v+s < 0 {
			continue
		}
		r.split(out, p, a, uint64(v+s))
	}
}

// sameBesideAddresses reports whether ru identifies the same instruction
// at p in a and at q in b: equal in the bits its conditions test, but for
// address bits.
func (r *Rules) sameBesideAddresses(ru *rule, a []byte, p int, b []byte, q int) bool {
	for _, c := range ru.conds {
		mask := c.mask
		for _, sl := range ru.addrs {
			for _, s := range sl {
				if !s.fixed && s.word == c.word {
					mask &^= uint32(1<<s.bits()-1) << s.lo
				}
			}
		}
		if (r.word(a, p, c.word)^r.word(b, q, c.word))&mask != 0 {
			return false
		}
	}
	return true
}

// relocate returns the old image with every address the rules find moved
// through the map.
func (r *Rules) relocate(m *relocMap, raw []byte) []byte {
	out := append([]byte{}, raw...)
	for p := 0; p < len(raw); p++ {
		if ru := r.ruleAt(m, raw, p); ru != nil {
			r.moveAt(m, ru, raw, out, p)
		}
	}
	return out
}
