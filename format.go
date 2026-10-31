// Package tinydelta implements a minimal, custom binary delta format with
// no RFC 3284 heritage. A patch is a one-byte header, the format version
// (FormatVersion), followed by a bare stream of instructions; Decode is
// handed the dictionary and the patch bytes, checks the version and runs
// instructions until the patch is exhausted. There is no other framing:
// no lengths, no checksum.
//
// The version byte names the instruction set. A decoder rejects any
// version it does not know (ErrUnsupportedVersion) instead of producing
// a wrong target, so the instruction set can change in a later version
// without old decoders silently misreading new patches.
//
// Each instruction starts with one little-endian base-128 varint (LEB128:
// the opposite byte order from VCDIFF's big-endian varint) whose low 2
// bits are the opcode and whose remaining bits
// (value >> 2) are len, the number of target bytes this instruction
// produces. The output position (bytes of target produced so far)
// advances by len after every instruction, regardless of opcode.
//
// The format is 32-bit throughout, so a decoder never needs wider
// arithmetic:
//
//   - Every varint in a patch (instruction, COPY/COPY_TARGET delta) holds
//     at most 32 bits: at most maxVarintLen (5) bytes, the 5th carrying
//     only bits 28-31, so it must be 0x00-0x0F. Decoders reject anything
//     longer.
//   - The dictionary and the target are each at most MaxSize (2^30-1)
//     bytes. That bounds an instruction's len (the varint shifted right
//     by 2) by 2^30-1, every zigzag delta to [-2^31, 2^31-1], and the
//     output position and dictionary cursor to below 2^31.
//
// A dictionary cursor is tracked alongside the output position, starting
// at 0. It advances by len after every instruction too, except that COPY
// additionally resyncs it to wherever it actually read from — so absent
// any COPY with a nonzero delta, the cursor simply tracks the output
// position one-for-one, which is the common case for same-length
// dictionary/target pairs (e.g. config or firmware patching) where most
// of the file is unchanged.
//
//   - COPY (opcode 0): len bytes copied from the dictionary. A second
//     varint immediately follows: a zigzag-encoded signed delta from the
//     current cursor (see zigzagEncode) giving the dictionary address to
//     copy from. len == 0 is a valid, deliberate case: no bytes are
//     copied, but the cursor is still resynced to the given address —
//     this is how to explicitly reset the cursor (e.g. after an edit
//     that shifted dictionary/target alignment) without emitting output.
//   - ADD (opcode 1): len literal bytes, taken from the next len bytes of
//     the patch stream itself, appended to the target as-is.
//   - SKIP (opcode 2): len bytes copied from the dictionary at the
//     *current cursor position* — no address field at all, since the
//     cursor already names it. This is COPY's len==0 case taken to its
//     opposite extreme: the cheapest possible instruction, one varint,
//     for the common case of a run that's unchanged from the dictionary
//     at the position the cursor already names.
//   - COPY_TARGET (opcode 3): len bytes copied from the target already
//     produced, not the dictionary (and not affecting the dictionary
//     cursor). A second varint immediately follows: a zigzag-encoded
//     signed delta from the current output position giving the address
//     to copy from, which must land strictly before it — there must be
//     at least one already-produced byte there. Copying is byte by byte,
//     so addr+len may legitimately exceed the length produced so far: a
//     run whose period is shorter than len is valid and reproduces
//     correctly (e.g. a delta of -1 encodes an arbitrarily long run of
//     one byte).
//
// Format version 2 (FormatVersion2) is made for patches that are
// compressed afterwards. It has the same header, varints, cursor and
// opcodes 0-2; only opcode 3 differs:
//
//   - ADD_DIFF (opcode 3, version 2 only, in place of COPY_TARGET): len
//     bytes from the patch, each added (mod 256) to the dictionary byte at
//     the cursor: target = dict[cursor+i] + patch byte. The range
//     [cursor, cursor+len) must lie within the dictionary; the cursor
//     advances by len like every instruction. A region that lines up with
//     the dictionary but has a few changed bytes (moved addresses, branch
//     offsets) becomes mostly zeros and the same few difference values,
//     which a compressor reduces to very little. Repeats within the
//     target, COPY_TARGET's job in version 1, are left to that compressor.
package tinydelta

// FormatVersion is the format version Encode, EncodeGreedy and
// EncodeWith write by default: the first byte of the patch.
const FormatVersion = 1

// FormatVersion2 is the format version EncodeWith writes with
// ForCompression: COPY_TARGET is replaced by ADD_DIFF (see the package
// doc). Decode reads both versions.
const FormatVersion2 = 2

// headerLen is the size of the patch header: the version byte.
const headerLen = 1

// withHeader returns the version-1 patch made of the header and the
// instruction stream instructions.
func withHeader(instructions []byte) []byte {
	return withVersion(FormatVersion, instructions)
}

// withVersion returns the patch made of the version byte and the
// instruction stream instructions.
func withVersion(version byte, instructions []byte) []byte {
	patch := make([]byte, 0, headerLen+len(instructions))
	patch = append(patch, version)
	return append(patch, instructions...)
}

const (
	opCopy       = 0
	opAdd        = 1
	opSkip       = 2
	opTargetCopy = 3 // version 1
	opAddDiff    = 3 // version 2
)

// MaxSize is the largest dictionary or target the format supports, just
// under 1 GiB: the bound that keeps every field of a patch within 32 bits
// (see the package doc). Encoders reject larger inputs; decoders never
// produce a larger target.
const MaxSize = 1<<30 - 1

// maxVarintLen is the most bytes a varint may take: ceil(32/7).
const maxVarintLen = 5

// appendVarint appends v as a little-endian base-128 varint (LEB128): each
// byte holds 7 bits, least-significant group first, high bit set means
// more bytes follow (the opposite byte order from RFC 3284/VCDIFF's
// big-endian base-128 varint).
func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

// varintLen is the encoded length of v.
func varintLen(v uint64) int {
	n := 1
	for v >= 0x80 {
		v >>= 7
		n++
	}
	return n
}

// zigzagEncode maps a signed value to an unsigned one, small values of
// either sign mapping to small results (0, -1, 1, -2, 2, ... -> 0, 1, 2,
// 3, 4, ...), so that COPY/COPY_TARGET's delta fields cost the same
// whether the match they describe is before or after the reference
// point, rather than only being cheap for one direction.
func zigzagEncode(v int64) uint64 {
	return uint64(v<<1) ^ uint64(v>>63)
}

// zigzagDecode reverses zigzagEncode.
func zigzagDecode(u uint64) int64 {
	return int64(u>>1) ^ -int64(u&1)
}

// readVarint decodes a little-endian base-128 varint from the start of b,
// returning the value and the number of bytes consumed, or (0, -1) if b
// ends before a terminating byte (high bit clear) is found, or the value
// would exceed 32 bits.
func readVarint(b []byte) (uint64, int) {
	var v uint64
	var shift uint
	for i, c := range b {
		if shift == 28 && c > 0x0F {
			return 0, -1 // over 32 bits; see maxVarintLen
		}
		v |= uint64(c&0x7f) << shift
		if c&0x80 == 0 {
			return v, i + 1
		}
		shift += 7
	}
	return 0, -1
}
