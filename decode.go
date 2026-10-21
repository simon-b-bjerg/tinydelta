package tinydelta

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
)

// ErrTooLarge is returned when a dictionary or target exceeds MaxSize, or
// a decoded target would exceed the caller's output limit.
var ErrTooLarge = errors.New("tinydelta: size limit exceeded")

// Decode applies patch against dict and returns the reconstructed target.
// patch has no framing of its own (see the package doc); decoding runs
// until patch is exhausted.
func Decode(dict, patch []byte) ([]byte, error) {
	return DecodeReaderLimit(dict, bytes.NewReader(patch), MaxSize)
}

// DecodeReader is Decode for a patch available only as a stream: it reads
// exactly as many bytes as the patch needs and no more, so it composes
// directly with another stream decoder instead of requiring the whole
// patch to be buffered first (e.g. decoding straight off a network or
// flash read, a chunk at a time). Malformed input returns an error
// rather than panicking.
func DecodeReader(dict []byte, r io.Reader) ([]byte, error) {
	return DecodeReaderLimit(dict, r, MaxSize)
}

// DecodeReaderLimit is DecodeReader with a cap on the target's size: it
// fails with ErrTooLarge as soon as an instruction would take the target
// past maxOutput bytes, before producing any of that instruction's
// output. A short, valid patch can legitimately describe a huge target
// (a long COPY_TARGET run costs a few bytes), so a decoder that knows how
// big the result should be — say, the config buffer it's writing into —
// should say so rather than trust the patch. maxOutput <= 0 or above
// MaxSize means MaxSize.
func DecodeReaderLimit(dict []byte, r io.Reader, maxOutput int) (target []byte, err error) {
	if len(dict) > MaxSize {
		return nil, fmt.Errorf("%w: dictionary is %d bytes, MaxSize is %d", ErrTooLarge, len(dict), MaxSize)
	}
	if maxOutput <= 0 || maxOutput > MaxSize {
		maxOutput = MaxSize
	}
	defer func() {
		if rec := recover(); rec != nil {
			target, err = nil, fmt.Errorf("tinydelta: malformed patch: %v", rec)
		}
	}()

	br := bufio.NewReader(r)
	cursor := uint64(0) // dictionary cursor; see format.go
	for {
		v, err := readVarintFrom(br)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break // clean end of patch
			}
			return nil, fmt.Errorf("tinydelta: %w", err)
		}
		op := byte(v & 3)
		length := v >> 2
		if length > uint64(maxOutput-len(target)) {
			return nil, fmt.Errorf("%w: instruction would take the target past %d bytes", ErrTooLarge, maxOutput)
		}

		switch op {
		case opCopy:
			delta, err := readZigzagFrom(br)
			if err != nil {
				return nil, fmt.Errorf("tinydelta: COPY delta: %w", err)
			}
			addr := int64(cursor) + delta
			if addr < 0 || addr > int64(len(dict)) || length > uint64(len(dict))-uint64(addr) {
				return nil, fmt.Errorf("tinydelta: COPY [%d,%d) exceeds dictionary length %d", addr, addr+int64(length), len(dict))
			}
			// length == 0 is deliberate: no bytes copied, cursor still
			// resynced to addr -- an explicit "set position".
			target = append(target, dict[addr:uint64(addr)+length]...)
			cursor = uint64(addr) + length

		case opAdd:
			if target, err = appendFrom(target, br, length); err != nil {
				return nil, fmt.Errorf("tinydelta: ADD needs %d literal bytes: %w", length, err)
			}
			cursor += length

		case opSkip:
			if cursor > uint64(len(dict)) || length > uint64(len(dict))-cursor {
				return nil, fmt.Errorf("tinydelta: SKIP [%d,%d) exceeds dictionary length %d", cursor, cursor+length, len(dict))
			}
			target = append(target, dict[cursor:cursor+length]...)
			cursor += length

		case opTargetCopy:
			delta, err := readZigzagFrom(br)
			if err != nil {
				return nil, fmt.Errorf("tinydelta: COPY_TARGET delta: %w", err)
			}
			addr := int64(len(target)) + delta
			if addr < 0 || addr >= int64(len(target)) {
				return nil, fmt.Errorf("tinydelta: COPY_TARGET address %d invalid at produced target length %d", addr, len(target))
			}
			// Byte by byte: a run whose period is shorter than length is
			// valid (addr+length may exceed len(target) at the start),
			// and each byte just appended must be visible to the next.
			for i := uint64(0); i < length; i++ {
				target = append(target, target[uint64(addr)+i])
			}
			cursor += length // rides along; COPY_TARGET never resyncs it
		}
	}
	return target, nil
}

// readVarintFrom reads one little-endian base-128 varint from r, one byte
// at a time. It returns io.EOF unwrapped only when zero bytes could be
// read at all — the caller's signal that the patch stream has ended
// cleanly between instructions. Any other failure, including EOF after at
// least one byte, means a truncated varint and is reported as such.
func readVarintFrom(r io.ByteReader) (uint64, error) {
	var v uint64
	var shift uint
	for i := 0; ; i++ {
		c, err := r.ReadByte()
		if err != nil {
			if i == 0 {
				return 0, err // unwrapped: the caller's signal for a clean end of patch
			}
			// %v, not %w: this must NOT satisfy errors.Is(_, io.EOF), or a
			// patch truncated mid-varint would be mistaken for a clean end.
			return 0, fmt.Errorf("truncated varint: %v", err)
		}
		if shift == 28 && c > 0x0F {
			return 0, fmt.Errorf("varint exceeds 32 bits") // see maxVarintLen
		}
		v |= uint64(c&0x7f) << shift
		if c&0x80 == 0 {
			return v, nil
		}
		shift += 7
	}
}

// appendFrom appends exactly n bytes read from r to dst, in bounded
// chunks. n comes from the patch itself, so it is not trusted to size an
// allocation up front: a corrupt length near 2^62 would otherwise crash
// the process with an out-of-memory error instead of failing cleanly
// once r runs out.
func appendFrom(dst []byte, r io.Reader, n uint64) ([]byte, error) {
	var buf [4096]byte
	for n > 0 {
		k := min(n, uint64(len(buf)))
		if _, err := io.ReadFull(r, buf[:k]); err != nil {
			return dst, err
		}
		dst = append(dst, buf[:k]...)
		n -= k
	}
	return dst, nil
}

// readZigzagFrom reads one varint via readVarintFrom and zigzag-decodes
// it into a signed delta (see zigzagEncode in format.go).
func readZigzagFrom(r io.ByteReader) (int64, error) {
	v, err := readVarintFrom(r)
	if err != nil {
		return 0, err
	}
	return zigzagDecode(v), nil
}
