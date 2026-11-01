// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bytes"
	"errors"
	"math/rand"
	"testing"
)

// firmwareLike makes a dictionary and a target where code moved: the
// target has an insertion, and every 4-byte "address" after it shifted.
func firmwareLike(seed int64, size int) (dict, target []byte) {
	r := rand.New(rand.NewSource(seed))
	dict = make([]byte, size)
	r.Read(dict)
	for i := 0; i+4 <= size; i += 16 { // literal pool words
		v := uint32(0x2000 + r.Intn(size))
		dict[i], dict[i+1], dict[i+2], dict[i+3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
	}
	at := size / 3
	target = append(append(append([]byte{}, dict[:at]...), []byte("inserted function body")...), dict[at:]...)
	for i := at + 22; i+4 <= len(target); i += 16 {
		v := uint32(target[i]) | uint32(target[i+1])<<8 | uint32(target[i+2])<<16 | uint32(target[i+3])<<24
		v += 22
		target[i], target[i+1], target[i+2], target[i+3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
	}
	return dict, target
}

func TestCodedRoundTripAllLayouts(t *testing.T) {
	cases := map[string][2][]byte{}
	d, g := firmwareLike(1, 20000)
	cases["firmware-like"] = [2][]byte{d, g}
	cases["config edit"] = [2][]byte{[]byte("rate=10\nmode=fast\nname=meter\n"), []byte("rate=12\nmode=fast\nname=meter-2\n")}
	cases["empty target"] = [2][]byte{[]byte("abc"), nil}
	cases["empty dictionary"] = [2][]byte{nil, bytes.Repeat([]byte("abcab"), 300)}
	for name, c := range cases {
		dict, target := c[0], c[1]
		v1, _ := Encode(dict, target)
		v2 := withVersion(FormatVersion2, encodeCovers(dict, target, MaxSize))
		for _, plain := range [][]byte{v1, v2} {
			for _, layout := range []int{codedLayoutA, codedLayoutB, codedLayoutC} {
				coded := encodeCoded(plain, layout)
				if coded[0] != codedFlag|plain[0] || int(coded[1]) != layout {
					t.Fatalf("%s: header %x", name, coded[:2])
				}
				back, err := decodeCoded(coded, MaxSize)
				if err != nil || !bytes.Equal(back, plain) {
					t.Fatalf("%s v%d layout %d: uncoded stream differs: %v", name, plain[0], layout, err)
				}
				got, err := Decode(dict, coded)
				if err != nil || !bytes.Equal(got, target) {
					t.Fatalf("%s v%d layout %d: Decode: %v", name, plain[0], layout, err)
				}
			}
		}
	}
}

func TestEncodeWithDecoderRAM(t *testing.T) {
	dict, target := firmwareLike(2, 60000)
	plain, _ := Encode(dict, target)
	prev := len(plain) + 1
	for _, ram := range []int{0, 521, 522, 777, 1287, 4096} {
		patch, err := EncodeWith(dict, target, EncodeOptions{DecoderRAM: ram})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(dict, patch)
		if err != nil || !bytes.Equal(got, target) {
			t.Fatalf("ram %d: round trip: %v", ram, err)
		}
		coded := patch[0]&codedFlag != 0
		if ram < 522 && coded {
			t.Errorf("ram %d: coded patch, but no layout fits", ram)
		}
		if coded && CodedTablesSize(int(patch[1])) > ram {
			t.Errorf("ram %d: layout %d needs %d bytes", ram, patch[1], CodedTablesSize(int(patch[1])))
		}
		if len(patch) > prev {
			t.Errorf("ram %d: %d bytes, more than with less RAM (%d)", ram, len(patch), prev)
		}
		prev = len(patch)
		t.Logf("DecoderRAM %4d: %6d bytes (plain %d)", ram, len(patch), len(plain))
	}
	if prev*3 > len(plain)*2 {
		t.Errorf("coded patch %d bytes, expected well below plain %d", prev, len(plain))
	}
}

func TestEncodeWithDecoderRAMKeepsSmallPatchesPlain(t *testing.T) {
	dict, target := []byte("rate=10\nmode=fast\n"), []byte("rate=12\nmode=fast\n")
	patch, err := EncodeWith(dict, target, EncodeOptions{DecoderRAM: 1287})
	if err != nil {
		t.Fatal(err)
	}
	if patch[0] != FormatVersion {
		t.Errorf("tiny update coded (%x), plain is smaller", patch)
	}
}

func TestEncodeWithDecoderRAMOptions(t *testing.T) {
	if _, err := EncodeWith(nil, []byte("x"), EncodeOptions{DecoderRAM: -1}); err == nil {
		t.Error("negative DecoderRAM accepted")
	}
	if _, err := EncodeWith(nil, []byte("x"), EncodeOptions{DecoderRAM: 1287, ForCompression: true}); err == nil {
		t.Error("DecoderRAM with ForCompression accepted")
	}
}

func TestDecodeCodedRejectsBadInput(t *testing.T) {
	dict, target := firmwareLike(3, 5000)
	plain, _ := Encode(dict, target)
	coded := encodeCoded(plain, codedLayoutB)
	bad := map[string][]byte{
		"no features":      coded[:1],
		"layout 0":         append([]byte{coded[0], 0}, coded[2:]...),
		"reserved feature": append([]byte{coded[0], coded[1] | 0x10}, coded[2:]...),
		"truncated":        coded[:len(coded)/2],
		"no coded bytes":   coded[:2+varintLen(uint64(len(plain)-1))],
		"version 3 coded":  append([]byte{codedFlag | 3}, coded[1:]...),
	}
	for name, p := range bad {
		if _, err := Decode(dict, p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Decode(dict, append([]byte{coded[0], coded[1] | 0x10}, coded[2:]...)); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("reserved feature bit: %v, want ErrUnsupportedVersion", err)
	}
}

func TestCodedTablesSize(t *testing.T) {
	for layout, want := range map[int]int{codedLayoutA: 1287, codedLayoutB: 777, codedLayoutC: 522, 0: 0, 4: 0} {
		if got := CodedTablesSize(layout); got != want {
			t.Errorf("layout %d: %d, want %d", layout, got, want)
		}
	}
}
