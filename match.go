// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import "encoding/binary"

const (
	minMatch = 4  // shortest run the hash can even detect
	maxChain = 64 // bound per-position search so large dictionaries stay roughly linear
)

// matcher finds the longest run of target bytes, starting at a given
// position, that occurs somewhere in dict — a plain LZ77-against-a-fixed-
// dictionary search, feeding COPY instructions. The whole dictionary is
// known upfront, so its hash chain is built once and reused for every
// query, unlike selfMatcher below, which has to
// build its chain incrementally as the target is scanned.
type matcher struct {
	dict []byte
	head map[uint32]int32
	next []int32 // next[p] = the previous dict position with dict[p:p+4]'s key, or -1
}

func newMatcher(dict []byte) *matcher {
	m := &matcher{
		dict: dict,
		head: make(map[uint32]int32, len(dict)),
		next: make([]int32, len(dict)),
	}
	for p := range m.next {
		m.next[p] = -1
	}
	for p := 0; p+4 <= len(dict); p++ {
		h := hash4(dict[p:])
		prev, ok := m.head[h]
		if ok {
			m.next[p] = prev
		}
		m.head[h] = int32(p)
	}
	return m
}

func hash4(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }

// bestMatch returns the longest match in dict for target[p:], capped at
// limit bytes, and that match's dictionary address. Ties prefer the most
// recently inserted (and so nearest-addressed) dict position.
func (m *matcher) bestMatch(target []byte, p, limit int) (addr, length int) {
	if p+4 > len(target) || limit < 4 {
		return 0, 0
	}
	c, ok := m.head[hash4(target[p:])]
	if !ok {
		return 0, 0
	}
	for probes := 0; c >= 0 && probes < maxChain; probes, c = probes+1, m.next[c] {
		n := matchLen(m.dict, int(c), target, p, limit)
		if n > length {
			addr, length = int(c), n
		}
	}
	return addr, length
}

func matchLen(dict []byte, a int, target []byte, b, limit int) int {
	n := 0
	for n < limit && a+n < len(dict) && dict[a+n] == target[b+n] {
		n++
	}
	return n
}

// directMatchLen compares dict starting at dictPos against target
// starting at p byte by byte, with no hashing and no 4-byte minimum —
// unlike matcher and selfMatcher above, which can't even detect a match
// shorter than 4 bytes. It exists specifically for SKIP: a run as short
// as 1 byte at the dictionary cursor's exact current position can still
// be worth a SKIP instruction, since SKIP has no address field to pay
// for (the cost gate in bestCandidateAt decides whether it actually is).
func directMatchLen(dict []byte, dictPos int, target []byte, p, limit int) int {
	n := 0
	for n < limit && dictPos+n < len(dict) && dict[dictPos+n] == target[p+n] {
		n++
	}
	return n
}

// selfMatcher finds matches of target against target itself (bytes
// already produced), feeding COPY_TARGET instructions. Unlike matcher
// above, it can't be built once upfront: a position is only a valid match
// candidate once the scan has passed it, so insert must be called for
// every position, in increasing order, as planInstructions advances.
// Matches may legitimately overlap into bytes not yet "produced" at the
// time a match is found — see COPY_TARGET's doc in format.go — so there
// is no equivalent of matcher's dict-boundary cap here.
type selfMatcher struct {
	target      []byte
	maxDistance int // matches start at most this many bytes back
	head        map[uint32]int32
	next        []int32 // next[p] = the previous position with target[p:p+4]'s key, or -1
}

func newSelfMatcher(target []byte, maxDistance int) *selfMatcher {
	return &selfMatcher{
		target:      target,
		maxDistance: maxDistance,
		head:        make(map[uint32]int32, len(target)),
		next:        make([]int32, 0, len(target)),
	}
}

// insert records position p so later calls to bestMatch can find it. p
// must not have been inserted before, and insert must be called, in
// increasing order of p, for every position up to the one currently being
// matched.
func (m *selfMatcher) insert(p int) {
	if p+4 > len(m.target) {
		m.next = append(m.next, -1)
		return
	}
	h := hash4(m.target[p:])
	prev, ok := m.head[h]
	if !ok {
		prev = -1
	}
	m.next = append(m.next, prev)
	m.head[h] = int32(p)
}

// bestMatch returns the longest match at p against any previously
// inserted position at most maxDistance back, capped at limit bytes, and
// that position's address.
func (m *selfMatcher) bestMatch(p, limit int) (addr, length int) {
	if p+4 > len(m.target) || limit < 4 {
		return 0, 0
	}
	c, ok := m.head[hash4(m.target[p:])]
	if !ok {
		return 0, 0
	}
	// The chain runs from the nearest position back, so the first one
	// out of reach ends it.
	for probes := 0; c >= 0 && probes < maxChain && p-int(c) <= m.maxDistance; probes, c = probes+1, m.next[c] {
		n := matchLen(m.target, int(c), m.target, p, limit)
		if n > length {
			addr, length = int(c), n
		}
	}
	return addr, length
}
