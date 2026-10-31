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

// EncodeOptions configures EncodeWith.
type EncodeOptions struct {
	// Greedy selects the fast greedy encoder (see EncodeGreedy) instead
	// of the optimizing one (see Encode).
	Greedy bool

	// ForCompression writes a format version 2 patch (FormatVersion2),
	// made for patches that are compressed afterwards, e.g. with zlib or
	// a small embedded compressor such as heatshrink or tinyuz. Regions
	// that line up with the dictionary but have a few changed bytes
	// (moved addresses and branch offsets in firmware) are written as
	// differences from the dictionary, which compress far better than
	// the new bytes themselves. Uncompressed, such a patch is usually
	// larger than the default one. For small changes a version 1 patch
	// often compresses better, so both are made, each is compressed with
	// DEFLATE as a stand-in for whatever compressor follows, and the
	// smaller one is returned: ForCompression may return a version 1
	// patch. Greedy applies to that version 1 candidate.
	ForCompression bool

	// MaxInstructionLen, if positive, caps the number of target bytes
	// any single instruction may produce: longer ADD, SKIP, COPY and
	// COPY_TARGET runs are split into several instructions. Use it when
	// a decoder handles one instruction at a time and needs a bound on
	// it. 0 means no limit beyond MaxSize. The patch format and the
	// decoders are unchanged.
	MaxInstructionLen int
}

// EncodeWith is Encode or EncodeGreedy with options. With a
// MaxInstructionLen limit, Encode's guarantee becomes: never larger than
// EncodeGreedy's patch under the same limit.
func EncodeWith(dict, target []byte, opts EncodeOptions) ([]byte, error) {
	if err := checkSizes(dict, target); err != nil {
		return nil, err
	}
	if opts.MaxInstructionLen < 0 {
		return nil, fmt.Errorf("tinydelta: negative MaxInstructionLen %d", opts.MaxInstructionLen)
	}
	maxLen := MaxSize
	if opts.MaxInstructionLen > 0 && opts.MaxInstructionLen < MaxSize {
		maxLen = opts.MaxInstructionLen
	}
	if opts.ForCompression {
		return encodeForCompression(dict, target, maxLen, opts.Greedy), nil
	}
	if opts.Greedy {
		return withHeader(encodeGreedy(dict, target, maxLen)), nil
	}
	return withHeader(encode(dict, target, maxLen)), nil
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
	greedy := encodeGreedy(dict, target, maxLen)
	if opt := encodeOptimal(dict, target, greedy, maxLen); opt != nil && len(opt) < len(greedy) {
		return opt
	}
	return greedy
}

// encodeGreedy is EncodeGreedy without the size check or the header, with no
// instruction producing more than maxLen (at least 1) target bytes.
func encodeGreedy(dict, target []byte, maxLen int) []byte {
	dm := newMatcher(dict)
	sm := newSelfMatcher(target)
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

// encodeForCompression makes a version 1 and a version 2 patch and
// returns whichever compresses smaller with DEFLATE (see
// EncodeOptions.ForCompression).
func encodeForCompression(dict, target []byte, maxLen int, greedy bool) []byte {
	var v1 []byte
	if greedy {
		v1 = withHeader(encodeGreedy(dict, target, maxLen))
	} else {
		v1 = withHeader(encode(dict, target, maxLen))
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
