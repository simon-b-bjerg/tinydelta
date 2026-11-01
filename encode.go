// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"compress/flate"
	"fmt"
)

// Encode produces the smallest patch it can find that reconstructs target
// from dict via Decode, using an optimal parse (dynamic programming over
// exact instruction costs; see encodeOptimal). It never returns a patch
// larger than EncodeGreedy's, and falls back to EncodeGreedy outright for
// inputs too large for the DP table. Encoding costs far more than
// EncodeGreedy (roughly 100-300 ms for a 3.5 KB config, against under
// 1 ms); decoding is unaffected, since the format is the same. dict and
// target must each be at most MaxSize bytes, or Encode returns
// ErrTooLarge.
func Encode(dict, target []byte) ([]byte, error) {
	if err := checkSizes(dict, target); err != nil {
		return nil, err
	}
	return withHeader(encode(dict, target, MaxSize)), nil
}

// EncodeOptions describes the device's decoder to EncodeWith, which
// returns the smallest patch that decoder can decode. The zero value is a
// decoder with no limits: plain, single-pass patches with any copy.
type EncodeOptions struct {
	// Greedy selects the fast greedy encoder (see EncodeGreedy) instead
	// of the optimizing one (see Encode), for every patch considered.
	Greedy bool

	// Window, if positive, keeps every copy from the target already
	// produced (COPY_TARGET) within the last Window bytes. A decoder then
	// reads its own output only from RAM: the C callback decoder from its
	// buffer, never from flash, so its target_read may be NULL. Set it to
	// that buffer's size. 0, the default, means no limit. Smaller windows
	// give somewhat larger patches.
	Window int

	// CoderTables, if positive, allows coded patches (see coded.go): the
	// instruction stream range-coded, decoded in one pass. It is the size
	// of the decoder's buffer for the coder's tables, tables_size in C
	// (a buffer that may live on the stack); the encoder uses every table
	// layout that fits (CodedTablesSize: 522, 777 or 1287 bytes). 0, the
	// default, means a decoder without the coder.
	CoderTables int

	// TwoPass allows a two-pass patch, which the device decodes twice:
	// first against an empty dictionary, then the result against the
	// dictionary, with room to store that intermediate patch. On large
	// changes to firmware it is typically about half the size of a plain
	// one, and costs no decoder code. Patch.Passes says which was
	// returned.
	TwoPass bool

	// MaxInstructionLen, if positive, caps the number of target bytes
	// any single instruction may produce: longer ADD, SKIP, COPY and
	// COPY_TARGET runs are split into several instructions. Use it when
	// a decoder handles one instruction at a time and needs a bound on
	// it. 0 means no limit beyond MaxSize. The patch format and the
	// decoders are unchanged.
	MaxInstructionLen int
}

// Patch is EncodeWith's result.
type Patch struct {
	// Bytes is the patch, as sent.
	Bytes []byte

	// Passes is how many times the device decodes it: 1, or 2 for a
	// two-pass patch (EncodeOptions.TwoPass): first Bytes against an
	// empty dictionary, then the result against the dictionary. The
	// update has to carry it, as it is not part of the patch.
	Passes int
}

// EncodeWith returns the smallest patch the decoder described by opts can
// decode: plain, coded (CoderTables) or two-pass (TwoPass), whichever is
// smallest, single-pass on a tie. With a MaxInstructionLen limit,
// Encode's guarantee becomes: never larger than EncodeGreedy's patch
// under the same limit.
func EncodeWith(dict, target []byte, opts EncodeOptions) (Patch, error) {
	if err := checkSizes(dict, target); err != nil {
		return Patch{}, err
	}
	switch {
	case opts.MaxInstructionLen < 0:
		return Patch{}, fmt.Errorf("tinydelta: negative MaxInstructionLen %d", opts.MaxInstructionLen)
	case opts.Window < 0:
		return Patch{}, fmt.Errorf("tinydelta: negative Window %d", opts.Window)
	case opts.CoderTables < 0:
		return Patch{}, fmt.Errorf("tinydelta: negative CoderTables %d", opts.CoderTables)
	}
	window := MaxSize
	if opts.Window > 0 && opts.Window < MaxSize {
		window = opts.Window
	}
	maxLen := MaxSize
	if opts.MaxInstructionLen > 0 && opts.MaxInstructionLen < MaxSize {
		maxLen = opts.MaxInstructionLen
	}
	plain := func(dict, target []byte) []byte {
		if opts.Greedy {
			return withHeader(encodeGreedyLimited(dict, target, maxLen, window))
		}
		return withHeader(encodeLimited(dict, target, maxLen, window))
	}
	layouts := codedLayoutsWithin(opts.CoderTables)

	best := Patch{Bytes: plain(dict, target), Passes: 1}
	consider := func(patch []byte, passes int) {
		if len(patch) < len(best.Bytes) {
			best = Patch{Bytes: patch, Passes: passes}
		}
	}
	if len(layouts) > 0 {
		for _, candidate := range [][]byte{best.Bytes, withVersion(FormatVersion2, encodeCovers(dict, target, maxLen))} {
			for _, layout := range layouts {
				consider(encodeCoded(candidate, layout), 1)
			}
		}
	}
	if opts.TwoPass {
		// The second pass is tinydelta as a compressor: the first pass's
		// patch encoded against an empty dictionary.
		outer := plain(nil, encodeFirstPass(dict, target, maxLen, window, opts.Greedy))
		consider(outer, 2)
		for _, layout := range layouts {
			consider(encodeCoded(outer, layout), 2)
		}
	}
	return best, nil
}

// EncodeGreedy produces a patch that reconstructs target from dict via
// Decode, quickly: a greedy LZ77 match against both the dictionary
// (COPY/SKIP) and the target already produced (COPY_TARGET), with one-byte
// lazy evaluation (deferring a match that the next position would make
// longer) and a cost gate (skipping a match that would cost more to
// address than to just add literally). Its patches are typically a few
// percent larger than Encode's: always taking the longest match sometimes
// moves the cursor in a way that costs more to undo later. Same size
// limits as Encode.
func EncodeGreedy(dict, target []byte) ([]byte, error) {
	if err := checkSizes(dict, target); err != nil {
		return nil, err
	}
	return withHeader(encodeGreedy(dict, target, MaxSize)), nil
}

// checkSizes enforces MaxSize, which is what guarantees every field the
// encoders emit fits in 32 bits (see the package doc).
func checkSizes(dict, target []byte) error {
	if len(dict) > MaxSize || len(target) > MaxSize {
		return fmt.Errorf("%w: dictionary %d B, target %d B, MaxSize %d B", ErrTooLarge, len(dict), len(target), MaxSize)
	}
	return nil
}

// encode is Encode without the size check or the header (it returns the
// instruction stream only), with no instruction producing
// more than maxLen (at least 1) target bytes.
func encode(dict, target []byte, maxLen int) []byte {
	return encodeLimited(dict, target, maxLen, MaxSize)
}

// encodeLimited is encode with no COPY_TARGET reading more than
// maxDistance bytes back.
func encodeLimited(dict, target []byte, maxLen, maxDistance int) []byte {
	greedy := encodeGreedyLimited(dict, target, maxLen, maxDistance)
	if opt := encodeOptimal(dict, target, greedy, maxLen, maxDistance); opt != nil && len(opt) < len(greedy) {
		return opt
	}
	return greedy
}

// encodeGreedy is EncodeGreedy without the size check or the header, with no
// instruction producing more than maxLen (at least 1) target bytes.
func encodeGreedy(dict, target []byte, maxLen int) []byte {
	return encodeGreedyLimited(dict, target, maxLen, MaxSize)
}

// encodeGreedyLimited is encodeGreedy with no COPY_TARGET reading more
// than maxDistance bytes back.
func encodeGreedyLimited(dict, target []byte, maxLen, maxDistance int) []byte {
	dm := newMatcher(dict)
	sm := newSelfMatcher(target, maxDistance)
	cursor := uint64(0)

	var out []byte
	litStart := -1
	flush := func(end int) {
		for litStart >= 0 && end > litStart {
			k := min(end-litStart, maxLen)
			out = appendVarint(out, uint64(k)<<2|opAdd)
			out = append(out, target[litStart:litStart+k]...)
			litStart += k
		}
		litStart = -1
	}

	p := 0
	for p < len(target) {
		c := bestCandidateAt(dm, sm, dict, target, cursor, p, min(len(target)-p, maxLen))
		if c.length > 0 && p+1 < len(target) {
			// Peek with the cursor as it would be after one literal byte
			// (ADD, like every instruction, advances it) to see whether
			// deferring by one position does better.
			next := bestCandidateAt(dm, sm, dict, target, cursor+1, p+1, min(len(target)-p-1, maxLen))
			if next.length > c.length {
				c.length = 0
			}
		}
		if c.length > 0 {
			flush(p)
			out = appendVarint(out, uint64(c.length)<<2|uint64(c.kind))
			if c.kind != opSkip {
				out = appendVarint(out, zigzagEncode(c.delta))
			}
			if c.kind == opTargetCopy {
				cursor += uint64(c.length) // rides along; COPY_TARGET never resyncs it
			} else {
				cursor = uint64(int64(cursor) + c.delta + int64(c.length))
			}
			for i := 0; i < c.length; i++ {
				sm.insert(p + i)
			}
			p += c.length
		} else {
			if litStart < 0 {
				litStart = p
			}
			cursor++
			sm.insert(p)
			p++
		}
	}
	flush(len(target))
	return out
}

// candidate is one option bestCandidateAt considered: a COPY (delta from
// the dictionary cursor), a SKIP (implicitly at the cursor, so no delta),
// or a COPY_TARGET (delta from the current output position).
type candidate struct {
	kind   byte
	length int
	delta  int64 // unused for opSkip
	cost   int   // total encoded bytes this candidate would need
}

// bestCandidateAt finds the best of up to three matches at output
// position p, with the dictionary cursor at cursor: a COPY/SKIP against
// the dictionary (both the hash-indexed search, which can't detect a
// match shorter than 4 bytes, and a direct check at the cursor's exact
// position, which can), and a COPY_TARGET against the target already
// produced. "Best" means longest; ties prefer whichever costs less to
// encode (which prefers SKIP, since it has no address field at all).
// Only candidates actually cheaper than their own length as literal
// bytes are considered at all; if none are, the result's length is 0.
func bestCandidateAt(dm *matcher, sm *selfMatcher, dict, target []byte, cursor uint64, p, limit int) candidate {
	var best candidate

	consider := func(c candidate) {
		if c.length <= 0 || c.cost >= c.length {
			return
		}
		if c.length > best.length || (c.length == best.length && c.cost < best.cost) {
			best = c
		}
	}

	if addr, length := dm.bestMatch(target, p, limit); length > 0 {
		consider(dictCandidate(cursor, uint64(addr), length))
	}
	if length := directMatchLen(dict, int(cursor), target, p, limit); length > 0 {
		consider(candidate{kind: opSkip, length: length, cost: varintLen(uint64(length)<<2 | opSkip)})
	}
	if addr, length := sm.bestMatch(p, limit); length > 0 {
		delta := int64(addr) - int64(p)
		cost := varintLen(uint64(length)<<2|opTargetCopy) + varintLen(zigzagEncode(delta))
		consider(candidate{kind: opTargetCopy, length: length, delta: delta, cost: cost})
	}
	return best
}

// dictCandidate builds the COPY or SKIP candidate for a dictionary match
// at addr: SKIP when addr is exactly where the cursor already is (no
// address field needed at all), COPY otherwise (a delta from the cursor).
func dictCandidate(cursor, addr uint64, length int) candidate {
	if addr == cursor {
		return candidate{kind: opSkip, length: length, cost: varintLen(uint64(length)<<2 | opSkip)}
	}
	delta := int64(addr) - int64(cursor)
	cost := varintLen(uint64(length)<<2|opCopy) + varintLen(zigzagEncode(delta))
	return candidate{kind: opCopy, length: length, delta: delta, cost: cost}
}

// encodeFirstPass is the first pass of a two-pass patch (see
// EncodeOptions.TwoPass): it makes a version 1 and a version 2 patch and
// returns whichever compresses smaller with DEFLATE, a stand-in for the
// second pass. Regions that line up with the dictionary but have a few
// changed bytes (moved addresses and branch offsets in firmware) become
// differences from the dictionary in version 2, which compress far better
// than the new bytes themselves.
func encodeFirstPass(dict, target []byte, maxLen, maxDistance int, greedy bool) []byte {
	var v1 []byte
	if greedy {
		v1 = withHeader(encodeGreedyLimited(dict, target, maxLen, maxDistance))
	} else {
		v1 = withHeader(encodeLimited(dict, target, maxLen, maxDistance))
	}
	v2 := withVersion(FormatVersion2, encodeCovers(dict, target, maxLen))
	if deflatedLen(v2) < deflatedLen(v1) {
		return v2
	}
	return v1
}

// deflatedLen is the size of b compressed with DEFLATE at its best
// compression level.
func deflatedLen(b []byte) int {
	var n countingWriter
	w, _ := flate.NewWriter(&n, flate.BestCompression)
	w.Write(b)
	w.Close()
	return int(n)
}

// countingWriter counts the bytes written to it and discards them.
type countingWriter int

func (c *countingWriter) Write(p []byte) (int, error) {
	*c += countingWriter(len(p))
	return len(p), nil
}
