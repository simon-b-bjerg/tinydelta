// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"errors"
	"fmt"
	"sort"
)

// Relocation: firmware-aware coded patches.
//
// When code moves, every address that points into it changes: calls,
// branches and pointers all over the image, though nothing about them
// changed in the source. A plain or coded patch spends most of its bytes
// on these (where each one is, and its new bytes). With relocation the
// patch instead carries a map of how the old image moved, a few entries
// of (old start, length, shift), and decoder and encoder both rewrite the
// addresses in the old image through it before the patch applies. The
// patch is then made against the relocated old image and carries only
// what the map doesn't explain.
//
// Which bytes are addresses is decided by a rule per architecture
// (Relocation), by looking at the raw old image only and only a few bytes
// around each position, so the decoder can relocate any part of the old
// image it reads, in any order, and get the same bytes as the encoder.
// The rule doesn't have to be right about instructions: a wrong guess is
// corrected by the patch like any other difference, so it only costs
// size.
//
// In a coded patch's features byte, bits 2-3 name the rule; with a rule,
// the header goes on (before the stream length):
//
//	varint  base, the address of the old image's first byte
//	varint  N, the number of map entries
//	N times varint  start - the previous entry's end (0 for the first)
//	        varint  length
//	        varint  zigzag shift
//
// Entries are in order and don't overlap. A position in the old image
// moves by the shift of the entry containing it, 0 outside every entry.
// The decoder keeps the entries in RAM after the coder's tables,
// relocEntrySize bytes each.

// Relocation is an address-relocation rule: which bytes of a firmware
// image are addresses (EncodeOptions.Relocation).
type Relocation int

const (
	// RelocationNone: no relocation.
	RelocationNone Relocation = 0
	// RelocationThumb: ARM Thumb (Cortex-M): BL instructions (their
	// relative offset is adjusted by the callee's shift minus the call
	// site's) and 4-byte-aligned 32-bit pointers into the image.
	RelocationThumb Relocation = 1
	// RelocationMSP430X: MSP430X with 20-bit addresses (the large code
	// model): the address of CALLA #imm and &abs, MOVA, ADDA, CMPA and
	// SUBA #imm, MOVA &abs, and of the source and destination of
	// extended (MOVX, ADDX... with extension word) instructions in
	// immediate, absolute or indexed mode.
	RelocationMSP430X Relocation = 2
)

// relocEntrySize is the decoder RAM per map entry: start, length, shift.
const relocEntrySize = 12

// relocMinCover is the shortest cover that becomes a map entry.
const relocMinCover = 64

// MSP430X data runs: parts of the old image, in whole blocks of
// relocDataBlock bytes, where the rule looks for 4-byte data pointers
// instead of instructions (constant tables, initial values). The decoder
// keeps each run in relocRunSize bytes of RAM.
const (
	relocDataBlock = 256
	relocRunSize   = 8
)

// relocRun is a data run, in blocks of relocDataBlock bytes.
type relocRun struct{ start, length uint32 }

type relocEntry struct {
	start, length uint32
	shift         int32
}

type relocMap struct {
	rules   *Rules // which bytes are addresses
	ship    bool   // the patch carries the rule table (else names it by CRC)
	base    uint32
	entries []relocEntry
	data    []relocRun // in rule tables with data rules
	// Addresses outside the old image that moved (RAM: variables), start
	// and length as addresses.
	outside []relocEntry
}

// relocationRules is the features value of a patch relocated by a rule
// table (named by CRC, or shipped with feature bit 4).
const relocationRules Relocation = 3

// codedShippedRules is the features bit for a rule table in the patch.
const codedShippedRules = 0x10

// arch is the features value the patch carries: the built-in Thumb and
// MSP430X tables keep their numbers (1, 2), which the C decoders know.
func (m *relocMap) arch() Relocation {
	if m.rules.builtin != RelocationNone && !m.ship {
		return m.rules.builtin
	}
	return relocationRules
}

// carriesLists reports whether the header has data runs and RAM ranges
// (not with the built-in Thumb table's number).
func (m *relocMap) carriesLists() bool { return m.arch() != RelocationThumb }

// with returns a copy of m with other entries, data runs and RAM ranges.
func (m *relocMap) with(entries []relocEntry, data []relocRun, outside []relocEntry) *relocMap {
	return &relocMap{rules: m.rules, ship: m.ship, base: m.base, entries: entries, data: data, outside: outside}
}

// inData reports whether old-image offset h is in a data run.
func (m *relocMap) inData(h int) bool {
	b := int64(h) / relocDataBlock
	i := sort.Search(len(m.data), func(i int) bool { return int64(m.data[i].start)+int64(m.data[i].length) > b })
	return h >= 0 && i < len(m.data) && int64(m.data[i].start) <= b
}

// shiftAt is the shift of old-image offset o, 0 outside every entry.
func (m *relocMap) shiftAt(o int64) int64 { return entryShift(m.entries, o) }

// entryShift is the shift of the entry holding o, 0 if none.
func entryShift(es []relocEntry, o int64) int64 {
	i := sort.Search(len(es), func(i int) bool { return int64(es[i].start)+int64(es[i].length) > o })
	if i < len(es) && int64(es[i].start) <= o {
		return int64(es[i].shift)
	}
	return 0
}

// addressShift is the shift for an address, if it points into the old
// image of n bytes; 0 if not.
func (m *relocMap) addressShift(addr uint32, n int) int64 {
	if addr < m.base || uint64(addr-m.base) >= uint64(n) {
		return entryShift(m.outside, int64(addr))
	}
	return m.shiftAt(int64(addr - m.base))
}

func (m *relocMap) ramSize() int {
	return relocEntrySize*(len(m.entries)+len(m.outside)) + relocRunSize*len(m.data)
}

// appendHeader appends the rule table or its CRC (for a rule table that
// isn't a built-in one's number) and the map, as the coded header carries
// them after the features byte.
func (m *relocMap) appendHeader(b []byte) []byte {
	if m.arch() == relocationRules {
		if m.ship {
			b = appendVarint(b, uint64(len(m.rules.table)))
			b = append(b, m.rules.table...)
		} else {
			b = append(b, byte(m.rules.crc), byte(m.rules.crc>>8))
		}
	}
	b = appendVarint(b, uint64(m.base))
	b = appendEntries(b, m.entries)
	if m.carriesLists() { // data runs, in blocks; addresses outside the image
		b = appendVarint(b, uint64(len(m.data)))
		end := uint32(0)
		for _, r := range m.data {
			b = appendVarint(b, uint64(r.start-end))
			b = appendVarint(b, uint64(r.length))
			end = r.start + r.length
		}
		b = appendEntries(b, m.outside)
	}
	return b
}

// appendEntries writes a count, then per entry the gap since the previous
// one's end, its length and its shift (zigzag).
func appendEntries(b []byte, es []relocEntry) []byte {
	b = appendVarint(b, uint64(len(es)))
	end := uint32(0)
	for _, e := range es {
		b = appendVarint(b, uint64(e.start-end))
		b = appendVarint(b, uint64(e.length))
		b = appendVarint(b, zigzagEncode(int64(e.shift)))
		end = e.start + e.length
	}
	return b
}

var errRelocMap = errors.New("tinydelta: coded patch: bad relocation map")

// parseRelocMap reads the rule table or its CRC and the map from the
// header bytes b, returning how many bytes they took. A CRC names a
// built-in table or extra.
func parseRelocMap(arch Relocation, shipped bool, b []byte, extra *Rules) (*relocMap, int, error) {
	pos := 0
	next := func() (uint64, error) {
		v, n := readVarint(b[pos:])
		if n < 0 || v > 0xFFFFFFFF {
			return 0, errRelocMap
		}
		pos += n
		return v, nil
	}
	m := &relocMap{rules: rulesFor(arch), ship: shipped}
	if arch == relocationRules {
		if shipped {
			n, err := next()
			if err != nil || n > uint64(len(b)-pos) {
				return nil, 0, errRelocMap
			}
			r, err := ParseRulesTable(b[pos : pos+int(n)])
			if err != nil {
				return nil, 0, err
			}
			m.rules, pos = r, pos+int(n)
		} else {
			if len(b) < 2 {
				return nil, 0, errRelocMap
			}
			crc := uint16(b[0]) | uint16(b[1])<<8
			pos = 2
			for _, r := range []*Rules{extra, builtinThumb, builtinMSP430X} {
				if r != nil && r.crc == crc {
					m.rules = r
					break
				}
			}
			if m.rules == nil {
				return nil, 0, fmt.Errorf("%w: patch needs the relocation rules with CRC %04x", ErrUnsupportedVersion, crc)
			}
		}
	}
	base, err := next()
	if err != nil {
		return nil, 0, err
	}
	entries := func() ([]relocEntry, error) {
		count, err := next()
		if err != nil || count > uint64(len(b)) { // each entry takes at least 3 bytes
			return nil, errRelocMap
		}
		var es []relocEntry
		end := uint64(0)
		for i := uint64(0); i < count; i++ {
			gap, err1 := next()
			length, err2 := next()
			z, err3 := next()
			if err1 != nil || err2 != nil || err3 != nil {
				return nil, errRelocMap
			}
			start := end + gap
			if start+length > 0xFFFFFFFF {
				return nil, errRelocMap
			}
			es = append(es, relocEntry{uint32(start), uint32(length), int32(zigzagDecode(z))})
			end = start + length
		}
		return es, nil
	}
	m.base = uint32(base)
	if m.entries, err = entries(); err != nil {
		return nil, 0, err
	}
	if m.carriesLists() {
		runs, err := next()
		if err != nil || runs > uint64(len(b)) {
			return nil, 0, errRelocMap
		}
		end := uint64(0)
		for i := uint64(0); i < runs; i++ {
			gap, err1 := next()
			length, err2 := next()
			if err1 != nil || err2 != nil {
				return nil, 0, errRelocMap
			}
			start := end + gap
			if start+length > 0xFFFFFFFF {
				return nil, 0, errRelocMap
			}
			m.data = append(m.data, relocRun{uint32(start), uint32(length)})
			end = start + length
		}
		if m.outside, err = entries(); err != nil {
			return nil, 0, err
		}
	}
	return m, pos, nil
}

// relocMapFrom builds the map for an update from the covers: each cover
// of at least relocMinCover bytes that moved becomes an entry (trimmed
// where old ranges overlap; neighbours with the same shift merged).
func relocMapFrom(dict, target []byte, rules *Rules, base uint32) *relocMap {
	m := relocMapFromCovers(findCovers(dict, target), len(dict), rules, base)
	// Refine: once the first map's addresses are relocated, the old image
	// lines up with the target in longer stretches, which show the code's
	// moves more finely. Keep the map under which most bytes match.
	best, bestScore := m, relocMatchScore(m.relocate(dict), target)
	for i := 0; i < relocRefinements; i++ {
		m = relocMapFromCovers(findCovers(m.relocate(dict), target), len(dict), rules, base)
		if score := relocMatchScore(m.relocate(dict), target); score > bestScore {
			best, bestScore = m, score
		}
	}
	return best
}

// relocRefinements: how many times relocMapFrom rebuilds the map from the
// image relocated through the previous one.
const relocRefinements = 3

// relocMatchScore counts the old image's bytes that match the target once
// aligned with it.
func relocMatchScore(rel, target []byte) int {
	n := 0
	for _, c := range findCovers(rel, target) {
		for k := 0; k < c.length && c.extra == 0; k++ {
			if rel[c.oldPos+k] == target[c.newPos+k] {
				n++
			}
		}
	}
	return n
}

// relocMapFromCovers makes a map from the covers of dict (n bytes) that
// moved.
func relocMapFromCovers(covers []cover, n int, rules *Rules, base uint32) *relocMap {
	var es []relocEntry
	for _, c := range covers {
		if c.extra == 0 && c.length >= relocMinCover && c.newPos != c.oldPos {
			es = append(es, relocEntry{uint32(c.oldPos), uint32(c.length), int32(c.newPos - c.oldPos)})
		}
	}
	sort.Slice(es, func(i, j int) bool { return es[i].start < es[j].start })
	var out []relocEntry
	for _, e := range es {
		if len(out) > 0 {
			last := &out[len(out)-1]
			end := last.start + last.length
			if e.start < end { // overlaps the previous: keep only the rest
				if e.start+e.length <= end {
					continue
				}
				e.length -= end - e.start
				e.start = end
			}
			if e.start == end && e.shift == last.shift {
				last.length += e.length
				continue
			}
		}
		out = append(out, e)
	}
	return (&relocMap{rules: rules, base: base, entries: out}).filled(n)
}

// filled makes the entries contiguous: each reaches to the next one's
// start, the last to the end of the image (n bytes), as code between two
// regions that moved has moved too. Neighbours with the same shift merge.
func (m *relocMap) filled(n int) *relocMap {
	var es []relocEntry
	for i, e := range m.entries {
		end := uint32(n)
		if i+1 < len(m.entries) {
			end = m.entries[i+1].start
		}
		if end > e.start {
			e.length = end - e.start
		}
		if len(es) > 0 && es[len(es)-1].shift == e.shift && es[len(es)-1].start+es[len(es)-1].length == e.start {
			es[len(es)-1].length += e.length
			continue
		}
		es = append(es, e)
	}
	return m.with(es, m.data, m.outside)
}

// limitTo keeps the longest entries that fit ram bytes of decoder RAM,
// after the data runs and the entries outside the image (all kept; nil if
// they don't fit), and stretches
// them over the ones dropped.
func (m *relocMap) limitTo(ram int) *relocMap {
	ram -= relocRunSize*len(m.data) + relocEntrySize*len(m.outside)
	if ram < 0 {
		return nil
	}
	n := ram / relocEntrySize
	if n >= len(m.entries) {
		return m
	}
	es := append([]relocEntry{}, m.entries...)
	sort.SliceStable(es, func(i, j int) bool { return es[i].length > es[j].length })
	es = es[:max(n, 0)]
	sort.Slice(es, func(i, j int) bool { return es[i].start < es[j].start })
	end := uint32(0)
	if len(m.entries) > 0 {
		last := m.entries[len(m.entries)-1]
		end = last.start + last.length
	}
	return m.with(es, m.data, m.outside).filled(int(end))
}

// withDataRuns picks the data runs for an update, for rules with data
// rules: the blocks of the old image where the data rules make more of
// the covered bytes match the target than the code rules do. Returns m
// with them (m itself if none).
func (m *relocMap) withDataRuns(dict, target []byte) *relocMap {
	if !m.carriesLists() || len(m.rules.lists[ruleData]) == 0 || len(dict) == 0 {
		return m
	}
	code := m.relocate(dict)
	all := m.with(m.entries, []relocRun{{0, uint32((len(dict) + relocDataBlock - 1) / relocDataBlock)}}, nil)
	data := all.relocate(dict)
	// Per block, how many old bytes match the target once each version is
	// aligned with it on its own (a table whose every pointer moved only
	// lines up once relocated).
	score := make([]int, (len(dict)+relocDataBlock-1)/relocDataBlock) // data matches minus code matches
	for sign, rel := range map[int][]byte{1: data, -1: code} {
		for _, c := range findCovers(rel, target) {
			if c.extra > 0 {
				continue
			}
			for k := 0; k < c.length; k++ {
				if o := c.oldPos + k; rel[o] == target[c.newPos+k] {
					score[o/relocDataBlock] += sign
				}
			}
		}
	}
	var runs []relocRun
	for b, s := range score {
		if s <= 2 { // a few bytes don't pay for a run
			continue
		}
		if len(runs) > 0 && runs[len(runs)-1].start+runs[len(runs)-1].length == uint32(b) {
			runs[len(runs)-1].length++
			continue
		}
		runs = append(runs, relocRun{uint32(b), 1})
	}
	if len(runs) == 0 {
		return m
	}
	return m.with(m.entries, runs, m.outside)
}

// withOutside adds the addresses outside the old image that moved (RAM:
// variables added or removed): per address the old image's aligned
// instructions hold, the shift most of them show in the target; then
// ranges of neighbouring addresses with the same shift, each reaching to
// the next range. Returns m itself if none moved.
func (m *relocMap) withOutside(dict, target []byte) *relocMap {
	if !m.carriesLists() || len(dict) == 0 {
		return m
	}
	r := m.rules
	votes := map[uint32]map[int32]int{}
	for _, c := range findCovers(m.relocate(dict), target) {
		if c.extra > 0 {
			continue
		}
		// Every unit from the cover's start: a cover that starts mid-word
		// (its first bytes differ) gives no votes, which measured better.
		for k := 0; k < c.length; k += r.unit {
			p, n := c.oldPos+k, c.newPos+k
			ru := r.ruleAt(m, dict, p)
			if ru == nil || ru.relative || ru.space == spaceCode || k+ru.size > c.length {
				continue
			}
			if !r.sameBesideAddresses(ru, dict, p, target, n) {
				continue // another instruction
			}
			for _, a := range ru.addrs {
				oldAddr := r.join(dict, p, a)
				if inImage(m.base, len(dict), uint64(oldAddr)) {
					continue
				}
				newAddr := r.join(target, n, a)
				if votes[oldAddr] == nil {
					votes[oldAddr] = map[int32]int{}
				}
				votes[oldAddr][int32(newAddr)-int32(oldAddr)]++
			}
		}
	}
	addrs := make([]uint32, 0, len(votes))
	for a := range votes {
		addrs = append(addrs, a)
	}
	sort.Slice(addrs, func(i, j int) bool { return addrs[i] < addrs[j] })
	type level struct {
		start uint32
		shift int32
	}
	var levels []level // where the shift changes
	for _, a := range addrs {
		var shift int32
		best := 0
		for s, v := range votes[a] {
			if v > best || (v == best && abs64(int64(s)) < abs64(int64(shift))) {
				shift, best = s, v
			}
		}
		if len(levels) == 0 || levels[len(levels)-1].shift != shift {
			levels = append(levels, level{a, shift})
		}
	}
	var out []relocEntry
	for i, lv := range levels {
		if lv.shift == 0 {
			continue
		}
		end := addrs[len(addrs)-1] + 2
		if i+1 < len(levels) {
			end = levels[i+1].start
		}
		if lv.start < m.base && end > m.base { // stay outside the image
			end = m.base
		}
		out = append(out, relocEntry{lv.start, end - lv.start, lv.shift})
	}
	if len(out) == 0 {
		return m
	}
	return m.with(m.entries, m.data, out)
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// relocate returns the old image as the decoder sees it: every address
// the rule finds moved through the map. dict is not changed.
func (m *relocMap) relocate(dict []byte) []byte {
	return m.rules.relocate(m, dict)
}

func (r Relocation) String() string {
	switch r {
	case RelocationNone:
		return "none"
	case RelocationThumb:
		return "thumb"
	case RelocationMSP430X:
		return "msp430x"
	case relocationRules:
		return "rules"
	}
	return fmt.Sprintf("Relocation(%d)", int(r))
}
