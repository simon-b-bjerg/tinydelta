package tinydelta

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"testing"
)

// inst builds a plain (no delta field) instruction: an opcode+len varint.
func inst(op byte, length int) []byte {
	return appendVarint(nil, uint64(length)<<2|uint64(op))
}

// instDelta builds a COPY/COPY_TARGET instruction: an opcode+len varint
// followed by a zigzag-encoded signed delta varint.
func instDelta(op byte, length int, delta int64) []byte {
	return appendVarint(inst(op, length), zigzagEncode(delta))
}

func TestDecodeCopy(t *testing.T) {
	dict := []byte("the quick brown fox")
	patch := instDelta(opCopy, 5, 4) // cursor starts at 0; dict[4:9] = "quick"
	got, err := Decode(dict, withHeader(patch))
	if err != nil || string(got) != "quick" {
		t.Fatalf("Decode = %q, %v; want %q", got, err, "quick")
	}
}

func TestDecodeAdd(t *testing.T) {
	dict := []byte("the quick brown fox")
	patch := append(inst(opAdd, 3), "cat"...)
	got, err := Decode(dict, withHeader(patch))
	if err != nil || string(got) != "cat" {
		t.Fatalf("Decode = %q, %v; want %q", got, err, "cat")
	}
}

func TestDecodeCopyThenAdd(t *testing.T) {
	dict := []byte("the quick brown fox")
	var patch []byte
	patch = append(patch, instDelta(opCopy, 3, 0)...) // dict[0:3] = "the"
	patch = append(patch, inst(opAdd, 1)...)
	patch = append(patch, '!')
	got, err := Decode(dict, withHeader(patch))
	if err != nil || string(got) != "the!" {
		t.Fatalf("Decode = %q, %v; want %q", got, err, "the!")
	}
}

func TestDecodeSkip(t *testing.T) {
	// SKIP has no address field: it reads the dictionary cursor, which
	// starts at 0.
	dict := []byte("the quick brown fox")
	patch := inst(opSkip, 3) // dict[0:3] = "the"
	got, err := Decode(dict, withHeader(patch))
	if err != nil || string(got) != "the" {
		t.Fatalf("Decode = %q, %v; want %q", got, err, "the")
	}
}

func TestDecodeSkipMidStream(t *testing.T) {
	// ADD advances the cursor too, so a SKIP right after must read from
	// dict[4:], not dict[0:].
	dict := []byte("0123456789")
	var patch []byte
	patch = append(patch, inst(opAdd, 4)...)
	patch = append(patch, "ABCD"...)
	patch = append(patch, inst(opSkip, 3)...) // cursor is now 4: dict[4:7] = "456"
	got, err := Decode(dict, withHeader(patch))
	if err != nil || string(got) != "ABCD456" {
		t.Fatalf("Decode = %q, %v; want %q", got, err, "ABCD456")
	}
}

func TestDecodeCopyZeroLengthSetsPositionOnly(t *testing.T) {
	// A length-0 COPY emits nothing but resyncs the cursor, which a
	// following SKIP then uses.
	dict := []byte("abcdefghij")
	var patch []byte
	patch = append(patch, instDelta(opCopy, 0, 5)...) // cursor: 0 -> 5, no output
	patch = append(patch, inst(opSkip, 3)...)         // dict[5:8] = "fgh"
	got, err := Decode(dict, withHeader(patch))
	if err != nil || string(got) != "fgh" {
		t.Fatalf("Decode = %q, %v; want %q", got, err, "fgh")
	}
}

func TestDecodeRejectsMalformedInput(t *testing.T) {
	dict := []byte("the quick brown fox") // 19 bytes

	// A target the length of the dictionary, consumed entirely so the
	// cursor sits at len(dict) for the "SKIP past end of dictionary" case.
	fillToDict := inst(opAdd, len(dict))
	fillToDict = append(fillToDict, dict...)

	cases := []struct {
		name  string
		patch []byte
	}{
		{"truncated instruction varint", []byte{0x80}},
		{"COPY with no delta following", inst(opCopy, 1)},
		{"COPY address out of bounds", instDelta(opCopy, 5, 100)},
		{"COPY length out of bounds", instDelta(opCopy, 1000, 0)},
		{"COPY negative address", instDelta(opCopy, 1, -1)},
		{"ADD needs more bytes than present", append(inst(opAdd, 5), 'a', 'b')},
		{"SKIP exceeds dictionary bounds", inst(opSkip, 1000)},
		{"SKIP past end of dictionary", append(fillToDict, inst(opSkip, 1)...)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Decode(dict, withHeader(c.patch)); err == nil {
				t.Errorf("Decode(%s) succeeded, want an error", c.name)
			}
		})
	}
}

// TestDecodeChecksHeader checks the version byte: a patch with no header
// or another version fails, and a header alone is a valid, empty target.
func TestDecodeChecksHeader(t *testing.T) {
	dict := []byte("abc")
	if _, err := Decode(dict, nil); err == nil || errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("Decode(empty patch): err = %v, want a missing-header error", err)
	}
	for _, version := range []byte{0, FormatVersion + 1, 0xFF} {
		patch := append([]byte{version}, inst(opSkip, 3)...)
		if _, err := Decode(dict, patch); !errors.Is(err, ErrUnsupportedVersion) {
			t.Errorf("Decode(version %d): err = %v, want ErrUnsupportedVersion", version, err)
		}
	}
	if got, err := Decode(dict, []byte{FormatVersion}); err != nil || len(got) != 0 {
		t.Errorf("Decode(header only) = %q, %v; want an empty target", got, err)
	}
}

// TestCopyNeverReadsPastDictIntoTarget specifically checks the "only copy
// dict" rule: a COPY whose address+length would read past the dictionary
// (even if that range would have been valid in a combined dict+target
// address space) is rejected, not silently reinterpreted as reaching into
// the target.
func TestCopyNeverReadsPastDictIntoTarget(t *testing.T) {
	dict := []byte("abc")
	patch := instDelta(opCopy, 3, 1) // dict[1:4]: one byte past a 3-byte dictionary
	if _, err := Decode(dict, withHeader(patch)); err == nil {
		t.Error("COPY reading past the dictionary succeeded, want an error")
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	dict := []byte("the quick brown fox jumps over the lazy dog")
	target := []byte("the slow brown fox jumps over one lazy dog!!")
	patch := encode(dict, target, MaxSize)
	got, err := Decode(dict, withHeader(patch))
	if err != nil || !bytes.Equal(got, target) {
		t.Fatalf("round trip failed: err=%v got=%q", err, got)
	}
}

func TestDecodeTargetCopy(t *testing.T) {
	dict := []byte("unrelated dictionary content, never read by this test")
	var patch []byte
	patch = append(patch, inst(opAdd, 3)...)
	patch = append(patch, "abc"...)
	// Output position is 3 here; addr=0 is delta -3.
	patch = append(patch, instDelta(opTargetCopy, 2, -3)...) // copy target[0:2] = "ab"
	got, err := Decode(dict, withHeader(patch))
	if err != nil || string(got) != "abcab" {
		t.Fatalf("Decode = %q, %v; want %q", got, err, "abcab")
	}
}

// TestDecodeTargetCopyOverlapRun checks the RLE case documented in
// format.go: a COPY_TARGET whose delta is -1 (one byte behind the current
// output position) produces a run of one repeated byte, regardless of how
// long the run is.
func TestDecodeTargetCopyOverlapRun(t *testing.T) {
	dict := []byte("unused")
	var patch []byte
	patch = append(patch, inst(opAdd, 1)...)
	patch = append(patch, 'z')
	patch = append(patch, instDelta(opTargetCopy, 50, -1)...)
	got, err := Decode(dict, withHeader(patch))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := "z" + string(bytes.Repeat([]byte{'z'}, 50))
	if string(got) != want {
		t.Fatalf("Decode produced %d bytes of %q, want %d bytes of 'z'", len(got), got[:min(len(got), 10)], len(want))
	}
}

func TestDecodeTargetCopyRejectsForwardReference(t *testing.T) {
	dict := []byte("unused")
	// A COPY_TARGET with delta=0 at output position 0: nothing has been
	// produced yet, so there is no byte 0 to copy.
	patch := instDelta(opTargetCopy, 1, 0)
	if _, err := Decode(dict, withHeader(patch)); err == nil {
		t.Error("COPY_TARGET at output position 0 with delta=0 succeeded, want an error")
	}
}

// TestDecodeReaderStreams proves DecodeReader works against a genuine
// stream, not just something that happens to support random access: an
// io.Pipe only ever offers bytes as the writer produces them, with no
// seeking and no way to peek ahead past what's been written so far.
func TestDecodeReaderStreams(t *testing.T) {
	dict := []byte("the quick brown fox jumps over the lazy dog")
	target := []byte("the quick brown fox leaps over one lazy dog")
	patch := withHeader(encode(dict, target, MaxSize))

	pr, pw := io.Pipe()
	go func() {
		// Dribble the patch out a few bytes at a time rather than in one
		// write, so DecodeReader can't get away with assuming it'll all
		// arrive in a single Read.
		for i := 0; i < len(patch); i += 3 {
			end := min(i+3, len(patch))
			if _, err := pw.Write(patch[i:end]); err != nil {
				return
			}
		}
		pw.Close()
	}()

	got, err := DecodeReader(dict, pr)
	if err != nil || !bytes.Equal(got, target) {
		t.Fatalf("DecodeReader over a pipe: err=%v got=%q", err, got)
	}
}

// TestReadVarintFromDistinguishesCleanEOFFromTruncation checks the one
// subtlety in readVarintFrom/DecodeReader's EOF handling: a read failure
// on the very first byte of an instruction means "patch is exhausted"
// (reported as io.EOF, and DecodeReader treats it as success), but a read
// failure partway through a varint is a real error that must not be
// mistaken for the same thing via errors.Is.
func TestReadVarintFromDistinguishesCleanEOFFromTruncation(t *testing.T) {
	v, err := readVarintFrom(bufio.NewReader(bytes.NewReader(nil)))
	if v != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("readVarintFrom(empty) = %d, %v; want 0, io.EOF", v, err)
	}

	_, err = readVarintFrom(bufio.NewReader(bytes.NewReader([]byte{0x80})))
	if err == nil {
		t.Fatal("readVarintFrom(truncated mid-varint) succeeded, want an error")
	}
	if errors.Is(err, io.EOF) {
		t.Errorf("readVarintFrom(truncated mid-varint) = %v, incorrectly satisfies errors.Is(_, io.EOF)", err)
	}
}

// TestDecodeHugeAddLengthFailsCleanly checks that an ADD claiming far more
// literal bytes than the patch holds returns an error, rather than trying
// to allocate the claimed length up front and crashing.
func TestDecodeHugeAddLengthFailsCleanly(t *testing.T) {
	patch := append(inst(opAdd, 1<<29), 'a', 'b')
	if _, err := Decode(nil, withHeader(patch)); err == nil {
		t.Error("ADD claiming 2^29 bytes with 2 present succeeded, want an error")
	}
}

// TestDecodeReaderLimit checks the output cap: a target exactly at the
// limit decodes, one byte over fails with ErrTooLarge, and an 8-byte patch
// describing a 512 MiB COPY_TARGET run fails up front instead of trying
// to produce it.
func TestDecodeReaderLimit(t *testing.T) {
	dict := []byte("0123456789")
	patch := inst(opSkip, 10)
	if got, err := DecodeReaderLimit(dict, bytes.NewReader(withHeader(patch)), 10); err != nil || string(got) != "0123456789" {
		t.Fatalf("at the limit: got %q, err %v", got, err)
	}
	if _, err := DecodeReaderLimit(dict, bytes.NewReader(withHeader(patch)), 9); !errors.Is(err, ErrTooLarge) {
		t.Errorf("one byte over the limit: err = %v, want ErrTooLarge", err)
	}

	bomb := append(inst(opAdd, 1), 'z')
	bomb = append(bomb, instDelta(opTargetCopy, 1<<29, -1)...)
	if _, err := DecodeReaderLimit(nil, bytes.NewReader(withHeader(bomb)), 4096); !errors.Is(err, ErrTooLarge) {
		t.Errorf("COPY_TARGET run past the limit: err = %v, want ErrTooLarge", err)
	}
}

// TestEncodeRejectsOversizedInput checks MaxSize on the exported encoders.
// The oversized slice is never written to, so it costs address space, not
// memory.
func TestEncodeRejectsOversizedInput(t *testing.T) {
	huge := make([]byte, MaxSize+1)
	if _, err := Encode(nil, huge); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Encode with an oversized target: err = %v, want ErrTooLarge", err)
	}
	if _, err := EncodeGreedy(huge, nil); !errors.Is(err, ErrTooLarge) {
		t.Errorf("EncodeGreedy with an oversized dictionary: err = %v, want ErrTooLarge", err)
	}
	if _, err := Decode(huge, nil); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Decode with an oversized dictionary: err = %v, want ErrTooLarge", err)
	}
	patch, err := Encode([]byte("abc"), []byte("abcd"))
	if err != nil {
		t.Fatalf("Encode within limits: %v", err)
	}
	if got, err := Decode([]byte("abc"), patch); err != nil || string(got) != "abcd" {
		t.Errorf("round trip via exported API: got %q, err %v", got, err)
	}
}
