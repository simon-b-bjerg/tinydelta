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
	m := relocMapFrom(dict, target, rule, base).withDataRuns(dict, target)
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
	fmt.Fprintf(&b, "map: %d entries, %d data runs (%d blocks of %d B); relocation changed %d bytes of the old image\n", len(m.entries), len(m.data), blocks, relocDataBlock, moved)
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
	return b.String()
}
