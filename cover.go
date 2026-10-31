// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

// Cover encoder: the encoder for format version 2, which is made for
// patches that are compressed afterwards (see EncodeOptions.ForCompression).
//
// Version 1's encoders break a match at the first byte that differs and
// then look for an exact match elsewhere. When code moves, though, most
// of a region still lines up with the old file, with a changed byte here
// and there (a branch offset, an address in a literal pool). Version 2
// describes such a region as one alignment, a cover, and the changed
// bytes in it as differences from the old bytes. The same shift then gives
// the same difference byte wherever it occurs, which a compressor turns
// into very little.
//
// Covers are found the way bsdiff finds them: scan the target for exact
// matches, and only switch to a new alignment when it explains clearly
// more bytes than the current one; then extend each cover forwards and
// backwards as long as at least half of its bytes match.

// coverSwitchMargin is how many more bytes a new alignment must match
// than the current one before the encoder switches to it. 8, as in
// bsdiff, measured best on firmware (4 and 16 are within 1%).
const coverSwitchMargin = 8

// cover is one aligned region: target[newPos:newPos+length] against
// dict[oldPos:oldPos+length], preceded by extra literal bytes
// target[newPos-extra:newPos].
type cover struct {
	extra          int
	newPos, oldPos int
	length         int
}

// findCovers splits target into covers and the literal bytes between
// them, in order.
func findCovers(dict, target []byte) []cover {
	dm := newMatcher(dict)
	var covers []cover
	newSize, oldSize := len(target), len(dict)

	scan, length, pos := 0, 0, 0
	lastScan, lastPos, lastOffset := 0, 0, 0
	for scan < newSize {
		oldScore := 0
		scsc := scan + length
		for scan += length; scan < newSize; scan++ {
			pos, length = dm.bestMatch(target, scan, newSize-scan)
			for ; scsc < scan+length; scsc++ {
				if o := scsc + lastOffset; o >= 0 && o < oldSize && dict[o] == target[scsc] {
					oldScore++
				}
			}
			if (length == oldScore && length != 0) || length > oldScore+coverSwitchMargin {
				break
			}
			if o := scan + lastOffset; o >= 0 && o < oldSize && dict[o] == target[scan] {
				oldScore--
			}
		}
		if length == oldScore && scan != newSize {
			continue
		}

		// Extend the previous alignment forwards from lastScan...
		lenF := 0
		for s, best, i := 0, 0, 0; lastScan+i < scan && lastPos+i < oldSize; {
			if dict[lastPos+i] == target[lastScan+i] {
				s++
			}
			i++
			if s*2-i > best*2-lenF {
				best, lenF = s, i
			}
		}
		// ...and the new one backwards from scan.
		lenB := 0
		if scan < newSize {
			for s, best, i := 0, 0, 1; scan >= lastScan+i && pos >= i; i++ {
				if dict[pos-i] == target[scan-i] {
					s++
				}
				if s*2-i > best*2-lenB {
					best, lenB = s, i
				}
			}
		}
		// Where they overlap, split at the point that keeps most matches.
		if lastScan+lenF > scan-lenB {
			overlap := (lastScan + lenF) - (scan - lenB)
			s, best, split := 0, 0, 0
			for i := 0; i < overlap; i++ {
				if target[lastScan+lenF-overlap+i] == dict[lastPos+lenF-overlap+i] {
					s++
				}
				if target[scan-lenB+i] == dict[pos-lenB+i] {
					s--
				}
				if s > best {
					best, split = s, i+1
				}
			}
			lenF += split - overlap
			lenB -= split
		}

		if lenF > 0 {
			covers = append(covers, cover{newPos: lastScan, oldPos: lastPos, length: lenF})
		}
		lastScan += lenF
		if gap := (scan - lenB) - lastScan; gap > 0 {
			// Literal bytes before the next cover.
			covers = append(covers, cover{extra: gap, newPos: scan - lenB, oldPos: pos - lenB})
		}
		lastScan, lastPos, lastOffset = scan-lenB, pos-lenB, pos-scan
	}
	return covers
}

// encodeCovers is the version-2 encoder: the instruction stream (without
// header) for target against dict, no instruction producing more than
// maxLen bytes.
func encodeCovers(dict, target []byte, maxLen int) []byte {
	var out []byte
	cursor := 0
	emit := func(op byte, n int) { out = appendVarint(out, uint64(n)<<2|uint64(op)) }
	literal := func(from, n int) {
		for n > 0 {
			k := min(n, maxLen)
			emit(opAdd, k)
			out = append(out, target[from:from+k]...)
			from, n, cursor = from+k, n-k, cursor+k
		}
	}
	diff := func(from, n int) {
		for n > 0 {
			k := min(n, maxLen)
			emit(opAddDiff, k)
			for i := 0; i < k; i++ {
				out = append(out, target[from+i]-dict[cursor+i])
			}
			from, n, cursor = from+k, n-k, cursor+k
		}
	}
	skip := func(n int) {
		for n > 0 {
			k := min(n, maxLen)
			emit(opSkip, k)
			n, cursor = n-k, cursor+k
		}
	}

	pending := 0 // literal bytes not yet emitted, ending where the next cover starts
	for _, c := range findCovers(dict, target) {
		if c.extra > 0 {
			pending += c.extra
			continue
		}
		literal(c.newPos-pending, pending)
		pending = 0
		if c.oldPos != cursor {
			// Realign: a COPY covering the cover's leading run of
			// unchanged bytes (possibly none) sets the cursor.
			n := min(matchLen(dict, c.oldPos, target, c.newPos, c.length), maxLen)
			emit(opCopy, n)
			out = appendVarint(out, zigzagEncode(int64(c.oldPos)-int64(cursor)))
			cursor = c.oldPos + n
			c.newPos, c.oldPos, c.length = c.newPos+n, c.oldPos+n, c.length-n
		}
		// Inside the cover: unchanged runs as SKIP, changed bytes as
		// ADD_DIFF. (Folding short unchanged runs into the ADD_DIFF as
		// zeros measured worse after compression: SKIP lengths compress
		// better than runs of zeros.)
		for p, end := c.newPos, c.newPos+c.length; p < end; {
			if run := matchLen(dict, cursor, target, p, end-p); run > 0 {
				skip(run)
				p += run
				continue
			}
			changed := 1
			for p+changed < end && dict[cursor+changed] != target[p+changed] {
				changed++
			}
			diff(p, changed)
			p += changed
		}
	}
	literal(len(target)-pending, pending)
	return out
}
