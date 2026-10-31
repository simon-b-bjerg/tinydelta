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
		switch op {
		case opCopy, opTargetCopy:
			_, n := readVarint(patch[i:])
			i += n
		case opAdd:
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
				patch, err := EncodeWith(c.dict, c.tgt, EncodeOptions{Greedy: greedy, MaxInstructionLen: limit})
				if err != nil {
					t.Fatal(err)
				}
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
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("limit %d: patch differs from Encode's (err %v)", limit, err)
		}
	}
	if _, err := EncodeWith(dict, tgt, EncodeOptions{MaxInstructionLen: -1}); err == nil {
		t.Fatal("negative MaxInstructionLen accepted")
	}
}
