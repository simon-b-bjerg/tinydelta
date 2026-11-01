// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"errors"
	"fmt"
)

// Coded patches: the instruction stream of a version-1 or version-2 patch,
// range-coded. The instructions and their meaning are unchanged; only how
// their bytes are stored differs. Layout:
//
//	byte 0     0x80 | instruction set (0x81: version 1's, 0x82: version 2's)
//	byte 1     features: bits 0-1 the table layout (codedLayoutC/B/A), bits
//	           2-7 reserved, 0
//	varint     n, the length of the uncoded instruction stream in bytes
//	rest       the range-coded instruction stream
//
// Every bit of every instruction-stream byte is coded with an adaptive
// probability: a binary range coder (LZMA's, with 12-bit probabilities)
// whose probabilities are 1-byte states, as in H.264's CABAC: 63 states,
// each standing for a probability of the less likely bit (codedLPS), plus
// which bit is the more likely one. Encoder and decoder start every state
// at 0 (even odds) and update it the same way after each bit, so nothing
// about the model is sent.
//
// Which state codes a bit depends on the field the byte belongs to (its
// context), so each field's typical values become cheap:
//
//   - The first byte of an instruction varint: its two opcode bits with
//     the previous instruction's opcode as context (codedPrevStart before
//     the first), then its six other bits with this opcode as context.
//   - Every other byte: a bit tree (each bit's state chosen by the bits of
//     the byte so far, highest bit first) in one of up to four tables:
//     ADD literals, ADD_DIFF bytes, the first byte of a COPY or
//     COPY_TARGET delta, and the later bytes of any varint. Layout A
//     gives each its own table; layout B shares one table between all but
//     ADD_DIFF; layout C shares one table between all four.
//
// The decoder needs the states in RAM: CodedTablesSize bytes for a
// layout, for as long as it decodes. A smaller layout saves RAM and costs
// some patch size.

// Table layouts, as in the features byte.
const (
	codedLayoutC = 1 // one shared byte table
	codedLayoutB = 2 // ADD_DIFF bytes, and one table for the rest
	codedLayoutA = 3 // ADD literals, ADD_DIFF bytes, first delta bytes, later varint bytes
)

const (
	codedFlag      = 0x80
	codedPrevStart = 4  // the "previous opcode" before the first instruction
	codedOpStates  = 15 // 5 previous opcodes x 3 tree nodes
	codedLenStates = 4 * 63
	codedTreeSize  = 255
	codedProbBits  = 12
	codedTopValue  = 1 << 24
	codedStates    = 63
)

// codedLPS is each state's probability of the less likely bit, out of
// 1<<codedProbBits: 0.5 at state 0 down to about 0.02, by a constant
// factor per state.
var codedLPS = [codedStates]uint16{
	2048, 1944, 1845, 1752, 1663, 1578, 1498, 1422, 1350, 1281, 1216, 1154, 1096, 1040, 987, 937,
	890, 844, 802, 761, 722, 685, 651, 618, 586, 557, 528, 501, 476, 452, 429, 407,
	386, 367, 348, 330, 314, 298, 283, 268, 255, 242, 229, 218, 207, 196, 186, 177,
	168, 159, 151, 144, 136, 129, 123, 117, 111, 105, 100, 95, 90, 85, 81,
}

// codedNextLPS is the state after the less likely bit. After the more
// likely bit, state s becomes min(s+1, codedStates-1).
var codedNextLPS = [codedStates]uint8{
	0, 0, 1, 2, 3, 4, 4, 5, 6, 7, 8, 9, 10, 10, 11, 12,
	13, 14, 14, 15, 16, 17, 17, 18, 19, 20, 20, 21, 22, 22, 23, 24,
	24, 25, 26, 26, 27, 27, 28, 29, 29, 30, 30, 31, 31, 32, 32, 33,
	33, 33, 34, 34, 35, 35, 35, 36, 36, 36, 37, 37, 37, 38, 38,
}

// codedByteTables is how many byte tables a layout has.
func codedByteTables(layout int) int {
	switch layout {
	case codedLayoutC:
		return 1
	case codedLayoutB:
		return 2
	case codedLayoutA:
		return 4
	}
	return 0
}

// CodedTablesSize is the RAM, in bytes, a decoder needs for the
// probability states of a coded patch's table layout: 1287 for layout A,
// 777 for B, 522 for C (the features byte names the layout). 0 for an
// unknown layout.
func CodedTablesSize(layout int) int {
	n := codedByteTables(layout)
	if n == 0 {
		return 0
	}
	return codedOpStates + codedLenStates + n*codedTreeSize
}

// Kinds of instruction-stream bytes, the contexts beyond the first byte of
// an instruction.
const (
	fieldLiteral = iota
	fieldDiff
	fieldDelta      // first byte of a COPY/COPY_TARGET delta
	fieldVarintCont // later bytes of any varint
)

// codedTable is the byte table a field uses in a layout: its index among
// the byte tables.
func codedTable(layout, field int) int {
	switch layout {
	case codedLayoutA:
		return field
	case codedLayoutB:
		if field == fieldDiff {
			return 1
		}
		return 0
	}
	return 0
}

// codedModel is the probability states of one coded patch: the opcode
// tree, the length tree and the byte tables, one byte per state (the
// state number times 2, plus the more likely bit).
type codedModel struct {
	layout int
	states []byte
}

func newCodedModel(layout int) *codedModel {
	return &codedModel{layout: layout, states: make([]byte, CodedTablesSize(layout))}
}

// opState, lenState and byteState index states: node is the bit-tree node,
// 1 for a tree's first bit, then 2*node + bit.
func (m *codedModel) opState(prev, node int) int { return prev*3 + node - 1 }
func (m *codedModel) lenState(op, node int) int  { return codedOpStates + op*63 + node - 1 }
func (m *codedModel) byteState(field, node int) int {
	return codedOpStates + codedLenStates + codedTable(m.layout, field)*codedTreeSize + node - 1
}

// prob0 is the probability that the next bit at state index i is 0, out of
// 1<<codedProbBits.
func (m *codedModel) prob0(i int) uint32 {
	s := m.states[i]
	lps := uint32(codedLPS[s>>1])
	if s&1 == 0 { // 0 is the more likely bit
		return 1<<codedProbBits - lps
	}
	return lps
}

// update moves state i after bit.
func (m *codedModel) update(i int, bit uint32) {
	s := m.states[i]
	state, mps := s>>1, s&1
	if byte(bit) == mps {
		state = min(state+1, codedStates-1)
	} else {
		if state == 0 {
			mps ^= 1
		}
		state = codedNextLPS[state]
	}
	m.states[i] = state<<1 | mps
}

// rangeEncoder is LZMA's range encoder, carry handling included. Its first
// output byte is always 0 and is not stored.
type rangeEncoder struct {
	low       uint64
	rng       uint32
	cache     byte
	cacheSize int
	out       []byte
	started   bool
}

func newRangeEncoder() *rangeEncoder { return &rangeEncoder{rng: 0xFFFFFFFF, cacheSize: 1} }

func (e *rangeEncoder) emit(b byte) {
	if !e.started {
		e.started = true // the leading 0 (see rangeEncoder)
		return
	}
	e.out = append(e.out, b)
}

func (e *rangeEncoder) shiftLow() {
	if e.low < 0xFF000000 || e.low >= 1<<32 {
		carry := byte(e.low >> 32)
		b := e.cache
		for {
			e.emit(b + carry)
			b = 0xFF
			e.cacheSize--
			if e.cacheSize == 0 {
				break
			}
		}
		e.cache = byte(e.low >> 24)
	}
	e.cacheSize++
	e.low = (e.low << 8) & 0xFFFFFFFF
}

func (e *rangeEncoder) bit(m *codedModel, i int, bit uint32) {
	bound := (e.rng >> codedProbBits) * m.prob0(i)
	if bit == 0 {
		e.rng = bound
	} else {
		e.low += uint64(bound)
		e.rng -= bound
	}
	m.update(i, bit)
	for e.rng < codedTopValue {
		e.rng <<= 8
		e.shiftLow()
	}
}

func (e *rangeEncoder) finish() []byte {
	for i := 0; i < 5; i++ {
		e.shiftLow()
	}
	return e.out
}

// rangeDecoder mirrors rangeEncoder over data.
type rangeDecoder struct {
	data []byte
	pos  int
	rng  uint32
	code uint32
}

var errCodedTruncated = errors.New("coded stream ends early")

func newRangeDecoder(data []byte) (*rangeDecoder, error) {
	if len(data) < 4 {
		return nil, errCodedTruncated
	}
	d := &rangeDecoder{data: data, pos: 4, rng: 0xFFFFFFFF}
	d.code = uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
	return d, nil
}

func (d *rangeDecoder) bit(m *codedModel, i int) (uint32, error) {
	bound := (d.rng >> codedProbBits) * m.prob0(i)
	var bit uint32
	if d.code < bound {
		d.rng = bound
	} else {
		d.code -= bound
		d.rng -= bound
		bit = 1
	}
	m.update(i, bit)
	for d.rng < codedTopValue {
		if d.pos >= len(d.data) {
			return 0, errCodedTruncated
		}
		d.rng <<= 8
		d.code = d.code<<8 | uint32(d.data[d.pos])
		d.pos++
	}
	return bit, nil
}

// codedWalker walks an instruction stream byte by byte, telling which
// context each byte belongs to. The encoder feeds it the stream; the
// decoder feeds it each byte as decoded, so both see the same contexts.
type codedWalker struct {
	version   byte
	prevOp    int
	inVarint  bool // inside a varint; next byte is a later byte
	isIns     bool // that varint is an instruction
	varint    uint64
	shift     uint
	payload   uint64 // payload bytes still to come
	payField  int
	wantDelta bool // the instruction just read takes a delta varint
}

func newCodedWalker(version byte) *codedWalker {
	return &codedWalker{version: version, prevOp: codedPrevStart}
}

// next says how the next byte is coded: first is true for the first byte
// of an instruction, else field names its byte table.
func (w *codedWalker) next() (first bool, field int) {
	switch {
	case w.payload > 0:
		return false, w.payField
	case w.inVarint:
		return false, fieldVarintCont
	case w.wantDelta:
		return false, fieldDelta
	}
	return true, 0
}

// push records the byte just coded.
func (w *codedWalker) push(b byte) error {
	if w.payload > 0 {
		w.payload--
		return nil
	}
	if !w.inVarint {
		w.isIns = !w.wantDelta
		w.wantDelta = false
		w.varint, w.shift = 0, 0
	}
	if w.shift == 28 && b > 0x0F {
		return errors.New("varint over 32 bits")
	}
	w.varint |= uint64(b&0x7F) << w.shift
	w.shift += 7
	w.inVarint = b&0x80 != 0
	if w.inVarint || !w.isIns {
		return nil
	}
	op, n := int(w.varint&3), w.varint>>2
	w.prevOp = op
	switch {
	case op == opCopy || (op == opTargetCopy && w.version == FormatVersion):
		w.wantDelta = true
	case op == opAdd:
		w.payload, w.payField = n, fieldLiteral
	case op == opAddDiff: // version 2
		w.payload, w.payField = n, fieldDiff
	}
	return nil
}

// codeByte codes or decodes one byte with the walker's context; bitFn
// codes or decodes one bit at a state index, given the bit to code (the
// encoder) or ignoring it (the decoder), and returns the bit.
func codeByte(m *codedModel, w *codedWalker, b byte, bitFn func(i int, bit uint32) (uint32, error)) (byte, error) {
	first, field := w.next()
	var out uint32
	if first {
		node := 1
		for _, sh := range []uint{1, 0} {
			bit, err := bitFn(m.opState(w.prevOp, node), uint32(b>>sh)&1)
			if err != nil {
				return 0, err
			}
			out |= bit << sh
			node = node*2 + int(bit)
		}
		op := int(out)
		node = 1
		for sh := 7; sh >= 2; sh-- {
			bit, err := bitFn(m.lenState(op, node), uint32(b>>sh)&1)
			if err != nil {
				return 0, err
			}
			out |= bit << sh
			node = node*2 + int(bit)
		}
	} else {
		node := 1
		for sh := 7; sh >= 0; sh-- {
			bit, err := bitFn(m.byteState(field, node), uint32(b>>sh)&1)
			if err != nil {
				return 0, err
			}
			out |= bit << sh
			node = node*2 + int(bit)
		}
	}
	return byte(out), w.push(byte(out))
}

// encodeCoded range-codes plain, a version-1 or version-2 patch, with a
// table layout.
func encodeCoded(plain []byte, layout int) []byte {
	version, stream := plain[0], plain[headerLen:]
	m, w, e := newCodedModel(layout), newCodedWalker(version), newRangeEncoder()
	for _, b := range stream {
		codeByte(m, w, b, func(i int, bit uint32) (uint32, error) { e.bit(m, i, bit); return bit, nil })
	}
	out := []byte{codedFlag | version, byte(layout)}
	out = appendVarint(out, uint64(len(stream)))
	return append(out, e.finish()...)
}

// decodeCoded turns a coded patch back into the plain patch it codes.
// maxStream caps the uncoded instruction stream's length.
func decodeCoded(patch []byte, maxStream int) ([]byte, error) {
	if len(patch) < 2 {
		return nil, errors.New("tinydelta: coded patch has no features byte")
	}
	version, features := patch[0]&^codedFlag, int(patch[1])
	layout := features & 3
	if features&^3 != 0 || layout == 0 {
		return nil, fmt.Errorf("%w: coded patch features 0x%02x", ErrUnsupportedVersion, features)
	}
	n, used := readVarint(patch[2:])
	if used < 0 {
		return nil, errors.New("tinydelta: coded patch: bad stream length")
	}
	if n > uint64(maxStream) {
		return nil, fmt.Errorf("%w: coded patch's instruction stream is %d bytes", ErrTooLarge, n)
	}
	d, err := newRangeDecoder(patch[2+used:])
	if err != nil {
		return nil, fmt.Errorf("tinydelta: coded patch: %w", err)
	}
	m, w := newCodedModel(layout), newCodedWalker(version)
	plain := make([]byte, 0, headerLen+min(int(n), 1<<20))
	plain = append(plain, version)
	for i := uint64(0); i < n; i++ {
		b, err := codeByte(m, w, 0, func(i int, _ uint32) (uint32, error) { return d.bit(m, i) })
		if err != nil {
			return nil, fmt.Errorf("tinydelta: coded patch: %w", err)
		}
		plain = append(plain, b)
	}
	return plain, nil
}

// codedLayoutsWithin lists the table layouts whose states fit in ram
// bytes, largest first.
func codedLayoutsWithin(ram int) []int {
	var layouts []int
	for _, l := range []int{codedLayoutA, codedLayoutB, codedLayoutC} {
		if CodedTablesSize(l) <= ram {
			layouts = append(layouts, l)
		}
	}
	return layouts
}
