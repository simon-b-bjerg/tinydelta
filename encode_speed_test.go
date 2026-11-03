// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bytes"
	"math/rand"
	"testing"
	"time"
)

// TestEncodeLongRuns checks that long runs don't make the optimal encoder
// quadratic. Padding is the usual case: images filled up to a flash size
// with 0xFF. The limit is far above the seconds this takes, and far below
// the minutes it took when every position compared the whole run.
func TestEncodeLongRuns(t *testing.T) {
	const limit = 20 * time.Second
	r := rand.New(rand.NewSource(70))
	code := make([]byte, 60_000)
	r.Read(code)
	block := make([]byte, 1024)
	r.Read(block)

	changed := append([]byte{}, code...)
	changed[100] ^= 1
	padding := bytes.Repeat([]byte{0xFF}, 200_000)
	blocks := bytes.Repeat(block, 25)
	changedBlocks := append([]byte{}, blocks...)
	changedBlocks[12_000] ^= 1

	cases := []struct {
		name         string
		dict, target []byte
	}{
		{"padding at the end", append(append([]byte{}, code...), padding...), append(append([]byte{}, changed...), padding...)},
		{"padding in the middle", append(append([]byte{}, padding...), code...), append(append([]byte{}, padding...), changed...)},
		{"padding only in the target", code, append(append([]byte{}, changed...), padding...)},
		{"repeated block", blocks, changedBlocks},
		{"zeros", make([]byte, 200_000), make([]byte, 200_000)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start := time.Now()
			patch, err := EncodeWith(c.dict, c.target, EncodeOptions{TwoPass: true, CoderTables: 777})
			if err != nil {
				t.Fatal(err)
			}
			if elapsed := time.Since(start); elapsed > limit {
				t.Errorf("took %v, limit %v", elapsed, limit)
			}
			out := patch.Bytes
			if patch.Passes == 2 {
				if out, err = Decode(nil, out); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Decode(c.dict, out)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, c.target) {
				t.Error("patch does not decode to the target")
			}
		})
	}
}
