// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

// The hand-written Thumb and MSP430X rules of tinydelta 1.1, kept as the
// reference the rule tables (rules/*.yaml) must reproduce byte for byte
// (rules_test.go); with one change since: a relative field (a Thumb BL)
// moves only if its target is inside the image.

func legacyRelocate(m *relocMap, dict []byte) []byte {
	out := append([]byte{}, dict...)
	switch m.rules.builtin {
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
		if callee < 0 || callee >= int64(n) {
			continue // since tinydelta 1.2: data that looks like a BL calls far outside the image
		}
		s2 := m.addressShift(m.base+uint32(callee), n)
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
			// Only the address's own bits are written, so an overlapping
			// instruction's moved bits stay.
			cur := uint32(out[h]) | uint32(out[h+1])<<8
			cur = cur&^(0xF<<uint(f.shift)) | uint32(nv>>16)<<uint(f.shift)
			out[h], out[h+1] = byte(cur), byte(cur>>8)
			out[f.lo], out[f.lo+1] = byte(nv), byte(nv>>8)
		}
	}
}
