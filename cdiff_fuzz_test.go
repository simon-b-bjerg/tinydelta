// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bytes"
	"testing"

	"github.com/simon-b-bjerg/tinydelta/internal/cdiff"
)

// FuzzRelocGoVsC is differential fuzzing of the decoders on relocated
// patches: the Go decoder and the C memory and callback decoders, linked in
// through cgo (internal/cdiff: coder, MSP430X rules built in, shipped tables
// accepted), must accept and reject exactly the same patches, with the same
// output. Each input is a relocated update as FuzzRelocRoundTrip makes them
// (relocFuzzCase, forcedRelocPatch), then maybe damaged (bytes flipped, cut
// short) or decoded with one byte too little room. A patch naming the Thumb
// table built in is refused by both: this decoder has MSP430X built in.
// Skipped without cgo.
//
// flags: as relocFuzzOptions. io: bit 0 one byte too little room, bits 1-2
// the callback decoder's buffer (16 << 2n), bit 3 its page a quarter of it.
// damage: pairs of (position, xor); a first byte with bit 7 set cuts the
// patch at damage[1]/256 of its length instead.
func FuzzRelocGoVsC(f *testing.F) {
	if !cdiff.Available {
		f.Skip("the C decoders need cgo")
	}
	relocFuzzSeeds(func(img, script []byte, flags byte, at uint16, gap byte) {
		f.Add(img, script, flags, at, gap, byte(0), []byte{})
		f.Add(img, script, flags, at, gap, byte(6), []byte{7, 0x40, 30, 0x01})
		f.Add(img, script, flags, at, gap, byte(9), []byte{0x80, 120})
	})
	f.Fuzz(func(t *testing.T, img, script []byte, flags byte, at uint16, gap byte, io byte, damage []byte) {
		if len(img) > fuzzMaxSize {
			img = img[:fuzzMaxSize]
		}
		const base = 0x10000
		rules, _, ship, layout, dataRuns, moveRAM := relocFuzzOptions(flags)
		target := relocFuzzCase(img, script, rules, base, int(at), int(gap), moveRAM)
		patch := forcedRelocPatch(img, target, rules, base, ship, layout, dataRuns)
		if patch == nil {
			return
		}
		if len(damage) >= 2 && damage[0]&0x80 != 0 {
			patch = patch[:len(patch)*int(damage[1])/256]
		} else {
			for i := 0; i+1 < len(damage); i += 2 {
				patch[int(damage[i])%len(patch)] ^= damage[i+1]
			}
		}
		capacity := len(target)
		if io&1 != 0 && capacity > 1 {
			capacity--
		}
		// Room for any map a damaged patch can describe: each record takes
		// at least 3 bytes of the patch and at most 12 of the tables buffer.
		tablesSize := CodedTablesSize(codedLayoutA) + 4096 + 4*len(patch)
		buffer := 16 << (2 * (io >> 1 & 3))
		cfg := cdiff.IOConfig{BufferSize: buffer, PageSize: buffer >> (2 * (io >> 3 & 1))}

		want, goErr := DecodeRules(img, bytes.NewReader(patch), max(capacity, 1), builtinMSP430X)
		mem, memRC := cdiff.Decode(img, patch, max(capacity, 1), tablesSize)
		cb, cbRC, violation := cdiff.DecodeIO(img, patch, max(capacity, 1), tablesSize, cfg)
		if violation != "" {
			t.Fatalf("callback decoder (%+v): %s", cfg, violation)
		}
		// The Go decoder knows every built-in table; this C build only
		// MSP430X's, so it refuses a patch naming another by CRC.
		if len(patch) >= 4 && (patch[0] == 0x81 || patch[0] == 0x82) && patch[1]&0xF0 == 0 && patch[1]&3 != 0 && patch[1]>>2&3 == 1 &&
			uint16(patch[2])|uint16(patch[3])<<8 != builtinMSP430X.crc {
			if memRC == cdiff.OK || cbRC == cdiff.OK {
				t.Fatalf("a patch naming a table the C build doesn't have was accepted: memory %d, callback %d", memRC, cbRC)
			}
			return
		}
		if (goErr == nil) != (memRC == cdiff.OK) || (goErr == nil && !bytes.Equal(want, mem)) {
			t.Fatalf("memory decoder: result %d, Go: %v (outputs equal: %v)", memRC, goErr, bytes.Equal(want, mem))
		}
		if (goErr == nil) != (cbRC == cdiff.OK) || (goErr == nil && !bytes.Equal(want, cb)) {
			t.Fatalf("callback decoder (%+v): result %d, Go: %v (outputs equal: %v)", cfg, cbRC, goErr, bytes.Equal(want, cb))
		}
		intact := len(damage) < 2 && capacity == len(target)
		if intact && (goErr != nil || !bytes.Equal(want, target)) {
			t.Fatalf("an intact patch didn't decode to its target: %v", goErr)
		}
	})
}
