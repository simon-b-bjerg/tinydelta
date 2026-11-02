// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

//go:build ideas

package tinydelta

// More experiments (docs/ideas.md): the coded header and flush, starting
// states trained on configuration.

import (
	"bytes"
	"fmt"
	"testing"
)

// finishShort ends the range coder with as few bytes as possible, for a
// decoder that reads bytes past the end of the patch as 0: the value in
// [low, low+range) with the most trailing zero bytes.
func (e *rangeEncoder) finishShort() []byte {
	for k := 1; k <= 4; k++ {
		unit := uint64(1) << (32 - 8*k)
		v := (e.low + unit - 1) &^ (unit - 1)
		if v < e.low+uint64(e.rng) {
			e.low = v
			for i := 0; i < 5; i++ { // push out cache and the k significant bytes
				e.shiftLow()
			}
			out := e.out
			for len(out) > 0 && out[len(out)-1] == 0 {
				out = out[:len(out)-1]
			}
			return out
		}
	}
	return e.finish()
}

func encodeCodedShort(plain []byte, layout int) []byte {
	version, stream := plain[0], plain[headerLen:]
	m, w, e := newCodedModel(layout), newCodedWalker(version), newRangeEncoder()
	for _, b := range stream {
		codeByte(m, w, b, func(i int, bit uint32) (uint32, error) { e.bit(m, i, bit); return bit, nil })
	}
	out := []byte{codedFlag | version, byte(layout)}
	out = appendVarint(out, uint64(len(stream)))
	return append(out, e.finishShort()...)
}

func TestIdeasOverhead(t *testing.T) {
	for _, set := range []struct {
		name string
		path string
	}{{"firmware", *ideasFirmware}, {"configuration", *ideasConfig}} {
		pairs := loadIdeaPairs(t, set.path)
		if len(pairs) == 0 {
			continue
		}
		var today, short, packed, plainSum, nowCoded, thenCoded int
		for _, p := range pairs {
			v1, _ := Encode(p.dict, p.target)
			v2 := withVersion(FormatVersion2, encodeCovers(p.dict, p.target, MaxSize))
			bestToday, bestShort := len(v1), len(v1)
			for _, plain := range [][]byte{v1, v2} {
				c := encodeCoded(plain, codedLayoutB)
				s := encodeCodedShort(plain, codedLayoutB)
				back, err := decodeCoded(append(append([]byte{}, s...), 0, 0, 0, 0, 0), MaxSize)
				if err != nil || !bytes.Equal(back, plain) {
					t.Fatalf("%s: short flush does not decode: %v", p.name, err)
				}
				bestToday = min(bestToday, len(c))
				bestShort = min(bestShort, len(s))
			}
			today += bestToday
			short += bestShort
			// And the features byte folded into the version byte.
			packedLen := len(v1)
			for _, plain := range [][]byte{v1, v2} {
				packedLen = min(packedLen, len(encodeCodedShort(plain, codedLayoutB))-1)
			}
			packed += packedLen
			plainSum += len(v1)
			if bestToday < len(v1) {
				nowCoded++
			}
			if packedLen < len(v1) {
				thenCoded++
			}
		}
		pct := func(n int) string { return fmt.Sprintf("%d B (%+.1f%%)", n, 100*float64(n-today)/float64(today)) }
		t.Logf("== %s, %d pairs: smallest of plain (v1) and coded, layout B", set.name, len(pairs))
		t.Logf("  today:                                   %d B, coded in %d pairs", today, nowCoded)
		t.Logf("  shortest flush:                          %s", pct(short))
		t.Logf("  + layout in the version byte:            %s, coded in %d pairs", pct(packed), thenCoded)
	}
}

// TestIdeasConfigPriors: configuration only, with starting states trained
// on the other configuration items, and the short header and flush
// (estimated as 1 + varint + ideal bytes + 1).
func TestIdeasConfigPriors(t *testing.T) {
	cfg := loadIdeaPairs(t, *ideasConfig)
	toks := make([][]ideaToken, len(cfg))
	plains := make([][]byte, len(cfg))
	for i, p := range cfg {
		plains[i], _ = Encode(p.dict, p.target)
		toks[i] = ideaTokens(plains[i], p.dict)
	}
	var today, short, withPrior, coded int
	for i, p := range cfg {
		counts := map[[2]int][2]int{}
		for j, q := range cfg {
			if q.item == p.item {
				continue
			}
			m := newIdeaModel(nil)
			codeBaseline(m, toks[j])
			for k, c := range m.counts {
				cc := counts[k]
				cc[0] += c[0]
				cc[1] += c[1]
				counts[k] = cc
			}
		}
		prior := trainPrior(counts)
		hdr := 1 + varintLen(uint64(len(plains[i])-1)) + 1
		a, b := newIdeaModel(nil), newIdeaModel(prior)
		codeBaseline(a, toks[i])
		codeBaseline(b, toks[i])
		c := encodeCoded(plains[i], codedLayoutB)
		today += min(len(plains[i]), len(c))
		short += min(len(plains[i]), hdr+a.bytes())
		n := min(len(plains[i]), hdr+b.bytes())
		withPrior += n
		if n < len(plains[i]) {
			coded++
		}
	}
	pct := func(n int) string { return fmt.Sprintf("%d B (%+.1f%%)", n, 100*float64(n-today)/float64(today)) }
	t.Logf("configuration, %d pairs, v1: today %d B; short header %s; + configuration starting states %s, coded in %d pairs",
		len(cfg), today, pct(short), pct(withPrior), coded)
}
