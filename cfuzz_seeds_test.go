// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteCFuzzSeeds writes a seed corpus for the C fuzz harness
// (c/fuzz/fuzz_decode.c) into $TINYDELTA_C_FUZZ_SEEDS, and is skipped when
// that is unset. scripts/c-fuzz.sh runs it. The seeds are real patches from
// both encoders, so the fuzzer starts from inputs that decode and mutates
// them into ones that almost do, instead of starting from noise that the
// decoder rejects at the first byte.
func TestWriteCFuzzSeeds(t *testing.T) {
	dir := os.Getenv("TINYDELTA_C_FUZZ_SEEDS")
	if dir == "" {
		t.Skip("TINYDELTA_C_FUZZ_SEEDS not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	const (
		maxDict   = 255  // the harness gives the dictionary length one byte
		maxTarget = 4096 // the harness's FUZZ_MAX_TARGET
	)
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 300; i++ {
		var dict []byte
		if i%3 == 0 {
			dict = textFile(r, r.Intn(maxDict+1))
		} else {
			dict = randomFile(r, r.Intn(maxDict+1))
		}
		script := randomFile(r, 4*r.Intn(8))
		target := applyEdits(dict, script)
		if r.Intn(4) == 0 {
			target = append(target, make([]byte, r.Intn(300))...) // long run: COPY_TARGET
		}
		if len(target) > maxTarget {
			target = target[:maxTarget]
		}

		opts := EncodeOptions{Greedy: i%2 == 1}
		if r.Intn(3) == 0 {
			opts.MaxInstructionLen = 1 + r.Intn(40)
		}
		encoded, err := EncodeWith(dict, target, opts)
		if err != nil {
			t.Fatal(err)
		}
		patch := encoded.Bytes
		if i%3 == 2 { // format version 2, from the cover encoder
			limit := MaxSize
			if opts.MaxInstructionLen > 0 {
				limit = opts.MaxInstructionLen
			}
			patch = withVersion(FormatVersion2, encodeCovers(dict, target, limit))
		}
		if i%7 >= 4 { // coded, in one of the three table layouts
			patch = encodeCoded(patch, codedLayoutC+i%3)
		}

		// Header: output capacity (little-endian), dictionary length, page
		// size byte, callback call to fail (0: none); see the input layout
		// in fuzz_decode.c.
		capacity := maxTarget
		if i%5 == 0 {
			capacity = len(target) // exactly enough
		}
		failAt := 0
		if i%4 == 3 {
			failAt = 1 + r.Intn(60)
		}
		seed := []byte{byte(capacity), byte(capacity >> 8), byte(len(dict)), byte(r.Intn(256)), byte(failAt)}
		seed = append(append(seed, dict...), patch...)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("seed%03d", i)), seed, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Relocated coded patches (the harness is built with the MSP430X table
	// and accepts shipped tables): firmware-like images whose code moved,
	// with the built-in table named by CRC, the MSP430X or Thumb table
	// shipped, and the Thumb table named by CRC (which the harness's
	// decoders must refuse alike).
	for i := 0; i < 120; i++ {
		rules := builtinMSP430X
		if i%4 == 3 {
			rules = builtinThumb
		}
		base := uint32(0x10000)
		dict, target := relocSeedImages(r, rules, base, 32+r.Intn(maxDict-31))
		m := relocMapFrom(dict, target, rules, base).withDataRuns(dict, target).withOutside(dict, target)
		m.ship = i%2 == 1
		rd := m.relocate(dict)
		plain := withHeader(encode(rd, target, MaxSize))
		if i%3 == 1 {
			plain = withVersion(FormatVersion2, encodeCovers(rd, target, MaxSize))
		}
		patch := encodeCodedReloc(plain, codedLayoutC+i%3, m)
		failAt := 0
		if i%5 == 4 {
			failAt = 1 + r.Intn(80)
		}
		capacity := maxTarget
		seed := []byte{byte(capacity), byte(capacity >> 8), byte(len(dict)), byte(r.Intn(256)), byte(failAt)}
		seed = append(append(seed, dict...), patch...)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("reloc%03d", i)), seed, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// relocSeedImages makes a small old image of code (randomCode) and a new
// one where code moved: a few bytes inserted and deleted, so its addresses
// change.
func relocSeedImages(r *rand.Rand, rules *Rules, base uint32, n int) (old, new []byte) {
	old = randomCode(r, rules, base, n&^1)
	at := (r.Intn(len(old)/2) + 1) &^ 1
	new = append(append(append([]byte{}, old[:at]...), randomFile(r, 2*(1+r.Intn(4)))...), old[at:]...)
	if cut := (r.Intn(len(new)/2) + len(new)/2) &^ 1; cut+4 < len(new) {
		new = append(new[:cut:cut], new[cut+2:]...)
	}
	return old, new
}

// TestWriteCFuzzRelocSeeds writes a seed corpus for the relocation engine's
// fuzz harness (c/fuzz/fuzz_reloc.c) into $TINYDELTA_C_FUZZ_RELOC_SEEDS,
// and is skipped when that is unset: the real rule tables, a few corrupted
// ones, firmware-like images and maps.
func TestWriteCFuzzRelocSeeds(t *testing.T) {
	dir := os.Getenv("TINYDELTA_C_FUZZ_RELOC_SEEDS")
	if dir == "" {
		t.Skip("TINYDELTA_C_FUZZ_RELOC_SEEDS not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewSource(2))
	le32 := func(b []byte, v uint32) []byte { return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24)) }
	for i := 0; i < 200; i++ {
		rules := builtinMSP430X
		if i%3 == 2 {
			rules = builtinThumb
		}
		base := uint32(0x10000) + uint32(r.Intn(3))
		image := randomCode(r, rules, base, 16+r.Intn(240))
		m := randomMap(r, rules, base, len(image))
		entries, runs, ranges := min(len(m.entries), 7), min(len(m.data), 3), min(len(m.outside), 7)
		table := rules.Table()
		if i%10 == 9 { // a corrupted table: the checker's side
			table[r.Intn(len(table))] ^= byte(1 + r.Intn(255))
		}
		seed := []byte{byte(len(table)), byte(len(table) >> 8), byte(len(image))}
		seed = le32(seed, base)
		seed = append(seed, byte(entries|runs<<3|ranges<<5), byte(r.Intn(256)))
		// The records as the header gives them: gaps from the previous
		// record's end, zigzag shifts.
		end := uint32(0)
		for _, e := range m.entries[:entries] {
			seed = le32(le32(le32(seed, e.start-end), e.length), uint32(zigzagEncode(int64(e.shift))))
			end = e.start + e.length
		}
		end = 0
		for _, d := range m.data[:runs] {
			seed = le32(le32(seed, d.start-end), d.length)
			end = d.start + d.length
		}
		end = 0
		for _, e := range m.outside[:ranges] {
			seed = le32(le32(le32(seed, e.start-end), e.length), uint32(zigzagEncode(int64(e.shift))))
			end = e.start + e.length
		}
		seed = append(append(seed, table...), image...)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("seed%03d", i)), seed, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
