// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import "sort"

// Search limits for the optimal encoder. On real config files, widening
// all three (4x the chain, 8x the offsets, longer prefixes) gave
// byte-identical patches, so these are generous rather than tight.
const (
	optChain    = 64 // hash-chain candidates examined per position
	optOffsets  = 32 // cursor offsets tracked, besides 0 and greedy's own
	optPrefixes = 16 // every match length up to this is tried, plus the full one
)

// optMaxStates bounds the DP table: (len(target)+1) x tracked offsets, ~20
// bytes per state (so ~80 MB at the limit; roughly 100 KB targets). Larger
// inputs fall back to the greedy encoder. A variable only so tests can
// exercise the fallback.
var optMaxStates = 4 << 20

// opSetPos is internal to the optimal encoder: a length-0 COPY, used only
// to move the dictionary cursor (see COPY in format.go).
const opSetPos = 4

const optInf = int32(1) << 30

type optStep struct {
	prevO  int32 // offset index of the predecessor state
	length int32 // instruction length; the predecessor's position is q-length
	delta  int32 // COPY/COPY_TARGET/set-position delta
	op     int8
}

type match struct{ addr, length int }

// encodeOptimal finds the smallest patch expressible in this format for
// target, by dynamic programming over exact instruction costs, or returns
// nil if target is too large for the DP table (see optMaxStates).
//
// The state is (output position q, cursor offset off), where the cursor is
// always q+off. ADD, SKIP and COPY_TARGET advance q and the cursor together,
// leaving off unchanged; only COPY (including its length-0 "set position"
// form) changes off. Every transition is priced with its exact encoded size,
// so the result is optimal within the candidates searched: dictionary and
// self matches from the hash chains (bounded by optChain), a bounded set of
// cursor offsets, and a bounded set of lengths for each match.
//
// No instruction produces more than maxLen (at least 1) target bytes.
// No COPY_TARGET reads more than maxDistance bytes back.
//
// greedy is EncodeGreedy's patch for the same input and maxLen: the cursor
// offsets its COPYs land on are always tracked, so its parse stays
// reachable.
func encodeOptimal(dict, target, greedy []byte, maxLen, maxDistance int) []byte {
	n := len(target)
	if n == 0 {
		return nil
	}
	dm := newMatcher(dict)
	dictMatches := func(q int) []match {
		return chainMatches(dm.head, dm.next, dict, target, q)
	}

	// Cursor offsets to track: 0, greedy's, and those where dictionary
	// matches would cover the most target bytes.
	weight := map[int]int{}
	for q := 0; q < n; q++ {
		for _, m := range dictMatches(q) {
			weight[m.addr-q] += m.length
		}
	}
	type offWeight struct{ off, w int }
	var ranked []offWeight
	for o, w := range weight {
		ranked = append(ranked, offWeight{o, w})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].w != ranked[j].w {
			return ranked[i].w > ranked[j].w
		}
		return ranked[i].off < ranked[j].off
	})
	offIndex := map[int]int{}
	var offs []int
	addOff := func(o int) {
		if _, ok := offIndex[o]; !ok {
			offIndex[o] = len(offs)
			offs = append(offs, o)
		}
	}
	addOff(0)
	for _, o := range copyOffsets(greedy) {
		addOff(o)
	}
	for i := 0; i < len(ranked) && i < optOffsets; i++ {
		addOff(ranked[i].off)
	}
	K := len(offs)
	if (n+1)*K > optMaxStates {
		return nil
	}

	cost := make([]int32, (n+1)*K)
	for i := range cost {
		cost[i] = optInf
	}
	back := make([]optStep, (n+1)*K)
	ix := func(q, o int) int { return q*K + o }
	relax := func(at int, c int32, s optStep) {
		if c < cost[at] {
			cost[at], back[at] = c, s
		}
	}

	// ADD and SKIP of length k cost an opcode varint of hdr bytes, which
	// depends only on which band k falls in (1-31: 1 byte, 32-4095: 2, ...).
	// Within a band, the best predecessor for every end position q is a
	// sliding-window minimum, kept per band and offset in a monotonic
	// deque, so every length is considered without trying each one. The
	// last band ends at maxLen.
	type band struct{ lo, hi int }
	var bands []band
	for b := 1; ; b++ {
		hi := 1<<(7*b-2) - 1
		lo := 1
		if b > 1 {
			lo = bands[b-2].hi + 1
		}
		bands = append(bands, band{lo, min(hi, maxLen)})
		if hi >= n || hi >= maxLen {
			break
		}
	}
	addQ := make([]monoDeque, len(bands)*K)  // values cost[p]-p
	skipQ := make([]monoDeque, len(bands)*K) // values cost[p]
	runStart := make([]int, K)               // SKIP at offset o is valid only from runStart[o] on
	sm := newSelfMatcher(target, maxDistance)
	base := make([]int32, K)

	for q := 0; q <= n; q++ {
		if q == 0 {
			cost[ix(0, offIndex[0])] = 0
		} else {
			j := q - 1
			for o := 0; o < K; o++ {
				if a := j + offs[o]; a < 0 || a >= len(dict) || dict[a] != target[j] {
					runStart[o] = q
				}
			}
			for b, bd := range bands {
				p := q - bd.lo
				if p < 0 {
					continue
				}
				for o := 0; o < K; o++ {
					if v := cost[ix(p, o)]; v < optInf {
						addQ[b*K+o].push(p, v-int32(p))
						skipQ[b*K+o].push(p, v)
					}
				}
			}
			for o := 0; o < K; o++ {
				at := ix(q, o)
				for b, bd := range bands {
					hdr := int32(b + 1)
					if p, v, ok := addQ[b*K+o].front(q - bd.hi); ok {
						relax(at, v+int32(q)+hdr, optStep{int32(o), int32(q - p), 0, opAdd})
					}
					if p, v, ok := skipQ[b*K+o].front(max(runStart[o], q-bd.hi)); ok {
						relax(at, v+hdr, optStep{int32(o), int32(q - p), 0, opSkip})
					}
				}
			}
		}

		// Set position: move the cursor to another tracked offset without
		// emitting output.
		copy(base, cost[ix(q, 0):ix(q, 0)+K])
		for o2 := 0; o2 < K; o2++ {
			if a := q + offs[o2]; a < 0 || a > len(dict) {
				continue
			}
			for o := 0; o < K; o++ {
				if o != o2 && base[o] < optInf {
					d := int64(offs[o2] - offs[o])
					relax(ix(q, o2), base[o]+1+int32(varintLen(zigzagEncode(d))), optStep{int32(o), 0, int32(d), opSetPos})
				}
			}
		}
		if q == n {
			break
		}

		// COPY_TARGET candidates (target already produced), keeping only
		// the longest per delta cost.
		bestSelf := map[int]match{}
		for _, m := range chainMatches(sm.head, sm.next, target, target, q) {
			if q-m.addr > maxDistance {
				break // the chain runs from the nearest position back
			}
			c := varintLen(zigzagEncode(int64(m.addr - q)))
			if cur, ok := bestSelf[c]; !ok || m.length > cur.length {
				bestSelf[c] = m
			}
		}
		sm.insert(q)
		var dc []match
		for _, m := range dictMatches(q) {
			if _, ok := offIndex[m.addr-q]; ok {
				dc = append(dc, m)
			}
		}

		for o := 0; o < K; o++ {
			c0 := cost[ix(q, o)]
			if c0 >= optInf {
				continue
			}
			cursor := q + offs[o]
			for _, m := range dc {
				o2 := offIndex[m.addr-q]
				d := int64(m.addr - cursor)
				dcost := int32(varintLen(zigzagEncode(d)))
				for _, k := range matchLengths(min(m.length, maxLen)) {
					relax(ix(q+k, o2), c0+int32(varintLen(uint64(k)<<2))+dcost, optStep{int32(o), int32(k), int32(d), opCopy})
				}
			}
			for _, m := range bestSelf {
				d := int64(m.addr - q)
				dcost := int32(varintLen(zigzagEncode(d)))
				for _, k := range matchLengths(min(m.length, maxLen)) {
					relax(ix(q+k, o), c0+int32(varintLen(uint64(k)<<2|opTargetCopy))+dcost, optStep{int32(o), int32(k), int32(d), opTargetCopy})
				}
			}
		}
	}

	bestO := 0
	for o := 1; o < K; o++ {
		if cost[ix(n, o)] < cost[ix(n, bestO)] {
			bestO = o
		}
	}
	var steps []optStep
	var at []int // position each step starts at
	for q, o := n, bestO; !(q == 0 && o == offIndex[0]); {
		s := back[ix(q, o)]
		if s.op != opSetPos {
			q -= int(s.length)
		}
		steps, at = append(steps, s), append(at, q)
		o = int(s.prevO)
	}

	var out []byte
	for i := len(steps) - 1; i >= 0; i-- {
		s, p := steps[i], at[i]
		switch s.op {
		case opAdd:
			out = appendVarint(out, uint64(s.length)<<2|opAdd)
			out = append(out, target[p:p+int(s.length)]...)
		case opSkip:
			out = appendVarint(out, uint64(s.length)<<2|opSkip)
		case opCopy, opTargetCopy:
			out = appendVarint(out, uint64(s.length)<<2|uint64(s.op))
			out = appendVarint(out, zigzagEncode(int64(s.delta)))
		case opSetPos:
			out = appendVarint(out, opCopy) // length 0
			out = appendVarint(out, zigzagEncode(int64(s.delta)))
		}
	}
	if int32(len(out)) != cost[ix(n, bestO)] {
		panic("tinydelta: optimal encoder's cost model disagrees with its own output")
	}
	return out
}

// chainMatches returns the matches (all at least 4 bytes) for target[q:]
// along one hash chain, up to optChain candidates.
func chainMatches(head map[uint32]int32, next []int32, src, target []byte, q int) []match {
	if q+4 > len(target) {
		return nil
	}
	c, ok := head[hash4(target[q:])]
	if !ok {
		return nil
	}
	var out []match
	for probes := 0; c >= 0 && probes < optChain; probes, c = probes+1, next[c] {
		if n := matchLen(src, int(c), target, q, len(target)-q); n >= 4 {
			out = append(out, match{int(c), n})
		}
	}
	return out
}

// matchLengths lists the lengths tried for a match of length mlen: every
// length up to optPrefixes, the longest length with each opcode-varint
// size (31, 4095, ...) below mlen, and mlen itself.
func matchLengths(mlen int) []int {
	var ks []int
	for k := 2; k <= min(mlen, optPrefixes); k++ {
		ks = append(ks, k)
	}
	for b := 1; ; b++ {
		hi := 1<<(7*b-2) - 1
		if hi >= mlen {
			break
		}
		if hi > optPrefixes {
			ks = append(ks, hi)
		}
	}
	if mlen > optPrefixes {
		ks = append(ks, mlen)
	}
	return ks
}

// copyOffsets returns the cursor offset (cursor - output position) right
// after each COPY in patch.
func copyOffsets(patch []byte) []int {
	var offs []int
	i, p, cursor := 0, 0, 0
	for i < len(patch) {
		v, n := readVarint(patch[i:])
		i += n
		op, length := v&3, int(v>>2)
		switch op {
		case opCopy:
			d, n := readVarint(patch[i:])
			i += n
			cursor += int(zigzagDecode(d)) + length
			p += length
			offs = append(offs, cursor-p)
		case opAdd:
			i += length
			p, cursor = p+length, cursor+length
		case opSkip:
			p, cursor = p+length, cursor+length
		case opTargetCopy:
			_, n := readVarint(patch[i:])
			i += n
			p, cursor = p+length, cursor+length
		}
	}
	return offs
}

// monoDeque is a monotonic deque: the minimum value over a sliding window
// of positions whose bounds only ever move forward.
type monoDeque struct {
	p    []int
	v    []int32
	head int
}

func (m *monoDeque) push(p int, v int32) {
	for len(m.p) > m.head && m.v[len(m.v)-1] >= v {
		m.p, m.v = m.p[:len(m.p)-1], m.v[:len(m.v)-1]
	}
	m.p, m.v = append(m.p, p), append(m.v, v)
}

// front returns the minimum among positions >= left, dropping older ones.
func (m *monoDeque) front(left int) (int, int32, bool) {
	for m.head < len(m.p) && m.p[m.head] < left {
		m.head++
	}
	if m.head == len(m.p) {
		return 0, 0, false
	}
	return m.p[m.head], m.v[m.head], true
}
