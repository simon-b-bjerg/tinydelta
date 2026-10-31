package tinydelta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/rand"
	"testing"
)

// shiftedFirmware fakes what a code change does to a firmware image:
// after an insertion, 12 bytes of code and a 32-bit address, over and
// over, with every address moved by the insertion's length.
func shiftedFirmware(r *rand.Rand) (dict, target []byte) {
	const entries, insert = 2000, 36
	dict = make([]byte, 0, entries*16)
	target = make([]byte, 0, entries*16+insert)
	target = append(target, randomFile(r, insert)...)
	for i := 0; i < entries; i++ {
		code := randomFile(r, 12)
		addr := 0x08001000 + uint32(r.Intn(1<<16))*4
		dict = append(append(dict, code...), binary.LittleEndian.AppendUint32(nil, addr)...)
		target = append(append(target, code...), binary.LittleEndian.AppendUint32(nil, addr+insert)...)
	}
	return dict, target
}

func TestDecodeAddDiff(t *testing.T) {
	dict := []byte{10, 20, 30, 40, 250}
	var instructions []byte
	instructions = appendVarint(instructions, 1<<2|opSkip)    // 10
	instructions = appendVarint(instructions, 4<<2|opAddDiff) // 20+1, 30-1, 40+0, 250+10 (wraps)
	instructions = append(instructions, 1, 0xFF, 0, 10)
	got, err := Decode(dict, withVersion(FormatVersion2, instructions))
	if want := []byte{10, 21, 29, 40, 4}; err != nil || !bytes.Equal(got, want) {
		t.Fatalf("Decode = %v, %v; want %v", got, err, want)
	}

	// Opcode 3 is still COPY_TARGET in version 1.
	if _, err := Decode(dict, withHeader(instructions)); err == nil {
		t.Error("version 1 decoded opcode 3 as ADD_DIFF")
	}
	// Past the end of the dictionary, or short of difference bytes.
	for _, bad := range [][]byte{
		append(appendVarint(nil, 6<<2|opAddDiff), 0, 0, 0, 0, 0, 0),
		append(appendVarint(nil, 3<<2|opAddDiff), 0, 0),
	} {
		if _, err := Decode(dict, withVersion(FormatVersion2, bad)); err == nil || errors.Is(err, ErrTooLarge) {
			t.Errorf("Decode(% x): err = %v, want malformed", bad, err)
		}
	}
}

func TestEncodeForCompression(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	dict, target := shiftedFirmware(r)
	text := textFile(r, fileSize)
	cases := []struct {
		name         string
		dict, target []byte
		version      byte
	}{
		{"shifted addresses", dict, target, FormatVersion2},
		{"one small edit", text, splice(text, 900, 3, []byte("xyz")), FormatVersion},
		{"identical", text, text, 0},
		{"empty dict", nil, text, 0},
		{"empty target", text, nil, 0},
	}
	for _, c := range cases {
		patch := mustEncode(t, c.dict, c.target, EncodeOptions{ForCompression: true})
		if got, err := Decode(c.dict, patch); err != nil || !bytes.Equal(got, c.target) {
			t.Fatalf("%s: round trip failed: %v", c.name, err)
		}
		if c.version != 0 && patch[0] != c.version {
			t.Errorf("%s: version %d, want %d", c.name, patch[0], c.version)
		}
		v2 := withVersion(FormatVersion2, encodeCovers(c.dict, c.target, MaxSize))
		if got, err := Decode(c.dict, v2); err != nil || !bytes.Equal(got, c.target) {
			t.Fatalf("%s: cover encoder round trip failed: %v", c.name, err)
		}
	}

	// The point of version 2: moved addresses compress far better.
	v1 := mustEncode(t, dict, target, EncodeOptions{})
	v2 := mustEncode(t, dict, target, EncodeOptions{ForCompression: true})
	if a, b := deflatedLen(v1), deflatedLen(v2); b*2 > a {
		t.Errorf("shifted addresses: version 2 deflated to %d B, version 1 to %d B; want at most half", b, a)
	}
}
