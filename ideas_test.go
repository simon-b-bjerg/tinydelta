// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

//go:build ideas

package tinydelta

// Experiments for ideas beyond coded patches (docs/ideas.md). Not part of
// the format: each experiment estimates the coded size of a variant with
// the same adaptive model as coded.go (CABAC states, bit trees), as the
// ideal code length (sum of -log2 p), which the range coder matches to
// within a few bytes per patch.
//
//	go test -tags ideas -run TestIdeas -v . \
//	    -args -firmware <pairs file> -config <pairs file>
//
// A pairs file has one pair per line: name, old file, new file.

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
)

var (
	ideasFirmware = flag.String("firmware", "", "firmware pairs file")
	ideasConfig   = flag.String("config", "", "configuration pairs file")
)

type ideaPair struct {
	name, item   string
	dict, target []byte
}

func loadIdeaPairs(t *testing.T, path string) []ideaPair {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var pairs []ideaPair
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		if len(p) != 3 {
			continue
		}
		d, err1 := os.ReadFile(p[1])
		g, err2 := os.ReadFile(p[2])
		if err1 != nil || err2 != nil {
			t.Fatal(err1, err2)
		}
		item := p[1][:strings.LastIndex(p[1], "/")]
		pairs = append(pairs, ideaPair{p[0], item, d, g})
	}
	return pairs
}

// ---- A generic adaptive model: bit trees of CABAC states, keyed freely.

type ideaModel struct {
	states map[[2]int]byte // (tree, node) -> state
	prior  map[[2]int]byte // initial states, if trained
	bits   float64
	counts map[[2]int][2]int // for training priors
}

func newIdeaModel(prior map[[2]int]byte) *ideaModel {
	return &ideaModel{states: map[[2]int]byte{}, prior: prior, counts: map[[2]int][2]int{}}
}

func (m *ideaModel) bit(tree, node int, bit uint32) {
	k := [2]int{tree, node}
	s, ok := m.states[k]
	if !ok && m.prior != nil {
		s = m.prior[k]
	}
	lps := float64(codedLPS[s>>1]) / (1 << codedProbBits)
	p := 1 - lps
	if byte(bit) != s&1 {
		p = lps
	}
	m.bits -= math.Log2(p)
	c := m.counts[k]
	c[bit]++
	m.counts[k] = c
	state, mps := s>>1, s&1
	if byte(bit) == mps {
		state = min(state+1, codedStates-1)
	} else {
		if state == 0 {
			mps ^= 1
		}
		state = codedNextLPS[state]
	}
	m.states[k] = state<<1 | mps
}

// tree codes the low n bits of v, highest first, in bit tree number tree.
func (m *ideaModel) tree(tree int, n uint, v uint32) {
	node := 1
	for sh := int(n) - 1; sh >= 0; sh-- {
		b := v >> uint(sh) & 1
		m.bit(tree, node, b)
		node = node*2 + int(b)
	}
}

func (m *ideaModel) bytes() int { return int(math.Ceil(m.bits / 8)) }

// trainPrior turns bit counts into initial states: the state whose
// probability is nearest each context's observed one.
func trainPrior(counts map[[2]int][2]int) map[[2]int]byte {
	prior := map[[2]int]byte{}
	for k, c := range counts {
		n := c[0] + c[1]
		if n < 4 {
			continue
		}
		mps, lpsCount := byte(0), c[1]
		if c[1] > c[0] {
			mps, lpsCount = 1, c[0]
		}
		// Shrink toward even odds: a prior should not be too sure.
		p := (float64(lpsCount) + 1) / (float64(n) + 2)
		best, bestErr := 0, math.Inf(1)
		for s := 0; s < codedStates; s++ {
			if e := math.Abs(math.Log(float64(codedLPS[s])/(1<<codedProbBits)) - math.Log(p)); e < bestErr {
				best, bestErr = s, e
			}
		}
		prior[k] = byte(best)<<1 | mps
	}
	return prior
}

// ---- Walking an instruction stream: every byte with its context.

const (
	tokFirst = iota // first byte of an instruction varint
	tokCont         // later byte of any varint
	tokDelta        // first byte of a COPY/COPY_TARGET delta
	tokLiteral
	tokDiff
)

type ideaToken struct {
	kind   int
	b      byte
	prevOp int    // tokFirst: the previous opcode
	pos    int    // tokLiteral/tokDiff: target position of the byte
	old    byte   // tokLiteral/tokDiff: the dictionary byte at the cursor
	index  int    // tokLiteral/tokDiff: index within the instruction
	op     int    // tokCont: the instruction's opcode, or -1 for a delta
	value  uint64 // tokDelta: the whole delta varint
	n      int    // tokDelta: its length in bytes
}

// ideaTokens walks a plain patch. dict gives the old bytes at the cursor.
func ideaTokens(plain, dict []byte) []ideaToken {
	version, s := plain[0], plain[headerLen:]
	var toks []ideaToken
	pos, cursor, prevOp := 0, 0, codedPrevStart
	i := 0
	readVarint := func(first ideaToken, op int) uint64 {
		var v uint64
		var shift uint
		for k := 0; ; k++ {
			b := s[i]
			i++
			t := ideaToken{kind: tokCont, b: b, op: op}
			if k == 0 {
				t = first
				t.b = b
			}
			toks = append(toks, t)
			v |= uint64(b&0x7F) << shift
			shift += 7
			if b&0x80 == 0 {
				return v
			}
		}
	}
	for i < len(s) {
		v := readVarint(ideaToken{kind: tokFirst, prevOp: prevOp}, -2)
		op, n := int(v&3), int(v>>2)
		// Later bytes of the instruction varint carry its opcode.
		for k := len(toks) - 1; k >= 0 && toks[k].kind == tokCont && toks[k].op == -2; k-- {
			toks[k].op = op
		}
		prevOp = op
		switch {
		case op == opCopy:
			at := len(toks)
			raw := readVarint(ideaToken{kind: tokDelta}, -1)
			toks[at].value, toks[at].n = raw, len(toks)-at
			d := zigzagDecode(raw)
			cursor = int(int64(cursor) + d)
			pos, cursor = pos+n, cursor+n
		case op == opTargetCopy && version == FormatVersion:
			readVarint(ideaToken{kind: tokDelta}, -1)
			pos += n
		case op == opSkip:
			pos, cursor = pos+n, cursor+n
		case op == opAdd || op == opAddDiff:
			kind := tokLiteral
			if op == opAddDiff {
				kind = tokDiff
			}
			for k := 0; k < n; k++ {
				var old byte
				if cursor+k >= 0 && cursor+k < len(dict) {
					old = dict[cursor+k]
				}
				toks = append(toks, ideaToken{kind: kind, b: s[i], pos: pos + k, old: old, index: k})
				i++
			}
			pos, cursor = pos+n, cursor+n
		}
	}
	return toks
}

// ---- Models: how each token is coded.

// codeBaseline is coded.go's model, layout B (tables: 0 shared, 1 diff).
func codeBaseline(m *ideaModel, toks []ideaToken) {
	for _, t := range toks {
		switch t.kind {
		case tokFirst:
			m.tree(100+t.prevOp, 2, uint32(t.b&3))
			m.tree(200+int(t.b&3), 6, uint32(t.b>>2))
		case tokDiff:
			m.tree(1, 8, uint32(t.b))
		default:
			m.tree(0, 8, uint32(t.b))
		}
	}
}

// codeLanes gives ADD_DIFF bytes one table per byte lane (position mod 4).
func codeLanes(m *ideaModel, toks []ideaToken) {
	for _, t := range toks {
		switch t.kind {
		case tokFirst:
			m.tree(100+t.prevOp, 2, uint32(t.b&3))
			m.tree(200+int(t.b&3), 6, uint32(t.b>>2))
		case tokDiff:
			m.tree(10+t.pos&3, 8, uint32(t.b))
		default:
			m.tree(0, 8, uint32(t.b))
		}
	}
}

// codePredicted codes each ADD_DIFF byte first as "same as the last
// difference byte in this lane" (one state per lane), and only if not,
// with the diff table. The decoder keeps the last four difference bytes.
func codePredicted(m *ideaModel, toks []ideaToken) {
	var last [4]byte
	for _, t := range toks {
		switch t.kind {
		case tokFirst:
			m.tree(100+t.prevOp, 2, uint32(t.b&3))
			m.tree(200+int(t.b&3), 6, uint32(t.b>>2))
		case tokDiff:
			lane := t.pos & 3
			if t.b == last[lane] {
				m.bit(20, lane, 1)
			} else {
				m.bit(20, lane, 0)
				m.tree(1, 8, uint32(t.b))
			}
			last[lane] = t.b
		default:
			m.tree(0, 8, uint32(t.b))
		}
	}
}

// codePredictedCtx is codePredicted with the flag's state also chosen by
// whether the previous flag in this lane hit: 8 states.
func codePredictedCtx(m *ideaModel, toks []ideaToken) { codePredictedLanes(m, toks, 4) }

// codePredictedLanes is codePredictedCtx with lanes of the given width:
// 4 for 32-bit addresses, 2 for 16-bit ones (MSP430), 1 for none.
func codePredictedLanes(m *ideaModel, toks []ideaToken, width int) {
	var last [4]byte
	var hit [4]int
	for _, t := range toks {
		switch t.kind {
		case tokFirst:
			m.tree(100+t.prevOp, 2, uint32(t.b&3))
			m.tree(200+int(t.b&3), 6, uint32(t.b>>2))
		case tokDiff:
			lane := t.pos & (width - 1)
			ctx := lane*2 + hit[lane]
			if t.b == last[lane] {
				m.bit(21, ctx, 1)
				hit[lane] = 1
			} else {
				m.bit(21, ctx, 0)
				hit[lane] = 0
				m.tree(1, 8, uint32(t.b))
			}
			last[lane] = t.b
		default:
			m.tree(0, 8, uint32(t.b))
		}
	}
}

// withRepDelta codes a COPY's delta as one flag "same delta as the last
// COPY" (one state) and, if not, as before; the rest through base.
func withRepDelta(base func(*ideaModel, []ideaToken)) func(*ideaModel, []ideaToken) {
	return func(m *ideaModel, toks []ideaToken) {
		var last uint64
		have := false
		// base must see the tokens in order for its own state (lanes),
		// so hand it runs between deltas.
		start := 0
		for i := 0; i < len(toks); i++ {
			t := toks[i]
			if t.kind != tokDelta || t.n == 0 {
				continue
			}
			base(m, toks[start:i])
			if have && t.value == last {
				m.bit(40, 1, 1)
			} else {
				m.bit(40, 1, 0)
				base(m, toks[i:i+t.n])
			}
			last, have = t.value, true
			i += t.n - 1
			start = i + 1
		}
		base(m, toks[start:])
	}
}

// codeMatchedLiteral codes ADD literals LZMA-style against the old byte
// at the cursor: while the bits so far equal the old byte's, the old
// byte's next bit is part of the context. Costs a second literal table.
func codeMatchedLiteral(m *ideaModel, toks []ideaToken, base func(*ideaModel, []ideaToken)) {
	for _, t := range toks {
		if t.kind != tokLiteral {
			base(m, []ideaToken{t})
			continue
		}
		node, matching := 1, true
		for sh := 7; sh >= 0; sh-- {
			b := uint32(t.b>>uint(sh)) & 1
			if matching {
				ob := int(t.old>>uint(sh)) & 1
				m.bit(30+ob, node, b)
				matching = int(b) == ob
			} else {
				m.bit(0, node, b)
			}
			node = node*2 + int(b)
		}
	}
}

// ---- Encoder variants (encoder only, same format).

// encodeCoversFold is encodeCovers with unchanged runs inside a cover
// shorter than fold coded as zero difference bytes instead of SKIP.
func encodeCoversFold(dict, target []byte, fold int) []byte {
	if fold <= 0 {
		return encodeCovers(dict, target, MaxSize)
	}
	var out []byte
	cursor := 0
	emit := func(op byte, n int) { out = appendVarint(out, uint64(n)<<2|uint64(op)) }
	pending := 0
	for _, c := range findCovers(dict, target) {
		if c.extra > 0 {
			pending += c.extra
			continue
		}
		if pending > 0 {
			emit(opAdd, pending)
			out = append(out, target[c.newPos-pending:c.newPos]...)
			cursor += pending
			pending = 0
		}
		if c.oldPos != cursor {
			n := matchLen(dict, c.oldPos, target, c.newPos, c.length)
			emit(opCopy, n)
			out = appendVarint(out, zigzagEncode(int64(c.oldPos)-int64(cursor)))
			cursor = c.oldPos + n
			c.newPos, c.oldPos, c.length = c.newPos+n, c.oldPos+n, c.length-n
		}
		var diff []byte
		flush := func() {
			if len(diff) > 0 {
				emit(opAddDiff, len(diff))
				out = append(out, diff...)
				diff = diff[:0]
			}
		}
		for p, end := c.newPos, c.newPos+c.length; p < end; {
			if run := matchLen(dict, cursor, target, p, end-p); run > 0 {
				if run < fold && len(diff) > 0 && p+run < end {
					for k := 0; k < run; k++ {
						diff = append(diff, 0)
					}
				} else {
					flush()
					emit(opSkip, run)
				}
				p, cursor = p+run, cursor+run
				continue
			}
			diff = append(diff, target[p]-dict[cursor])
			p, cursor = p+1, cursor+1
		}
		flush()
	}
	if pending > 0 {
		emit(opAdd, pending)
		out = append(out, target[len(target)-pending:]...)
	}
	return withVersion(FormatVersion2, out)
}

// encodeCoversCarry is encodeCovers for an ADD_DIFF that adds whole
// 32-bit little-endian words: the carry out of each byte goes into the
// next, up to the end of each aligned word (by target position) and of
// the instruction. A moved pointer then gives the same difference bytes
// whether or not the addition carries into its second byte.
func encodeCoversCarry(dict, target []byte) []byte { return encodeCoversCarryWidth(dict, target, 4) }

// encodeCoversCarryWidth is encodeCoversCarry for words of width bytes.
func encodeCoversCarryWidth(dict, target []byte, width int) []byte {
	plain := encodeCovers(dict, target, MaxSize)
	// Same instructions; recompute the ADD_DIFF bytes with carries.
	out := append([]byte{}, plain...)
	i, pos, cursor := 0, 0, 0
	for i < len(out) {
		v, n := readVarintAt(out, i)
		i += n
		op, length := int(v&3), int(v>>2)
		switch op {
		case opCopy:
			d, n := readVarintAt(out, i)
			i += n
			cursor = int(int64(cursor)+zigzagDecode(d)) + length
			pos += length
		case opSkip:
			pos, cursor = pos+length, cursor+length
		case opAdd:
			i += length
			pos, cursor = pos+length, cursor+length
		case opAddDiff:
			carry := 0
			for k := 0; k < length; k++ {
				o, t := int(dict[cursor+k]), int(target[pos+k])
				d := (t - o - carry) & 0xFF
				out[i+k] = byte(d)
				carry = (o + d + carry) >> 8
				if (pos+k)&(width-1) == width-1 {
					carry = 0
				}
			}
			i += length
			pos, cursor = pos+length, cursor+length
		}
	}
	return withVersion(FormatVersion2, out)
}

func readVarintAt(b []byte, i int) (uint64, int) {
	var v uint64
	var shift uint
	for n := 0; ; n++ {
		c := b[i+n]
		v |= uint64(c&0x7F) << shift
		shift += 7
		if c&0x80 == 0 {
			return v, n + 1
		}
	}
}

// thumbBCJ converts Thumb BL instructions (two halfwords, 0xF000 and
// 0xF800 prefixes) from relative to absolute targets, as xz's ARM-Thumb
// filter does. A call whose callee moved changes by the move either way;
// relative, a call from moved code to code that did not move changes too,
// absolute it does not, and a moved call to a moved callee keeps its
// bytes relative but not absolute. Which wins is what this measures.
// Returns a filtered copy.
func thumbBCJ(b []byte) []byte {
	out := append([]byte{}, b...)
	for i := 0; i+4 <= len(out); i += 2 {
		if out[i+1]&0xF8 == 0xF0 && out[i+3]&0xF8 == 0xF8 {
			src := uint32(out[i+1]&7)<<19 | uint32(out[i])<<11 | uint32(out[i+3]&7)<<8 | uint32(out[i+2])
			dest := (src<<1 + uint32(i+4)) >> 1
			out[i+1] = 0xF0 | byte(dest>>19&7)
			out[i] = byte(dest >> 11)
			out[i+3] = 0xF8 | byte(dest>>8&7)
			out[i+2] = byte(dest)
			i += 2
		}
	}
	return out
}

// ---- The experiments.

type ideaVariant struct {
	name  string
	ram   string // decoder RAM for the tables, beyond layout B's 777 B
	patch func(p ideaPair) []byte
	code  func(m *ideaModel, toks []ideaToken)
}

func TestIdeas(t *testing.T) {
	sets := []struct {
		name  string
		pairs []ideaPair
	}{
		{"firmware", loadIdeaPairs(t, *ideasFirmware)},
		{"configuration", loadIdeaPairs(t, *ideasConfig)},
	}
	v1 := func(p ideaPair) []byte { b, _ := Encode(p.dict, p.target); return b }
	v2 := func(p ideaPair) []byte { return withVersion(FormatVersion2, encodeCovers(p.dict, p.target, MaxSize)) }
	variants := []ideaVariant{
		{"baseline, layout B, v1", "", v1, codeBaseline},
		{"baseline, layout B, v2", "", v2, codeBaseline},
		{"diff tables per lane, v2", "+765 B", v2, codeLanes},
		{"predicted diff byte, v2", "+4 B", v2, codePredicted},
		{"matched literal, v1", "+510 B", v1, func(m *ideaModel, toks []ideaToken) { codeMatchedLiteral(m, toks, codeBaseline) }},
		{"matched literal, v2", "+510 B", v2, func(m *ideaModel, toks []ideaToken) { codeMatchedLiteral(m, toks, codeBaseline) }},
	}
	variants = append(variants,
		ideaVariant{"predicted diff byte, flag context, v2", "+4 B", v2, codePredictedCtx},
		ideaVariant{"predicted diff byte, 16-bit lanes, v2", "+2 B", v2, func(m *ideaModel, t []ideaToken) { codePredictedLanes(m, t, 2) }},
		ideaVariant{"predicted diff byte, no lanes, v2", "+1 B", v2, func(m *ideaModel, t []ideaToken) { codePredictedLanes(m, t, 1) }},
		ideaVariant{"repeated delta flag, v1", "+1 B", v1, withRepDelta(codeBaseline)},
		ideaVariant{"repeated delta flag, v2", "+1 B", v2, withRepDelta(codeBaseline)},
	)
	carry := func(p ideaPair) []byte { return encodeCoversCarry(p.dict, p.target) }
	variants = append(variants,
		ideaVariant{"word carry in ADD_DIFF, v2", "format change", carry, codeBaseline},
		ideaVariant{"word carry + predicted diff byte, v2", "format change, +4 B", carry, codePredicted},
		ideaVariant{"word carry + diff tables per lane, v2", "format change, +765 B", carry, codeLanes},
		ideaVariant{"word carry + predicted diff byte, 16-bit lanes, v2", "format change, +2 B", carry, func(m *ideaModel, t []ideaToken) { codePredictedLanes(m, t, 2) }},
		ideaVariant{"16-bit carry + predicted, 16-bit lanes, v2", "format change, +2 B", func(p ideaPair) []byte { return encodeCoversCarryWidth(p.dict, p.target, 2) }, func(m *ideaModel, t []ideaToken) { codePredictedLanes(m, t, 2) }},
	)
	bcj := func(p ideaPair) []byte {
		return withVersion(FormatVersion2, encodeCovers(thumbBCJ(p.dict), thumbBCJ(p.target), MaxSize))
	}
	variants = append(variants,
		ideaVariant{"Thumb BL filter, v2", "decoder filter", bcj, codeBaseline},
		ideaVariant{"Thumb BL filter + predicted diff byte, v2", "decoder filter, +4 B", bcj, codePredicted},
	)
	for _, fold := range []int{2, 4} {
		fold := fold
		variants = append(variants, ideaVariant{fmt.Sprintf("v2, runs under %d folded into diffs", fold), "encoder only",
			func(p ideaPair) []byte { return encodeCoversFold(p.dict, p.target, fold) }, codeBaseline})
		variants = append(variants, ideaVariant{fmt.Sprintf("v2, runs under %d folded, predicted diff", fold), "+4 B",
			func(p ideaPair) []byte { return encodeCoversFold(p.dict, p.target, fold) }, codePredicted})
	}
	for _, set := range sets {
		if len(set.pairs) == 0 {
			continue
		}
		t.Logf("== %s: %d pairs", set.name, len(set.pairs))
		var real int
		for _, p := range set.pairs {
			patch, _ := EncodeWith(p.dict, p.target, EncodeOptions{CoderTables: 777})
			real += len(patch.Bytes)
		}
		t.Logf("%-46s %10d B   (EncodeWith, CoderTables 777)", "today: smallest of plain and coded", real)
		best := make([]int, len(set.pairs)) // per pair, the smallest variant so far (min of v1/v2 baseline)
		for vi, v := range variants {
			total := 0
			for i, p := range set.pairs {
				plain := v.patch(p)
				m := newIdeaModel(nil)
				v.code(m, ideaTokens(plain, p.dict))
				n := 3 + varintLen(uint64(len(plain)-1)) + m.bytes()
				if vi < 2 {
					if best[i] == 0 || n < best[i] {
						best[i] = n
					}
				}
				total += n
			}
			t.Logf("%-46s %10d B   %s", v.name, total, v.ram)
			if vi == 1 {
				sum := 0
				for _, b := range best {
					sum += b
				}
				t.Logf("%-46s %10d B", "baseline, better of v1 and v2 per pair", sum)
			}
		}
		ideaPriors(t, set.pairs, v1, v2)
	}
}

// ideaPriors measures starting the states from trained priors instead of
// even odds: trained on every item but the pair's own (leave one item out).
func ideaPriors(t *testing.T, pairs []ideaPair, v1, v2 func(ideaPair) []byte) {
	items := map[string]bool{}
	for _, p := range pairs {
		items[p.item] = true
	}
	var names []string
	for k := range items {
		names = append(names, k)
	}
	sort.Strings(names)
	if len(names) < 2 {
		t.Logf("priors: need pairs from at least two items")
		return
	}
	for _, enc := range []struct {
		name string
		fn   func(ideaPair) []byte
	}{{"v1", v1}, {"v2", v2}} {
		toks := make([][]ideaToken, len(pairs))
		for i, p := range pairs {
			toks[i] = ideaTokens(enc.fn(p), p.dict)
		}
		plainTotal, priorTotal, smallTotal := 0, 0, 0
		for _, item := range names {
			train := newIdeaModel(nil)
			for i, p := range pairs {
				if p.item != item {
					// Train on each pair from a fresh model, so the
					// counts are what a patch's start looks like too.
					m := newIdeaModel(nil)
					codeBaseline(m, toks[i])
					for k, c := range m.counts {
						tc := train.counts[k]
						tc[0] += c[0]
						tc[1] += c[1]
						train.counts[k] = tc
					}
				}
			}
			prior := trainPrior(train.counts)
			small := map[[2]int]byte{} // opcode and length trees only: 267 states
			for k, v := range prior {
				if k[0] >= 100 {
					small[k] = v
				}
			}
			for i, p := range pairs {
				if p.item != item {
					continue
				}
				a, b, c := newIdeaModel(nil), newIdeaModel(prior), newIdeaModel(small)
				codeBaseline(a, toks[i])
				codeBaseline(b, toks[i])
				codeBaseline(c, toks[i])
				plainTotal += a.bytes()
				priorTotal += b.bytes()
				smallTotal += c.bytes()
			}
		}
		t.Logf("%-46s %10d B   (even odds: %d B; flash +777 B)", "trained starting states, "+enc.name, priorTotal, plainTotal)
		t.Logf("%-46s %10d B   (flash +267 B)", "trained opcode/length states only, "+enc.name, smallTotal)
	}
}

// TestIdeasShipped estimates what a device would receive: per pair, the
// smallest of the plain patch and the coded ones (v1, v2), today and with
// the two cheapest ideas, trained starting states (priors trained on the
// other items of both sets: one table in the decoder for all data) and
// the predicted difference byte.
func TestIdeasShipped(t *testing.T) {
	fw, cfg := loadIdeaPairs(t, *ideasFirmware), loadIdeaPairs(t, *ideasConfig)
	all := append(append([]ideaPair{}, fw...), cfg...)
	v1 := func(p ideaPair) []byte { b, _ := Encode(p.dict, p.target); return b }
	v2 := func(p ideaPair) []byte { return withVersion(FormatVersion2, encodeCovers(p.dict, p.target, MaxSize)) }
	type enc struct {
		plain []byte
		toks  [2][]ideaToken
	}
	encs := make([]enc, len(all))
	for i, p := range all {
		a, b := v1(p), v2(p)
		encs[i] = enc{a, [2][]ideaToken{ideaTokens(a, p.dict), ideaTokens(b, p.dict)}}
	}
	// Priors per held-out item, trained with the predicted-byte model on
	// everything else.
	priorFor := map[string]map[[2]int]byte{}
	for _, p := range all {
		if _, ok := priorFor[p.item]; ok {
			continue
		}
		counts := map[[2]int][2]int{}
		for j, q := range all {
			if q.item == p.item {
				continue
			}
			for _, tk := range encs[j].toks {
				m := newIdeaModel(nil)
				codePredictedCtx(m, tk)
				for k, c := range m.counts {
					cc := counts[k]
					cc[0] += c[0]
					cc[1] += c[1]
					counts[k] = cc
				}
			}
		}
		priorFor[p.item] = trainPrior(counts)
	}
	report := func(name string, pairs []ideaPair, offset int) {
		var today, pred, prior, both, codedWins int
		for i := range pairs {
			e := encs[offset+i]
			hdr := 3 + varintLen(uint64(len(e.plain)-1))
			best := [4]int{len(e.plain), len(e.plain), len(e.plain), len(e.plain)}
			for _, tk := range e.toks {
				for k, code := range []struct {
					fn    func(*ideaModel, []ideaToken)
					prior map[[2]int]byte
				}{{codeBaseline, nil}, {codePredictedCtx, nil}, {codeBaseline, priorFor[pairs[i].item]}, {codePredictedCtx, priorFor[pairs[i].item]}} {
					m := newIdeaModel(code.prior)
					code.fn(m, tk)
					best[k] = min(best[k], hdr+m.bytes())
				}
			}
			today += best[0]
			pred += best[1]
			prior += best[2]
			both += best[3]
			if best[3] < len(e.plain) {
				codedWins++
			}
		}
		pct := func(n int) string { return fmt.Sprintf("%d B (%+.1f%%)", n, 100*float64(n-today)/float64(today)) }
		t.Logf("== %s, %d pairs, smallest of plain and coded per pair", name, len(pairs))
		t.Logf("  today:                         %d B", today)
		t.Logf("  predicted diff byte:           %s", pct(pred))
		t.Logf("  trained starting states:       %s", pct(prior))
		t.Logf("  both:                          %s; coded is smaller in %d of %d pairs", pct(both), codedWins, len(pairs))
	}
	report("firmware", fw, 0)
	report("configuration", cfg, len(fw))
}
