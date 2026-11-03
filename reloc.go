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
	arch    Relocation
	base    uint32
	entries []relocEntry
	data    []relocRun // MSP430X only
	// MSP430X only: addresses outside the old image that moved (RAM:
	// variables), start and length as addresses.
	outside []relocEntry
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

// appendHeader appends the map as the coded header carries it.
func (m *relocMap) appendHeader(b []byte) []byte {
	b = appendVarint(b, uint64(m.base))
	b = appendEntries(b, m.entries)
	if m.arch == RelocationMSP430X { // data runs, in blocks; addresses outside the image
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

// parseRelocMap reads a map from the header bytes b, returning how many
// bytes it took.
func parseRelocMap(arch Relocation, b []byte) (*relocMap, int, error) {
	pos := 0
	next := func() (uint64, error) {
		v, n := readVarint(b[pos:])
		if n < 0 || v > 0xFFFFFFFF {
			return 0, errRelocMap
		}
		pos += n
		return v, nil
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
	m := &relocMap{arch: arch, base: uint32(base)}
	if m.entries, err = entries(); err != nil {
		return nil, 0, err
	}
	if arch == RelocationMSP430X {
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
func relocMapFrom(dict, target []byte, arch Relocation, base uint32) *relocMap {
	m := relocMapFromCovers(findCovers(dict, target), len(dict), arch, base)
	// Refine: once the first map's addresses are relocated, the old image
	// lines up with the target in longer stretches, which show the code's
	// moves more finely. Keep the map under which most bytes match.
	best, bestScore := m, relocMatchScore(m.relocate(dict), target)
	for i := 0; i < relocRefinements; i++ {
		m = relocMapFromCovers(findCovers(m.relocate(dict), target), len(dict), arch, base)
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
func relocMapFromCovers(covers []cover, n int, arch Relocation, base uint32) *relocMap {
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
	return (&relocMap{arch: arch, base: base, entries: out}).filled(n)
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
	return &relocMap{arch: m.arch, base: m.base, entries: es, data: m.data, outside: m.outside}
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
	return (&relocMap{arch: m.arch, base: m.base, entries: es, data: m.data, outside: m.outside}).filled(int(end))
}

// withDataRuns picks the MSP430X data runs for an update: the blocks of
// the old image where relocating 4-byte data pointers makes more of the
// covered bytes match the target than the instruction rule does. Returns
// m with them (m itself if none).
func (m *relocMap) withDataRuns(dict, target []byte) *relocMap {
	if m.arch != RelocationMSP430X || len(dict) == 0 {
		return m
	}
	code := m.relocate(dict)
	all := &relocMap{arch: m.arch, base: m.base, entries: m.entries, data: []relocRun{{0, uint32((len(dict) + relocDataBlock - 1) / relocDataBlock)}}}
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
	return &relocMap{arch: m.arch, base: m.base, entries: m.entries, data: runs, outside: m.outside}
}

// withOutside adds, for MSP430X, the addresses outside the old image that
// moved (RAM: variables added or removed): per address the old image's
// aligned instructions hold, the shift most of them show in the target;
// then ranges of neighbouring addresses with the same shift, each reaching
// to the next range. Returns m itself if none moved.
func (m *relocMap) withOutside(dict, target []byte) *relocMap {
	if m.arch != RelocationMSP430X || len(dict) == 0 {
		return m
	}
	votes := map[uint32]map[int32]int{}
	word := func(img []byte, i int) uint32 { return uint32(img[i]) | uint32(img[i+1])<<8 }
	for _, c := range findCovers(m.relocate(dict), target) {
		if c.extra > 0 {
			continue
		}
		for k := 0; k+4 <= c.length; k += 2 {
			h, n := c.oldPos+k, c.newPos+k
			l, f, nf := msp430xValid(m, dict, m.base, h)
			if l == 0 || k+l > c.length {
				continue
			}
			mask := uint32(0xFFFF) // the first word without the fields' high bits
			for i := 0; i < nf; i++ {
				mask &^= 0xF << uint32(f[i].shift)
			}
			if word(dict, h)&mask != word(target, n)&mask {
				continue // another instruction
			}
			for i := 0; i < nf; i++ {
				hb := uint32(f[i].shift)
				oldAddr := word(dict, f[i].lo) | (word(dict, h)>>hb&0xF)<<16
				if oldAddr >= m.base && uint64(oldAddr-m.base) < uint64(len(dict)) {
					continue
				}
				newAddr := word(target, n+f[i].lo-h) | (word(target, n)>>hb&0xF)<<16
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
	return &relocMap{arch: m.arch, base: m.base, entries: m.entries, data: m.data, outside: out}
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
	out := append([]byte{}, dict...)
	switch m.arch {
	case RelocationThumb:
		relocateThumb(m, dict, out)
	case RelocationMSP430X:
		relocateMSP430X(m, dict, out)
	}
	return out
}

// ---- ARM Thumb ----------------------------------------------------------

// thumbBL reports a BL at h: a halfword 0xF000-0xF7FF followed by one
// 0xF800-0xFFFF. The two ranges don't overlap, so BLs can't overlap.
func thumbBL(raw []byte, base uint32, h int) bool {
	return h >= 0 && h+4 <= len(raw) && (base+uint32(h))&1 == 0 && raw[h+1]&0xF8 == 0xF0 && raw[h+3]&0xF8 == 0xF8
}

func relocateThumb(m *relocMap, raw, out []byte) {
	n := len(raw)
	for h := 0; h+4 <= n; h++ {
		if !thumbBL(raw, m.base, h) {
			continue
		}
		v := uint32(raw[h+1]&7)<<19 | uint32(raw[h])<<11 | uint32(raw[h+3]&7)<<8 | uint32(raw[h+2])
		off := int64(int32(v<<10) >> 9) // sign-extend 22 bits, times 2
		callee := int64(h) + 4 + off
		var s2 int64
		if callee >= 0 && callee < int64(n) {
			s2 = m.addressShift(m.base+uint32(callee), n)
		}
		next := off + s2 - m.addressShift(m.base+uint32(h), n)
		if next == off || next&1 != 0 || next < -(1<<22) || next >= 1<<22 {
			continue
		}
		w := uint32(next>>1) & 0x3FFFFF
		out[h+1] = 0xF0 | byte(w>>19&7)
		out[h] = byte(w >> 11)
		out[h+3] = 0xF8 | byte(w>>8&7)
		out[h+2] = byte(w)
	}
	for a := 0; a+4 <= n; a++ {
		if (m.base+uint32(a))&3 != 0 || thumbBL(raw, m.base, a-2) || thumbBL(raw, m.base, a) || thumbBL(raw, m.base, a+2) {
			continue
		}
		v := uint32(raw[a]) | uint32(raw[a+1])<<8 | uint32(raw[a+2])<<16 | uint32(raw[a+3])<<24
		if s := m.addressShift(v, n); s != 0 {
			v = uint32(int64(v) + s)
			out[a], out[a+1], out[a+2], out[a+3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
		}
	}
}

// ---- MSP430X ------------------------------------------------------------

// msp430xField is a 20-bit address in an instruction: its low 16 bits are
// the word at lo, its high 4 bits the opcode or extension word's bits
// shift+3..shift.
type msp430xField struct{ lo, shift int }

// msp430xCandidate decodes what looks like an instruction with 20-bit
// addresses at h: its length in bytes (0 if none) and its address fields.
func msp430xCandidate(m *relocMap, raw []byte, base uint32, h int) (int, [2]msp430xField, int) {
	var f [2]msp430xField
	if h < 0 || h+4 > len(raw) || (base+uint32(h))&1 != 0 || m.inData(h) {
		return 0, f, 0
	}
	w0 := uint32(raw[h]) | uint32(raw[h+1])<<8
	switch {
	case w0&0xFFF0 == 0x13B0 || w0&0xFFF0 == 0x1380: // CALLA #imm20 / &abs20
		f[0] = msp430xField{h + 2, 0}
		return 4, f, 1
	case w0&0xF0C0 == 0x0080: // MOVA, CMPA, ADDA, SUBA #imm20,Rd
		f[0] = msp430xField{h + 2, 8}
		return 4, f, 1
	case w0&0xF0F0 == 0x0020: // MOVA &abs20,Rd
		f[0] = msp430xField{h + 2, 8}
		return 4, f, 1
	case w0&0xF0F0 == 0x0060: // MOVA Rs,&abs20
		f[0] = msp430xField{h + 2, 0}
		return 4, f, 1
	case w0&0xF800 == 0x1800: // extension word
		w1 := uint32(raw[h+2]) | uint32(raw[h+3])<<8
		if w1 < 0x4000 { // only two-operand instructions follow
			return 0, f, 0
		}
		as, sreg, ad, dreg := w1>>4&3, w1>>8&0xF, w1>>7&1, w1&0xF
		p, k := h+4, 0
		if (as == 1 && sreg != 3) || (as == 3 && sreg == 0) { // a source word
			if !(as == 1 && sreg == 0) { // not PC-relative
				f[k] = msp430xField{p, 7}
				k++
			}
			p += 2
		}
		if ad == 1 {
			if dreg != 0 {
				f[k] = msp430xField{p, 0}
				k++
			}
			p += 2
		}
		if p > len(raw) {
			return 0, f, 0
		}
		return p - h, f, k
	}
	return 0, f, 0
}

// msp430xSyncWords: how many words before a candidate msp430xValid decodes
// to tell instructions from the operand words of instructions before.
const msp430xSyncWords = 8

// msp430xLength is the length in bytes of any instruction at h, if it is
// one (0 at odd positions and in data runs).
func msp430xLength(m *relocMap, raw []byte, base uint32, h int) int {
	if h < 0 || h+2 > len(raw) || (base+uint32(h))&1 != 0 || m.inData(h) {
		return 0
	}
	operand := func(as, reg uint32) int { // a source operand word
		if (as == 1 && reg != 3) || (as == 3 && reg == 0) {
			return 2
		}
		return 0
	}
	length, w := 2, uint32(raw[h])|uint32(raw[h+1])<<8
	if w&0xF800 == 0x1800 && h+4 <= len(raw) { // extension word
		length, w = 4, uint32(raw[h+2])|uint32(raw[h+3])<<8
	}
	switch {
	case w >= 0x4000: // two operands
		length += operand(w>>4&3, w>>8&0xF) + int(w>>6&2)
	case w < 0x1000: // MSP430X address instructions
		length += int(0x0FCC>>(w>>4&0xF)&1) * 2
	case w < 0x1300: // one operand
		length += operand(w>>4&3, w&0xF)
	case w < 0x1400: // RETI, CALLA
		length += int(0x0B20>>(w>>4&0xF)&1) * 2
	}
	return length
}

// msp430xValid: a candidate at h counts if decoding instructions from
// msp430xSyncWords words before reaches h as an instruction, not as an
// operand word of one before.
func msp430xValid(m *relocMap, raw []byte, base uint32, h int) (int, [2]msp430xField, int) {
	l, f, k := msp430xCandidate(m, raw, base, h)
	if l == 0 {
		return 0, f, 0
	}
	covered := uint32(0) // bit i: the word i words on is an operand
	for i := msp430xSyncWords; i > 0; i-- {
		if covered&1 == 0 {
			if n := msp430xLength(m, raw, base, h-2*i); n > 0 {
				covered |= (1<<(n>>1) - 1) &^ 1
			}
		}
		covered >>= 1
	}
	if covered&1 != 0 {
		return 0, f, 0
	}
	return l, f, k
}

// msp430xDataPointer reports what looks like a 4-byte data pointer at h,
// in a data run: high half 1-15, pointing into the image, and returns it.
func msp430xDataPointer(m *relocMap, raw []byte, h int) (uint32, bool) {
	if h < 0 || h+4 > len(raw) || (m.base+uint32(h))&1 != 0 || !m.inData(h) {
		return 0, false
	}
	v := uint32(raw[h]) | uint32(raw[h+1])<<8 | uint32(raw[h+2])<<16 | uint32(raw[h+3])<<24
	if v>>16 == 0 || v>>16 > 0xF || v < m.base || uint64(v-m.base) >= uint64(len(raw)) {
		return 0, false
	}
	return v, true
}

func relocateMSP430X(m *relocMap, raw, out []byte) {
	n := len(raw)
	for h := 0; h+4 <= n; h++ {
		if m.inData(h) {
			// A data pointer counts unless one 2 bytes before overlaps it.
			if v, ok := msp430xDataPointer(m, raw, h); ok {
				if _, before := msp430xDataPointer(m, raw, h-2); !before {
					if s := m.addressShift(v, n); s != 0 {
						if nv := int64(v) + s; nv >= 0 && nv < 1<<20 {
							out[h], out[h+1], out[h+2], out[h+3] = byte(nv), byte(nv>>8), byte(nv>>16), byte(nv>>24)
						}
					}
				}
			}
			continue
		}
		l, fields, k := msp430xValid(m, raw, m.base, h)
		if l == 0 {
			continue
		}
		w0 := uint32(raw[h]) | uint32(raw[h+1])<<8
		for _, f := range fields[:k] {
			lo := uint32(raw[f.lo]) | uint32(raw[f.lo+1])<<8
			v := (w0>>uint(f.shift)&0xF)<<16 | lo
			s := m.addressShift(v, n)
			if s == 0 {
				continue
			}
			nv := int64(v) + s
			if nv < 0 || nv >= 1<<20 {
				continue
			}
			w0 = w0&^(0xF<<uint(f.shift)) | uint32(nv>>16)<<uint(f.shift)
			out[f.lo], out[f.lo+1] = byte(nv), byte(nv>>8)
		}
		out[h], out[h+1] = byte(w0), byte(w0>>8)
	}
}

func (r Relocation) String() string {
	switch r {
	case RelocationNone:
		return "none"
	case RelocationThumb:
		return "thumb"
	case RelocationMSP430X:
		return "msp430x"
	}
	return fmt.Sprintf("Relocation(%d)", int(r))
}
