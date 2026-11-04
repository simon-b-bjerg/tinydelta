// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

//go:build ideas

package tinydelta

// Experiments (docs/ideas.md): relocating the dictionary with the shift
// map the covers describe, folded covers with a sparse model, and how many
// literals COPY_TARGET could reach. Pairs files as in ideas_test.go;
// TestIdeasRelocateMSP430X reads its MSP430X pairs from -config.

import (
	"fmt"
	"testing"
)

// codeSparse codes ADD_DIFF bytes as: "zero?" (state by lane and how long
// the current zero run is, bucketed), then "same as the last nonzero byte
// in this lane?", then the byte. For covers folded into one ADD_DIFF.
func codeSparse(m *ideaModel, toks []ideaToken, width int) {
	var last [4]byte
	run := 0
	for _, t := range toks {
		switch t.kind {
		case tokFirst:
			m.tree(100+t.prevOp, 2, uint32(t.b&3))
			m.tree(200+int(t.b&3), 6, uint32(t.b>>2))
		case tokDiff:
			lane := t.pos & (width - 1)
			bucket := 0
			switch {
			case run == 0:
				bucket = 0
			case run < 4:
				bucket = 1
			case run < 16:
				bucket = 2
			case run < 64:
				bucket = 3
			default:
				bucket = 4
			}
			if t.b == 0 {
				m.bit(50, lane*8+bucket, 1)
				run++
				continue
			}
			m.bit(50, lane*8+bucket, 0)
			run = 0
			if t.b == last[lane] {
				m.bit(51, lane, 1)
			} else {
				m.bit(51, lane, 0)
				m.tree(1, 8, uint32(t.b))
			}
			last[lane] = t.b
		default:
			m.tree(0, 8, uint32(t.b))
		}
	}
}

func TestIdeasSmash(t *testing.T) {
	for _, set := range []struct {
		name, path string
		width      int
	}{{"ARM", *ideasFirmware, 4}, {"MSP430X", *ideasConfig, 2}} {
		pairs := loadIdeaPairs(t, set.path)
		if len(pairs) == 0 {
			continue
		}
		var base, pred int
		folded := map[int]int{}
		var litTotal, litReach int
		for _, p := range pairs {
			v2 := withVersion(FormatVersion2, encodeCovers(p.dict, p.target, MaxSize))
			tk := ideaTokens(v2, p.dict)
			a, b := newIdeaModel(nil), newIdeaModel(nil)
			codeBaseline(a, tk)
			codePredictedLanes(b, tk, set.width)
			base += a.bytes()
			pred += b.bytes()
			for _, fold := range []int{8, 32, 128, 1 << 30} {
				f := encodeCoversFold(p.dict, p.target, fold)
				m := newIdeaModel(nil)
				codeSparse(m, ideaTokens(f, p.dict), set.width)
				folded[fold] += m.bytes()
			}
			// Literals COPY_TARGET could reach: bytes of ADD runs inside a
			// match of at least 6 bytes with earlier target.
			idx := map[string][]int{}
			for _, x := range tk {
				if x.kind != tokLiteral || x.index != 0 {
					continue
				}
			}
			// index every 6-byte window of the target before each literal
			const k = 6
			for i := 0; i+k <= len(p.target); i++ {
				idx[string(p.target[i:i+k])] = append(idx[string(p.target[i:i+k])], i)
			}
			for _, x := range tk {
				if x.kind != tokLiteral {
					continue
				}
				litTotal++
				pos := x.pos
				for s := max(0, pos-k+1); s <= pos && s+k <= len(p.target); s++ {
					found := false
					for _, j := range idx[string(p.target[s:s+k])] {
						if j < s {
							found = true
							break
						}
					}
					if found {
						litReach++
						break
					}
				}
			}
		}
		pct := func(n int) string { return fmt.Sprintf("%d B (%+.1f%%)", n, 100*float64(n-base)/float64(base)) }
		t.Logf("== %s firmware, %d updates: today %d B; predicted %s", set.name, len(pairs), base, pct(pred))
		for _, fold := range []int{8, 32, 128, 1 << 30} {
			name := fmt.Sprintf("runs under %d", fold)
			if fold == 1<<30 {
				name = "whole covers"
			}
			t.Logf("  folded (%s), sparse model: %s", name, pct(folded[fold]))
		}
		t.Logf("  literal bytes: %d, of which %d (%.0f%%) inside a 6-byte match with earlier target (COPY_TARGET's reach)", litTotal, litReach, 100*float64(litReach)/float64(max(litTotal, 1)))
	}
}

// relocate returns dict with every aligned 32-bit little-endian word that
// points into dict (at base) moved by the shift of the cover containing
// its target, and the number of distinct shifts (the map the patch would
// carry).
func relocate(dict, target []byte, base uint32, align int) ([]byte, int, int) {
	covers := findCovers(dict, target)
	type seg struct{ old, n, shift int }
	var segs []seg
	shifts := map[int]bool{}
	for _, c := range covers {
		if c.extra > 0 || c.length < 64 {
			continue
		}
		segs = append(segs, seg{c.oldPos, c.length, c.newPos - c.oldPos})
		shifts[c.newPos-c.oldPos] = true
	}
	out := append([]byte{}, dict...)
	moved := 0
	for i := 0; i+4 <= len(dict); i += align {
		v := uint32(dict[i]) | uint32(dict[i+1])<<8 | uint32(dict[i+2])<<16 | uint32(dict[i+3])<<24
		if v < base || v >= base+uint32(len(dict)) {
			continue
		}
		o := int(v - base)
		for _, s := range segs {
			if o >= s.old && o < s.old+s.n {
				if s.shift != 0 {
					w := uint32(int64(v) + int64(s.shift))
					out[i], out[i+1], out[i+2], out[i+3] = byte(w), byte(w>>8), byte(w>>16), byte(w>>24)
					moved++
				}
				break
			}
		}
	}
	return out, len(shifts), moved
}

func guessBase(b []byte, align int) uint32 {
	best, bestN := uint32(0), -1
	for _, base := range []uint32{0, 0x2000, 0x4000, 0x8000, 0x10000, 0x08000000, 0x08004000} {
		n := 0
		for i := 0; i+4 <= len(b); i += align {
			v := uint32(b[i]) | uint32(b[i+1])<<8 | uint32(b[i+2])<<16 | uint32(b[i+3])<<24
			if v >= base+0x100 && v < base+uint32(len(b)) {
				n++
			}
		}
		if n > bestN {
			best, bestN = base, n
		}
	}
	return best
}

func TestIdeasRelocate(t *testing.T) {
	for _, set := range []struct {
		name, path   string
		width, align int
		base         int64
	}{{"ARM, base 0", *ideasFirmware, 4, 4, 0}, {"ARM, base 0x2000", *ideasFirmware, 4, 4, 0x2000}, {"MSP430X, base 0x10000", *ideasConfig, 2, 2, 0x10000}} {
		pairs := loadIdeaPairs(t, set.path)
		if len(pairs) == 0 {
			continue
		}
		var base, pred, reloc, mapBytes, movedTotal int
		for i, p := range pairs {
			b := uint32(set.base)
			_ = i
			v2 := withVersion(FormatVersion2, encodeCovers(p.dict, p.target, MaxSize))
			tk := ideaTokens(v2, p.dict)
			m0, m1 := newIdeaModel(nil), newIdeaModel(nil)
			codeBaseline(m0, tk)
			codePredictedLanes(m1, tk, set.width)
			base += m0.bytes()
			pred += m1.bytes()
			rd, nShifts, moved := relocate(p.dict, p.target, b, set.align)
			movedTotal += moved
			r2 := withVersion(FormatVersion2, encodeCovers(rd, p.target, MaxSize))
			m2 := newIdeaModel(nil)
			codePredictedLanes(m2, ideaTokens(r2, rd), set.width)
			// The map: each segment start and shift, about 6 bytes each
			// before coding; charge every cover of 64+ bytes.
			segs := 0
			for _, c := range findCovers(p.dict, p.target) {
				if c.extra == 0 && c.length >= 64 {
					segs++
				}
			}
			_ = nShifts
			mapBytes += segs * 6
			reloc += m2.bytes() + segs*6
		}
		pct := func(n int) string { return fmt.Sprintf("%d B (%+.1f%%)", n, 100*float64(n-base)/float64(base)) }
		t.Logf("== %s, %d updates: today %d B; predicted %s; relocated dictionary + predicted %s, map %d B of it; %d words relocated",
			set.name, len(pairs), base, pct(pred), pct(reloc), mapBytes, movedTotal)
	}
}

// relocateChosen relocates only within covers where it makes more bytes
// match the target (one bit per cover in the patch).
func relocateChosen(dict, target []byte, base uint32, align int) ([]byte, int) {
	full, _, _ := relocate(dict, target, base, align)
	out := append([]byte{}, dict...)
	n := 0
	for _, c := range findCovers(dict, target) {
		if c.extra > 0 {
			continue
		}
		n++
		with, without := 0, 0
		for k := 0; k < c.length; k++ {
			if full[c.oldPos+k] == target[c.newPos+k] {
				with++
			}
			if dict[c.oldPos+k] == target[c.newPos+k] {
				without++
			}
		}
		if with > without {
			copy(out[c.oldPos:c.oldPos+c.length], full[c.oldPos:c.oldPos+c.length])
		}
	}
	return out, n
}

func TestIdeasRelocateChosen(t *testing.T) {
	for _, set := range []struct {
		name, path   string
		width, align int
		base         uint32
	}{{"ARM, align 4", *ideasFirmware, 4, 4, 0x2000}, {"MSP430X, align 2", *ideasConfig, 2, 2, 0x10000}, {"MSP430X, align 4", *ideasConfig, 2, 4, 0x10000}} {
		pairs := loadIdeaPairs(t, set.path)
		if len(pairs) == 0 {
			continue
		}
		var base, reloc int
		for _, p := range pairs {
			v2 := withVersion(FormatVersion2, encodeCovers(p.dict, p.target, MaxSize))
			m0 := newIdeaModel(nil)
			codeBaseline(m0, ideaTokens(v2, p.dict))
			base += m0.bytes()
			rd, covers := relocateChosen(p.dict, p.target, set.base, set.align)
			r2 := withVersion(FormatVersion2, encodeCovers(rd, p.target, MaxSize))
			m2 := newIdeaModel(nil)
			codePredictedLanes(m2, ideaTokens(r2, rd), set.width)
			segs := 0
			for _, c := range findCovers(p.dict, p.target) {
				if c.extra == 0 && c.length >= 64 {
					segs++
				}
			}
			reloc += m2.bytes() + segs*6 + (covers+7)/8
		}
		t.Logf("== %s, %d updates: today %d B; relocation chosen per cover + predicted %d B (%+.1f%%)", set.name, len(pairs), base, reloc, 100*float64(reloc-base)/float64(base))
	}
}

// relocateMSP430X relocates MSP430X 20-bit immediates (CALLA #imm,
// MOVA #imm,Rd: 4 address bits in the opcode word, 16 in the next) and
// 32-bit data pointers whose high half is 1-15.
func relocateMSP430X(dict, target []byte, base uint32, ins, data bool) []byte {
	covers := findCovers(dict, target)
	type seg struct{ old, n, shift int }
	var segs []seg
	for _, c := range covers {
		if c.extra == 0 && c.length >= 64 {
			segs = append(segs, seg{c.oldPos, c.length, c.newPos - c.oldPos})
		}
	}
	shift := func(v uint32) (uint32, bool) {
		if v < base || v >= base+uint32(len(dict)) {
			return v, false
		}
		o := int(v - base)
		for _, s := range segs {
			if o >= s.old && o < s.old+s.n {
				return uint32(int64(v) + int64(s.shift)), s.shift != 0
			}
		}
		return v, false
	}
	out := append([]byte{}, dict...)
	w := func(i int) uint32 { return uint32(dict[i]) | uint32(dict[i+1])<<8 }
	for i := 0; i+4 <= len(dict); i += 2 {
		w0, w1 := w(i), w(i+2)
		switch {
		case ins && w0&0xFFF0 == 0x13B0: // CALLA #imm20
			if v, ok := shift((w0&0xF)<<16 | w1); ok {
				n0 := w0&0xFFF0 | v>>16&0xF
				out[i], out[i+1], out[i+2], out[i+3] = byte(n0), byte(n0>>8), byte(v), byte(v>>8)
				i += 2
			}
		case ins && w0&0xF0F0 == 0x0080: // MOVA #imm20, Rd
			if v, ok := shift((w0>>8&0xF)<<16 | w1); ok {
				n0 := w0&0xF0FF | (v>>16&0xF)<<8
				out[i], out[i+1], out[i+2], out[i+3] = byte(n0), byte(n0>>8), byte(v), byte(v>>8)
				i += 2
			}
		case data && w1 >= 1 && w1 <= 0xF:
			if v, ok := shift(w1<<16 | w0); ok {
				out[i], out[i+1], out[i+2], out[i+3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
				i += 2
			}
		}
	}
	return out
}

func TestIdeasRelocateMSP430X(t *testing.T) {
	pairs := loadIdeaPairs(t, *ideasConfig)
	for _, v := range []struct {
		name      string
		ins, data bool
	}{{"20-bit immediates", true, false}, {"32-bit data pointers", false, true}, {"both", true, true}} {
		var base, pred, reloc, chosen int
		for _, p := range pairs {
			v2 := withVersion(FormatVersion2, encodeCovers(p.dict, p.target, MaxSize))
			tk := ideaTokens(v2, p.dict)
			m0, m1 := newIdeaModel(nil), newIdeaModel(nil)
			codeBaseline(m0, tk)
			codePredictedLanes(m1, tk, 2)
			base += m0.bytes()
			pred += m1.bytes()
			full := relocateMSP430X(p.dict, p.target, 0x10000, v.ins, v.data)
			segs := 0
			// per-cover choice, as relocateChosen
			rd := append([]byte{}, p.dict...)
			nc := 0
			for _, c := range findCovers(p.dict, p.target) {
				if c.extra > 0 {
					continue
				}
				nc++
				if c.length >= 64 {
					segs++
				}
				with, without := 0, 0
				for k := 0; k < c.length; k++ {
					if full[c.oldPos+k] == p.target[c.newPos+k] {
						with++
					}
					if p.dict[c.oldPos+k] == p.target[c.newPos+k] {
						without++
					}
				}
				if with > without {
					copy(rd[c.oldPos:c.oldPos+c.length], full[c.oldPos:c.oldPos+c.length])
				}
			}
			for _, d := range [][]byte{full, rd} {
				r2 := withVersion(FormatVersion2, encodeCovers(d, p.target, MaxSize))
				m := newIdeaModel(nil)
				codePredictedLanes(m, ideaTokens(r2, d), 2)
				if &d[0] == &full[0] {
					reloc += m.bytes() + segs*6
				} else {
					chosen += m.bytes() + segs*6 + (nc+7)/8
				}
			}
		}
		pct := func(n int) string { return fmt.Sprintf("%d B (%+.1f%%)", n, 100*float64(n-base)/float64(base)) }
		t.Logf("== MSP430X %s, %d updates: today %d B; predicted %s; relocated %s; chosen per cover %s", v.name, len(pairs), base, pct(pred), pct(reloc), pct(chosen))
	}
}

// relocateARM relocates 32-bit pointers (as relocate) and, with bl, Thumb
// BL offsets: new offset = old + shift(callee) - shift(call site).
func relocateARM(dict, target []byte, base uint32, ptrs, bl bool) []byte {
	type seg struct{ old, n, shift int }
	var segs []seg
	for _, c := range findCovers(dict, target) {
		if c.extra == 0 && c.length >= 64 {
			segs = append(segs, seg{c.oldPos, c.length, c.newPos - c.oldPos})
		}
	}
	shiftAt := func(o int) (int, bool) {
		for _, s := range segs {
			if o >= s.old && o < s.old+s.n {
				return s.shift, true
			}
		}
		return 0, false
	}
	out := append([]byte{}, dict...)
	if ptrs {
		r, _, _ := relocate(dict, target, base, 4)
		copy(out, r)
	}
	if bl {
		for i := 0; i+4 <= len(dict); i += 2 {
			if dict[i+1]&0xF8 == 0xF0 && dict[i+3]&0xF8 == 0xF8 {
				off := int32(uint32(dict[i+1]&7)<<19|uint32(dict[i])<<11|uint32(dict[i+3]&7)<<8|uint32(dict[i+2])) << 10 >> 9 // sign-extend 22 bits, x2
				callee := i + 4 + int(off)
				s1, ok1 := shiftAt(i)
				s2, ok2 := shiftAt(callee)
				if ok1 && ok2 && s1 != s2 {
					n := (off + int32(s2-s1)) >> 1
					out[i+1] = 0xF0 | byte(n>>19&7)
					out[i] = byte(n >> 11)
					out[i+3] = 0xF8 | byte(n>>8&7)
					out[i+2] = byte(n)
				}
				i += 2
			}
		}
	}
	return out
}

func TestIdeasRelocateARM(t *testing.T) {
	pairs := loadIdeaPairs(t, *ideasFirmware)
	for _, v := range []struct {
		name     string
		ptrs, bl bool
	}{{"pointers", true, false}, {"BL offsets", false, true}, {"both", true, true}} {
		var base, reloc int
		for _, p := range pairs {
			v2 := withVersion(FormatVersion2, encodeCovers(p.dict, p.target, MaxSize))
			m0 := newIdeaModel(nil)
			codeBaseline(m0, ideaTokens(v2, p.dict))
			base += m0.bytes()
			d := relocateARM(p.dict, p.target, 0x2000, v.ptrs, v.bl)
			segs := 0
			for _, c := range findCovers(p.dict, p.target) {
				if c.extra == 0 && c.length >= 64 {
					segs++
				}
			}
			r2 := withVersion(FormatVersion2, encodeCovers(d, p.target, MaxSize))
			m := newIdeaModel(nil)
			codePredictedLanes(m, ideaTokens(r2, d), 4)
			reloc += m.bytes() + segs*6
		}
		t.Logf("== ARM %s, %d updates: today %d B; relocated + predicted %d B (%+.1f%%)", v.name, len(pairs), base, reloc, 100*float64(reloc-base)/float64(base))
	}
}

func TestIdeasRelocateCheck(t *testing.T) {
	pairs := loadIdeaPairs(t, *ideasFirmware)
	var today, real int
	for _, p := range pairs {
		patch, _ := EncodeWith(p.dict, p.target, EncodeOptions{CoderTables: 777})
		today += len(patch.Bytes)
		d := relocateARM(p.dict, p.target, 0x2000, true, true)
		r2 := withVersion(FormatVersion2, encodeCovers(d, p.target, MaxSize))
		coded := encodeCoded(r2, codedLayoutB)
		got, err := Decode(d, coded)
		if err != nil || string(got) != string(p.target) {
			t.Fatalf("%s: round trip against the relocated dictionary failed: %v", p.name, err)
		}
		segs := 0
		for _, c := range findCovers(p.dict, p.target) {
			if c.extra == 0 && c.length >= 64 {
				segs++
			}
		}
		real += len(coded) + segs*6
	}
	t.Logf("ARM, %d updates, real coder, no prediction: today %d B; relocated %d B (%+.1f%%)", len(pairs), today, real, 100*float64(real-today)/float64(today))
}
