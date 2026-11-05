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
func RelocationStats(dict, target []byte, rules *Rules, base uint32) string {
	return relocationStats(dict, target, rules, base, false)
}

// RelocationStatsVerbose is RelocationStats, then the map itself (entries,
// data runs, RAM ranges) and every address field left wrong, one per line:
// where it is, the rule, its old and new address, the map's shift and the
// one it needed, and why. Numbers and rule names only, so it can be shared
// where the images can't.
func RelocationStatsVerbose(dict, target []byte, rules *Rules, base uint32) string {
	return relocationStats(dict, target, rules, base, true)
}

func relocationStats(dict, target []byte, rules *Rules, base uint32, verbose bool) string {
	var b strings.Builder
	// As the encoder builds it: entries, gaps, RAM ranges, then data runs
	// picked with the ranges in place, and the ranges voted again.
	m := relocMapFrom(dict, target, rules, base).withGaps(dict, target).withOutside(dict, target).withDataRuns(dict, target).withOutside(dict, target)
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
	var detail *strings.Builder
	if verbose {
		detail = &strings.Builder{}
	}
	fieldStats(&b, m, dict, rd, target, detail)
	baseCheck(&b, m.rules, dict, target, base)
	if verbose {
		fmt.Fprintf(&b, "\nmap entries (old address, length, shift):\n")
		for _, e := range m.entries {
			fmt.Fprintf(&b, "  0x%05x  0x%05x  %+d\n", base+e.start, e.length, e.shift)
		}
		fmt.Fprintf(&b, "\ndata runs (address, length):\n")
		for _, d := range m.data {
			fmt.Fprintf(&b, "  0x%05x  0x%05x\n", base+d.start*relocDataBlock, d.length*relocDataBlock)
		}
		fmt.Fprintf(&b, "\nRAM ranges (address, length, shift):\n")
		for _, e := range m.outside {
			fmt.Fprintf(&b, "  0x%05x  0x%05x  %+d\n", e.start, e.length, e.shift)
		}
		fmt.Fprintf(&b, "\naddress fields left wrong (instruction address, rule, old address -> new, map shift, needed, why):\n%s", detail.String())
	}
	return b.String()
}

// fieldStats sorts the address fields relocation left wrong by why: in a
// data run, not an instruction start (the sync decode took it for part of
// another instruction), excluded by unless, an address outside the image
// with no RAM range, or a shift other than the one the address needed.
// afterPrefix reports whether a prefix (an MSP430X extension word) comes
// right before p: the instruction at p is then the prefixed one, whose rule
// at the prefix accounts for its addresses.
func afterPrefix(r *Rules, dict []byte, base uint32, p int) bool {
	for i := range r.lists[rulePrefixes] {
		if ru := &r.lists[rulePrefixes][i]; r.matches(ru, dict, base, p-ru.n) {
			return true
		}
	}
	return false
}

// fieldStats counts address fields left wrong, by reason; with detail, it
// also lists each one there.
func fieldStats(b *strings.Builder, m *relocMap, dict, rd, target []byte, detail *strings.Builder) {
	const none = int(^uint(0) >> 1)
	r := m.rules
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
	type pair struct{ mapShift, needed int64 }
	reasons := map[string]int{}
	wrong := map[pair]int{}
	rejected := map[[4]uint32]int{}
	right := 0
	for _, c := range covers {
		if c.extra > 0 {
			continue
		}
		for k := 0; k < c.length; k++ {
			p, n := c.oldPos+k, c.newPos+k
			if (m.base+uint32(p))%uint32(r.unit) != 0 {
				continue
			}
			i := r.first(ruleCode, dict, m.base, p)
			if i < 0 || len(r.lists[ruleCode][i].addrs) == 0 || k+r.lists[ruleCode][i].size > c.length || afterPrefix(r, dict, m.base, p) {
				continue
			}
			ru := &r.lists[ruleCode][i]
			if !r.sameBesideAddresses(ru, dict, p, target, n) {
				continue // another instruction
			}
			for _, a := range ru.addrs {
				oldV, newV := r.join(dict, p, a), r.join(target, n, a)
				if oldV == newV {
					continue
				}
				if r.join(rd, p, a) == newV {
					right++
					continue
				}
				var why string
				var mapShift int64
				switch {
				case m.inData(p):
					why = "in a data run"
				case r.sync > 0 && !r.starts(m, dict, p):
					why = "taken for part of another instruction by the instruction-start decode"
					var ctx [4]uint32
					for j := range ctx {
						if q := p - (4-j)*r.unit; q >= 0 && q+r.wordBytes <= len(dict) {
							ctx[j] = r.word(dict, q, 0)
						}
					}
					rejected[ctx]++
				case r.ruleAt(m, dict, p) == nil:
					why = "excluded by unless"
				case ru.relative:
					why = "relative: the map's shifts don't give the new offset"
				default:
					a64 := int64(oldV)
					mapShift = m.shiftOf(a64, len(dict), ru.space)
					needed := int64(newV) - a64
					inside := inImage(m.base, len(dict), uint64(oldV))
					switch {
					case !inside && mapShift == 0:
						why = "address outside the image, in no RAM range"
					case inside && moved[oldV-m.base] != none && int64(moved[oldV-m.base]) == needed:
						why = "map shift differs from where the address really moved"
					default:
						why = "map shift differs, and the address's target changed too"
					}
					wrong[pair{mapShift, needed}]++
				}
				reasons[why]++
				if detail != nil {
					fmt.Fprintf(detail, "  0x%05x  %-60q  0x%05x -> 0x%05x  map %+d  needed %+d  %s\n",
						m.base+uint32(p), ru.name, oldV, newV, mapShift, int64(newV)-int64(oldV), why)
				}
			}
		}
	}
	fmt.Fprintf(b, "\naddress fields that moved: %d relocated right; left wrong:\n", right)
	var rk []string
	for k := range reasons {
		rk = append(rk, k)
	}
	sort.Slice(rk, func(i, j int) bool {
		return reasons[rk[i]] > reasons[rk[j]] || (reasons[rk[i]] == reasons[rk[j]] && rk[i] < rk[j])
	})
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
	sort.Slice(ps, func(i, j int) bool {
		return ps[i].v > ps[j].v || (ps[i].v == ps[j].v && ps[i].p.needed < ps[j].p.needed)
	})
	fmt.Fprintf(b, "\nwrong shift, by map shift and the shift needed (top 15):\n  count  map  needed\n")
	for i, p := range ps {
		if i == 15 {
			break
		}
		fmt.Fprintf(b, "  %5d  %+d  %+d\n", p.v, p.p.mapShift, p.p.needed)
	}
	type rc struct {
		k [4]uint32
		v int
	}
	var rs []rc
	for k, v := range rejected {
		rs = append(rs, rc{k, v})
	}
	sort.Slice(rs, func(i, j int) bool {
		return rs[i].v > rs[j].v || (rs[i].v == rs[j].v && fmt.Sprint(rs[i].k) < fmt.Sprint(rs[j].k))
	})
	fmt.Fprintf(b, "\ntaken for part of another instruction, by the 4 words before it (top 15):\n")
	for i, x := range rs {
		if i == 15 {
			break
		}
		fmt.Fprintf(b, "  %5d ", x.v)
		for _, w := range x.k {
			fmt.Fprintf(b, " %0*x", 2*r.wordBytes, w)
		}
		fmt.Fprintln(b)
	}
}

// baseCheck tests the image base: for each absolute address in a code rule
// whose value changed, the base is right where the old image's bytes at
// that address moved by the same amount. It reports how many the given
// base explains and the bases near it that explain most.
func baseCheck(b *strings.Builder, r *Rules, dict, target []byte, base uint32) {
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
	type addr struct {
		addr   uint32
		needed int
	}
	var addrs []addr
	for _, c := range covers {
		if c.extra > 0 {
			continue
		}
		for k := 0; k < c.length; k++ {
			p, n := c.oldPos+k, c.newPos+k
			if (base+uint32(p))%uint32(r.unit) != 0 {
				continue
			}
			i := r.first(ruleCode, dict, base, p)
			if i < 0 {
				continue
			}
			ru := &r.lists[ruleCode][i]
			if len(ru.addrs) == 0 || ru.relative || k+ru.size > c.length || !r.sameBesideAddresses(ru, dict, p, target, n) {
				continue
			}
			for _, a := range ru.addrs {
				if o, nw := r.join(dict, p, a), r.join(target, n, a); o != nw {
					addrs = append(addrs, addr{o, int(nw) - int(o)})
				}
			}
		}
	}
	explained := func(bs int64) int {
		k := 0
		for _, c := range addrs {
			if o := int64(c.addr) - bs; o >= 0 && o < int64(len(dict)) && moved[o] == c.needed {
				k++
			}
		}
		return k
	}
	// Bases explaining the most addresses within 128 KB of the given one,
	// as ranges of neighbouring bases.
	type rng struct {
		from, to int64
		k        int
	}
	var rs []rng
	step := int64(r.unit)
	for bs := max(0, int64(base)-0x20000) / step * step; bs <= int64(base)+0x20000; bs += step {
		k := explained(bs)
		if k == 0 {
			continue
		}
		if l := len(rs) - 1; l >= 0 && rs[l].to+step == bs && rs[l].k == k {
			rs[l].to = bs
			continue
		}
		rs = append(rs, rng{bs, bs, k})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].k > rs[j].k || (rs[i].k == rs[j].k && rs[i].from < rs[j].from) })
	fmt.Fprintf(b, "\nimage base check: %d absolute addresses that changed; base 0x%05x explains %d of them; best bases:", len(addrs), base, explained(int64(base)))
	for i, x := range rs {
		if i == 5 {
			break
		}
		fmt.Fprintf(b, " 0x%05x-0x%05x (%d)", x.from, x.to, x.k)
	}
	fmt.Fprintf(b, "\n")
}
