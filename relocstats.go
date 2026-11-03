// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"fmt"
	"sort"
	"strings"
)

// RelocationStats describes, for a firmware update, what relocation
// leaves to the patch: experimental, for tuning the relocation rules on
// images that can't be shared (tinydelta relocstats). It reports counts
// and instruction words only, no image content beyond those.
func RelocationStats(dict, target []byte, rule Relocation, base uint32) string {
	var b strings.Builder
	m := relocMapFrom(dict, target, rule, base).withDataRuns(dict, target).withOutside(dict, target)
	rd := m.relocate(dict)
	moved := 0
	for i := range dict {
		if rd[i] != dict[i] {
			moved++
		}
	}
	blocks := 0
	for _, r := range m.data {
		blocks += int(r.length)
	}
	fmt.Fprintf(&b, "map: %d entries, %d data runs (%d blocks of %d B), %d entries outside the image; relocation changed %d bytes of the old image\n", len(m.entries), len(m.data), blocks, relocDataBlock, len(m.outside), moved)
	shifts := map[uint16]bool{}
	for _, e := range m.entries {
		shifts[uint16(e.shift)] = true
	}

	type key struct{ prev, next uint16 }
	diffBytes, literal, diffWords, addrWords := 0, 0, 0, 0
	regions := map[uint32]int{}
	contexts := map[key]int{}
	inData := 0
	for _, c := range findCovers(rd, target) {
		if c.extra > 0 {
			literal += c.extra
			continue
		}
		seen := map[int]bool{}
		for k := 0; k < c.length; k++ {
			o, n := c.oldPos+k, c.newPos+k
			if rd[o] == target[n] {
				continue
			}
			diffBytes++
			regions[(base+uint32(n))>>12]++
			// The 16-bit word holding it, by address parity.
			w := n
			if (base+uint32(n))&1 != 0 {
				w--
			}
			if seen[w] || w < c.newPos || w+2 > c.newPos+c.length {
				continue
			}
			seen[w] = true
			diffWords++
			ow := uint16(rd[o-(n-w)]) | uint16(rd[o-(n-w)+1])<<8
			nw := uint16(target[w]) | uint16(target[w+1])<<8
			if !shifts[nw-ow] {
				continue
			}
			addrWords++
			if m.inData(o - (n - w)) {
				inData++
			}
			var kk key
			if w >= 2 {
				kk.prev = uint16(target[w-2]) | uint16(target[w-1])<<8
			}
			if w+4 <= len(target) {
				kk.next = uint16(target[w+2]) | uint16(target[w+3])<<8
			}
			contexts[kk]++
		}
	}
	fmt.Fprintf(&b, "left to the patch: %d bytes differ inside aligned regions, %d new bytes outside them\n", diffBytes, literal)
	fmt.Fprintf(&b, "differing 16-bit words: %d; of them %d changed by exactly a map shift (likely addresses not relocated), %d of those in data runs\n", diffWords, addrWords, inData)

	type kv struct {
		k uint32
		v int
	}
	var rs []kv
	for k, v := range regions {
		rs = append(rs, kv{k, v})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].v > rs[j].v || (rs[i].v == rs[j].v && rs[i].k < rs[j].k) })
	fmt.Fprintf(&b, "\ndiffering bytes by 4 KB of the new image (top 15):\n")
	for i, r := range rs {
		if i == 15 {
			break
		}
		fmt.Fprintf(&b, "  0x%05x-0x%05x  %6d\n", r.k<<12, r.k<<12|0xFFF, r.v)
	}
	type kc struct {
		k key
		v int
	}
	var cs []kc
	for k, v := range contexts {
		cs = append(cs, kc{k, v})
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].v > cs[j].v || (cs[i].v == cs[j].v && cs[i].k.prev < cs[j].k.prev) })
	fmt.Fprintf(&b, "\nwords changed by a map shift, by the words around them in the new image (top 25):\n  count  word before  word after\n")
	for i, c := range cs {
		if i == 25 {
			break
		}
		fmt.Fprintf(&b, "  %5d  0x%04x       0x%04x\n", c.v, c.k.prev, c.k.next)
	}
	if rule == RelocationMSP430X {
		msp430xStats(&b, m, dict, rd, target, base)
		msp430xBaseCheck(&b, dict, target, base)
	}
	return b.String()
}

// msp430xStats sorts the address fields relocation left wrong by why: the
// instruction is in a data run, the decode took it for an operand word,
// its address isn't in the image, or the map gave it another shift than
// where the address really moved.
func msp430xStats(b *strings.Builder, m *relocMap, dict, rd, target []byte, base uint32) {
	const none = int(^uint(0) >> 1)
	moved := make([]int, len(dict)) // where each old byte really went, from the covers
	for i := range moved {
		moved[i] = none
	}
	covers := findCovers(rd, target)
	for _, c := range covers {
		if c.extra == 0 {
			for k := 0; k < c.length; k++ {
				moved[c.oldPos+k] = c.newPos - c.oldPos
			}
		}
	}
	word := func(img []byte, i int) uint32 { return uint32(img[i]) | uint32(img[i+1])<<8 }
	type pair struct{ mapShift, needed int64 }
	reasons := map[string]int{}
	wrong := map[pair]int{}
	rejected := map[[4]uint16]int{}
	right := 0
	for _, c := range covers {
		if c.extra > 0 {
			continue
		}
		for k := 0; k+4 <= c.length; k++ {
			h, n := c.oldPos+k, c.newPos+k
			l, f, nf := msp430xCandidate(&relocMap{arch: m.arch, base: m.base, entries: m.entries}, dict, base, h)
			if l == 0 || k+l > c.length {
				continue
			}
			for i := 0; i < nf; i++ {
				at := f[i].lo - h
				oldAddr := word(dict, f[i].lo) | (word(dict, h)>>uint32(f[i].shift)&0xF)<<16
				newAddr := word(target, n+at) | (word(target, n)>>uint32(f[i].shift)&0xF)<<16
				if oldAddr == newAddr {
					continue
				}
				if word(rd, f[i].lo) == word(target, n+at) && word(rd, h)>>uint32(f[i].shift)&0xF == word(target, n)>>uint32(f[i].shift)&0xF {
					right++
					continue
				}
				switch lv, _, _ := msp430xValid(m, dict, base, h); {
				case m.inData(h):
					reasons["in a data run"]++
				case lv == 0:
					reasons["taken for an operand word by the instruction decode"]++
					var ctx [4]uint16
					for j := range ctx {
						if p := h - 8 + 2*j; p >= 0 {
							ctx[j] = uint16(word(dict, p))
						}
					}
					rejected[ctx]++
				case oldAddr < base || uint64(oldAddr-base) >= uint64(len(dict)):
					reasons["address outside the old image"]++
				default:
					s := m.addressShift(oldAddr, len(dict))
					needed := int64(newAddr) - int64(oldAddr)
					if mv := moved[oldAddr-base]; mv != none && int64(mv) == needed {
						reasons["map shift differs from where the address really moved"]++
					} else {
						reasons["map shift differs, and the address's target changed too"]++
					}
					wrong[pair{s, needed}]++
				}
			}
		}
	}
	fmt.Fprintf(b, "\nMSP430X address fields that moved: %d relocated right; left wrong:\n", right)
	var rk []string
	for k := range reasons {
		rk = append(rk, k)
	}
	sort.Slice(rk, func(i, j int) bool { return reasons[rk[i]] > reasons[rk[j]] })
	for _, k := range rk {
		fmt.Fprintf(b, "  %6d  %s\n", reasons[k], k)
	}
	type pc struct {
		p pair
		v int
	}
	var ps []pc
	for k, v := range wrong {
		ps = append(ps, pc{k, v})
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].v > ps[j].v })
	fmt.Fprintf(b, "\nwrong shift, by map shift and the shift needed (top 15):\n  count  map  needed\n")
	for i, p := range ps {
		if i == 15 {
			break
		}
		fmt.Fprintf(b, "  %5d  %+d  %+d\n", p.v, p.p.mapShift, p.p.needed)
	}
	type rc struct {
		k [4]uint16
		v int
	}
	var rs []rc
	for k, v := range rejected {
		rs = append(rs, rc{k, v})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].v > rs[j].v })
	fmt.Fprintf(b, "\ntaken for operands, by the 4 words before the instruction (top 15):\n")
	for i, r := range rs {
		if i == 15 {
			break
		}
		fmt.Fprintf(b, "  %5d  %04x %04x %04x %04x\n", r.v, r.k[0], r.k[1], r.k[2], r.k[3])
	}
}

// msp430xBaseCheck tests the image base: for each CALLA #imm whose callee
// address changed, the base is right where the old image's bytes at the
// callee moved by that change. It reports how many calls the given base
// explains and the bases that explain most.
func msp430xBaseCheck(b *strings.Builder, dict, target []byte, base uint32) {
	const none = int(^uint(0) >> 1)
	moved := make([]int, len(dict))
	for i := range moved {
		moved[i] = none
	}
	covers := findCovers(dict, target)
	for _, c := range covers {
		if c.extra == 0 {
			for k := 0; k < c.length; k++ {
				moved[c.oldPos+k] = c.newPos - c.oldPos
			}
		}
	}
	type call struct {
		addr   uint32
		needed int
	}
	var calls []call
	for _, c := range covers {
		if c.extra > 0 {
			continue
		}
		for k := 0; k+4 <= c.length; k++ {
			h, n := c.oldPos+k, c.newPos+k
			w := uint32(dict[h]) | uint32(dict[h+1])<<8
			if w&0xFFF0 != 0x13B0 || uint32(target[n])|uint32(target[n+1])<<8 != w&0xFFF0|uint32(target[n])&0xF {
				continue
			}
			oldAddr := (w&0xF)<<16 | uint32(dict[h+2]) | uint32(dict[h+3])<<8
			newAddr := uint32(target[n]&0xF)<<16 | uint32(target[n+2]) | uint32(target[n+3])<<8
			if oldAddr != newAddr {
				calls = append(calls, call{oldAddr, int(newAddr) - int(oldAddr)})
			}
		}
	}
	explained := func(bs uint32) int {
		k := 0
		for _, c := range calls {
			if o := int64(c.addr) - int64(bs); o >= 0 && o < int64(len(dict)) && moved[o] == c.needed {
				k++
			}
		}
		return k
	}
	// Bases explaining the most calls, as ranges of neighbouring bases.
	type rng struct {
		from, to uint32
		k        int
	}
	var rs []rng
	for bs := uint32(0); bs < 0x40000; bs += 2 {
		k := explained(bs)
		if k == 0 {
			continue
		}
		if l := len(rs) - 1; l >= 0 && rs[l].to+2 == bs && rs[l].k == k {
			rs[l].to = bs
			continue
		}
		rs = append(rs, rng{bs, bs, k})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].k > rs[j].k || (rs[i].k == rs[j].k && rs[i].from < rs[j].from) })
	fmt.Fprintf(b, "\nimage base check: %d CALLA #imm whose callee moved; base 0x%05x explains %d of them; best bases:", len(calls), base, explained(base))
	for i, r := range rs {
		if i == 5 {
			break
		}
		fmt.Fprintf(b, " 0x%05x-0x%05x (%d)", r.from, r.to, r.k)
	}
	fmt.Fprintf(b, "\n")
}
