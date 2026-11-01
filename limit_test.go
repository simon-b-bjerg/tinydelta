// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bytes"
	"math/rand"
	"testing"
)

// instructionLens returns the len of every instruction in patch.
func instructionLens(t *testing.T, patch []byte) []int { // patch with header
	t.Helper()
	var lens []int
	for i := headerLen; i < len(patch); {
		v, n := readVarint(patch[i:])
		if n < 0 {
			t.Fatalf("bad varint at %d", i)
		}
		i += n
		op, length := v&3, int(v>>2)
		switch {
		case op == opCopy, op == opTargetCopy && patch[0] == FormatVersion:
			_, n := readVarint(patch[i:])
			i += n
		case op == opAdd, op == opAddDiff && patch[0] == FormatVersion2:
			i += length
		}
		lens = append(lens, length)
	}
	return lens
}

func TestMaxInstructionLen(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	base := textFile(r, fileSize)
	cases := []struct {
		name      string
		dict, tgt []byte
	}{
		{"identical (SKIP)", base, base},
		{"shifted (COPY)", base, splice(base, 10, 0, []byte("xyz"))},
		{"unrelated random (ADD)", nil, randomFile(r, fileSize)},
		{"long run (COPY_TARGET)", nil, bytes.Repeat([]byte{'z'}, fileSize)},
		{"edits", base, splice(splice(base, 900, 7, []byte("EDITED")), 2000, 0, randomFile(r, 300))},
	}
	for _, c := range cases {
		for _, limit := range []int{1, 5, 31, 32, 100, 1024} {
			for _, greedy := range []bool{true, false} {
				patch := mustEncode(t, c.dict, c.tgt, EncodeOptions{Greedy: greedy, MaxInstructionLen: limit})
				if got, err := Decode(c.dict, patch); err != nil || !bytes.Equal(got, c.tgt) {
					t.Fatalf("%s, limit %d, greedy %v: round trip failed (err %v)", c.name, limit, greedy, err)
				}
				for _, l := range instructionLens(t, patch) {
					if l > limit {
						t.Fatalf("%s, limit %d, greedy %v: instruction of %d bytes", c.name, limit, greedy, l)
					}
				}
				if !greedy {
					g := withHeader(encodeGreedy(c.dict, c.tgt, limit))
					if len(patch) > len(g) {
						t.Fatalf("%s, limit %d: optimal patch (%d B) larger than greedy (%d B)", c.name, limit, len(patch), len(g))
					}
				}
			}
		}
	}
}

func TestMaxInstructionLenUnlimited(t *testing.T) {
	r := rand.New(rand.NewSource(8))
	dict := textFile(r, fileSize)
	tgt := splice(dict, 1000, 0, randomFile(r, 50))
	want := withHeader(encode(dict, tgt, MaxSize))
	for _, limit := range []int{0, fileSize, MaxSize, MaxSize + 1} {
		got, err := EncodeWith(dict, tgt, EncodeOptions{MaxInstructionLen: limit})
		if err != nil || !bytes.Equal(got.Bytes, want) {
			t.Fatalf("limit %d: patch differs from Encode's (err %v)", limit, err)
		}
	}
	if _, err := EncodeWith(dict, tgt, EncodeOptions{MaxInstructionLen: -1}); err == nil {
		t.Fatal("negative MaxInstructionLen accepted")
	}
}

// targetDistances returns how far back every COPY_TARGET in a version 1
// patch reads.
func targetDistances(t *testing.T, patch []byte) []int {
	t.Helper()
	var distances []int
	for i := headerLen; i < len(patch); {
		v, n := readVarint(patch[i:])
		if n < 0 {
			t.Fatalf("bad varint at %d", i)
		}
		i += n
		switch op, length := v&3, int(v>>2); op {
		case opCopy, opTargetCopy:
			z, n := readVarint(patch[i:])
			i += n
			if op == opTargetCopy {
				distances = append(distances, int(-zigzagDecode(z)))
			}
		case opAdd:
			i += length
		}
	}
	return distances
}

func TestWindow(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	block := textFile(r, 700)
	// Repeats 700 bytes back: a 512-byte window can't reach them.
	tgt := append(append(append([]byte{}, block...), randomFile(r, 50)...), block...)
	tgt = append(tgt, bytes.Repeat([]byte{'z'}, 300)...)
	unlimited := mustEncode(t, nil, tgt, EncodeOptions{})
	for _, limit := range []int{1, 16, 512, 1024} {
		for _, greedy := range []bool{true, false} {
			patch := mustEncode(t, nil, tgt, EncodeOptions{Greedy: greedy, Window: limit})
			if got, err := Decode(nil, patch); err != nil || !bytes.Equal(got, tgt) {
				t.Fatalf("limit %d, greedy %v: round trip failed (err %v)", limit, greedy, err)
			}
			for _, d := range targetDistances(t, patch) {
				if d > limit {
					t.Fatalf("limit %d, greedy %v: COPY_TARGET %d bytes back", limit, greedy, d)
				}
			}
			if limit < 700 && len(patch) <= len(unlimited) {
				t.Errorf("limit %d, greedy %v: %d B, not larger than unlimited %d B", limit, greedy, len(patch), len(unlimited))
			}
			if !greedy {
				g := mustEncode(t, nil, tgt, EncodeOptions{Greedy: true, Window: limit})
				if len(patch) > len(g) {
					t.Fatalf("limit %d: optimal patch (%d B) larger than greedy (%d B)", limit, len(patch), len(g))
				}
			}
		}
	}
	if _, err := EncodeWith(nil, tgt, EncodeOptions{Window: -1}); err == nil {
		t.Fatal("negative Window accepted")
	}
}
