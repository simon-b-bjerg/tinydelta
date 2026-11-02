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
}
