// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeFirmware builds two versions of a firmware image for arch, linked
// at base: functions of random code that call each other and a table of
// pointers to them; the new version has a function inserted in the
// middle and a few changed, so everything after the insertion moves and
// every call and pointer into it changes.
func fakeFirmware(r *rand.Rand, arch Relocation, base uint32, functions int) (old, new []byte) {
	type fn struct {
		body []byte
		v    int // MSP430X: the RAM variable it reads
	}
	const vars, ram = 40, 0x1C00   // MSP430X: variables of 4 bytes in RAM
	filler := func(n int) []byte { // code words that look like neither rule's patterns
		b := make([]byte, n)
		for i := 0; i+1 < n; i += 2 {
			w := uint16(0x4000 + r.Intn(0x6000)) // 0x4000-0x9FFF: two-operand MSP430 instructions, Thumb 16-bit ones
			binary.LittleEndian.PutUint16(b[i:], w)
		}
		return b
	}
	fns := make([]fn, functions)
	calls := make([][]int, functions)
	for i := range fns {
		fns[i].body, fns[i].v = filler(40+2*r.Intn(80)), r.Intn(vars)
		for k := 0; k < 3; k++ {
			calls[i] = append(calls[i], r.Intn(functions))
		}
	}
	build := func(fns []fn, calls [][]int, varGap uint32) []byte {
		// Lay out: each function is its body, then its calls (4 bytes each).
		starts := make([]int, len(fns))
		pos := 0
		for i, f := range fns {
			starts[i] = pos
			pos += len(f.body) + 4*len(calls[i])
			if arch == RelocationMSP430X {
				pos += 4
			}
		}
		table := pos
		img := make([]byte, 0, pos+4*len(fns))
		for i, f := range fns {
			img = append(img, f.body...)
			for _, callee := range calls[i] {
				site := len(img)
				var ins [4]byte
				switch arch {
				case RelocationThumb:
					off := uint32(int32(starts[callee]-(site+4))>>1) & 0x3FFFFF
					binary.LittleEndian.PutUint16(ins[0:], uint16(0xF000|off>>11&0x7FF))
					binary.LittleEndian.PutUint16(ins[2:], uint16(0xF800|off&0x7FF))
				case RelocationMSP430X:
					addr := base + uint32(starts[callee])
					binary.LittleEndian.PutUint16(ins[0:], uint16(0x13B0|addr>>16&0xF))
					binary.LittleEndian.PutUint16(ins[2:], uint16(addr))
				}
				img = append(img, ins[:]...)
			}
			if arch == RelocationMSP430X { // MOVA &var,R12; variables past the middle move by varGap
				addr := uint32(ram + 4*f.v)
				if f.v >= vars/2 {
					addr += varGap
				}
				img = binary.LittleEndian.AppendUint16(img, uint16(0x002C|(addr>>16&0xF)<<8))
				img = binary.LittleEndian.AppendUint16(img, uint16(addr))
			}
		}
		_ = table
		for i := range fns { // a table of function pointers
			var p [4]byte
			binary.LittleEndian.PutUint32(p[:], base+uint32(starts[i])|1)
			if arch == RelocationMSP430X { // MOVA #addr,R12 instead
				addr := base + uint32(starts[i])
				binary.LittleEndian.PutUint16(p[0:], uint16(0x008C|(addr>>16&0xF)<<8))
				binary.LittleEndian.PutUint16(p[2:], uint16(addr))
			}
			img = append(img, p[:]...)
		}
		if arch == RelocationMSP430X { // and a constant table of 4-byte pointers, as IAR's DATA20_C
			for k := 0; k < 4; k++ {
				for i := range fns {
					var p [4]byte
					binary.LittleEndian.PutUint32(p[:], base+uint32(starts[(i*7+k)%len(fns)]))
					img = append(img, p[:]...)
				}
			}
		}
		return img
	}
	old = build(fns, calls, 0)
	at := functions / 3
	newFns := append(append(append([]fn{}, fns[:at]...), fn{body: filler(130)}), fns[at:]...)
	newCalls := append(append(append([][]int{}, calls[:at]...), []int{0}), calls[at:]...)
	for i := range newCalls { // indices past the insertion move by one
		cs := append([]int{}, newCalls[i]...)
		for k, c := range cs {
			if c >= at && i != at {
				cs[k] = c + 1
			}
		}
		newCalls[i] = cs
	}
	for k := 0; k < 3; k++ { // a few real changes
		f := &newFns[r.Intn(len(newFns))]
		f.body = append([]byte{}, f.body...)
		copy(f.body[r.Intn(len(f.body)-8)&^1:], filler(6))
	}
	return old, build(newFns, newCalls, 6) // and a variable of 6 bytes added
}

var relocCases = []struct {
	arch Relocation
	base uint32
	cdef string
}{
	{RelocationThumb, 0x2000, "-DTINYDELTA_RELOC=1"},
	{RelocationMSP430X, 0x10000, "-DTINYDELTA_RELOC=2"},
}

func TestRelocationRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(71))
	for _, c := range relocCases {
		old, new := fakeFirmware(r, c.arch, c.base, 300)
		plain, _ := EncodeWith(old, new, EncodeOptions{CoderTables: 777})
		reloc, err := EncodeWith(old, new, EncodeOptions{CoderTables: 777 + 64*relocEntrySize, Relocation: c.arch, ImageBase: c.base})
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(old, reloc.Bytes)
		if err != nil || !bytes.Equal(got, new) {
			t.Fatalf("%v: round trip: %v", c.arch, err)
		}
		if reloc.Bytes[1]>>2 != featuresBuiltinRules {
			t.Fatalf("%v: relocation not chosen (features 0x%02x)", c.arch, reloc.Bytes[1])
		}
		if r, _ := newCodedReader(reloc.Bytes, nil); c.arch == RelocationMSP430X && len(r.reloc.data) == 0 {
			t.Errorf("%v: no data runs for the pointer table", c.arch)
		}
		if len(reloc.Bytes)*2 > len(plain.Bytes) {
			t.Errorf("%v: %d B with relocation, %d B without; want at most half", c.arch, len(reloc.Bytes), len(plain.Bytes))
		}
		t.Logf("%v: %d B image, coded %d B, with relocation %d B", c.arch, len(new), len(plain.Bytes), len(reloc.Bytes))

		// Without RAM for the map, or without a rule: no relocation.
		for _, opts := range []EncodeOptions{{CoderTables: 777}, {CoderTables: 777 + 64*relocEntrySize}} {
			p, _ := EncodeWith(old, new, opts)
			if p.Bytes[0]&codedFlag != 0 && p.Bytes[1]>>2 != 0 {
				t.Errorf("%v %+v: relocation without the option or its RAM", c.arch, opts)
			}
		}
	}
}

func TestRelocationOptions(t *testing.T) {
	if _, err := EncodeWith(nil, []byte("x"), EncodeOptions{Relocation: 3}); err == nil {
		t.Error("unknown Relocation accepted")
	}
	// A map that doesn't fit the RAM keeps its longest entries.
	m := &relocMap{entries: []relocEntry{{0, 10, 1}, {20, 100, 2}, {200, 50, 3}}}
	if got := m.limitTo(2 * relocEntrySize).entries; len(got) != 2 || got[0].start != 20 || got[1].start != 200 {
		t.Errorf("limitTo: %+v", got)
	}
}

func TestRelocationMapHeader(t *testing.T) {
	m := &relocMap{rules: builtinThumb, base: 0x08000000, entries: []relocEntry{{16, 100, -8}, {116, 4, 32}, {1000, 1 << 20, 1 << 30}}}
	b := m.appendHeader(nil)
	got, n, err := parseRelocMap(featuresBuiltinRules, b, nil)
	if err != nil || n != len(b) || fmt.Sprint(got) != fmt.Sprint(m) {
		t.Fatalf("parse: %+v %d %v", got, n, err)
	}
	for cut := 0; cut < len(b); cut++ {
		if _, _, err := parseRelocMap(featuresBuiltinRules, b[:cut], nil); err == nil {
			t.Errorf("truncated map (%d of %d bytes) accepted", cut, len(b))
		}
	}
	// MSP430X: data runs and addresses outside the image too.
	mx := &relocMap{rules: builtinMSP430X, base: 0x8000, entries: []relocEntry{{16, 100, 8}}, data: []relocRun{{3, 2}}, outside: []relocEntry{{0x1C00, 0x40, 6}, {0x1C80, 0x10, -4}}}
	bx := mx.appendHeader(nil)
	if got, n, err := parseRelocMap(featuresBuiltinRules, bx, nil); err != nil || n != len(bx) || fmt.Sprint(got) != fmt.Sprint(mx) {
		t.Fatalf("parse MSP430X: %+v %d %v", got, n, err)
	}
	for cut := 0; cut < len(bx); cut++ {
		if _, _, err := parseRelocMap(featuresBuiltinRules, bx[:cut], nil); err == nil {
			t.Errorf("truncated MSP430X map (%d of %d bytes) accepted", cut, len(bx))
		}
	}
	if mx.addressShift(0x1C3E, 1000) != 6 || mx.addressShift(0x1C84, 1000) != -4 || mx.addressShift(0x1C40, 1000) != 0 || mx.addressShift(0x8000+20, 1000) != 8 {
		t.Error("addressShift outside the image")
	}
	// An entry past 32 bits.
	if _, _, err := parseRelocMap(featuresBuiltinRules, appendVarint(appendVarint(appendVarint(appendVarint(appendVarint(nil, 0), 1), 0xFFFFFFF0), 0x20), 0), nil); err == nil {
		t.Error("entry past 32 bits accepted")
	}
}

// TestCRelocation decodes relocated patches with the C decoders built
// for each rule, at many buffer and page sizes (so relocation runs on
// spans cut everywhere), and compares corrupted ones with Go.
func TestCRelocation(t *testing.T) {
	r := rand.New(rand.NewSource(73))
	for _, c := range relocCases {
		memory := buildCDriver(t, append(append([]string{}, cCoderSources...), c.cdef, "c/tinydelta_reloc.c")...)
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "driver.c"), []byte(cIODriver), 0o644)
		callback := filepath.Join(dir, "decode_io")
		cCompile(t, callback, append(append([]string{}, cCoderSources...), c.cdef, "c/tinydelta_reloc.c", "c/tinydelta_io.c", filepath.Join(dir, "driver.c"))...)
		memoryDecode := func(dict, patch []byte, capacity int) ([]byte, string, bool) {
			d, p, o := filepath.Join(dir, "d"), filepath.Join(dir, "p"), filepath.Join(dir, "o")
			os.WriteFile(d, dict, 0o644)
			os.WriteFile(p, patch, 0o644)
			msg, err := cCommand(memory, d, p, o, fmt.Sprint(capacity)).CombinedOutput()
			if err != nil {
				return nil, string(bytes.TrimSpace(msg)), false
			}
			out, _ := os.ReadFile(o)
			return out, "", true
		}

		old, new := fakeFirmware(r, c.arch, c.base, 45)
		if len(new) > cCapacity {
			t.Fatalf("test image %d B, over cCapacity", len(new))
		}
		var patches [][]byte
		for _, layout := range []int{codedLayoutA, codedLayoutB, codedLayoutC} {
			m := relocMapFrom(old, new, rulesFor(c.arch), c.base).withDataRuns(old, new).withOutside(old, new)
			if c.arch == RelocationMSP430X && (len(m.data) == 0 || len(m.outside) == 0) {
				t.Fatalf("%v: no data runs or RAM entries to test", c.arch)
			}
			rd := m.relocate(old)
			v1, _ := Encode(rd, new)
			patches = append(patches, encodeCodedReloc(v1, layout, m), encodeCodedReloc(withVersion(FormatVersion2, encodeCovers(rd, new, MaxSize)), layout, m))
		}
		tables := CodedTablesSize(codedLayoutA) + 64*relocEntrySize
		for i, patch := range patches {
			if got, code, ok := memoryDecode(old, patch, cCapacity); !ok || !bytes.Equal(got, new) {
				t.Fatalf("%v patch %d: memory decoder: %s", c.arch, i, code)
			}
			for _, cfg := range []ioConfig{{1, 1, tables}, {8, 2, tables}, {16, 16, tables}, {64, 8, tables}, {256, 256, tables}} {
				if got, code, ok := runCIO(t, callback, cfg, true, old, patch, cCapacity); !ok || !bytes.Equal(got, new) {
					t.Fatalf("%v patch %d %+v: callback decoder: %s", c.arch, i, cfg, code)
				}
			}
		}

		// Corrupted and truncated: accept or reject as Go does, same output.
		patch := patches[3]
		for k := 0; k < 150; k++ {
			bad := append([]byte{}, patch...)
			switch k % 3 {
			case 0:
				bad = bad[:r.Intn(len(bad))]
			default:
				pos := 1 + r.Intn(min(len(bad)-1, 40)) // mostly the header and map
				bad[pos] ^= byte(1 + r.Intn(255))
			}
			want, werr := DecodeReaderLimit(old, bytes.NewReader(bad), cCapacity)
			got, code, ok := memoryDecode(old, bad, cCapacity)
			if ok != (werr == nil) || (ok && !bytes.Equal(got, want)) {
				t.Fatalf("%v corruption %d: memory decoder ok=%v (%s), Go err=%v", c.arch, k, ok, code, werr)
			}
			got, code, ok = runCIO(t, callback, ioConfig{16, 4, tables}, true, old, bad, cCapacity)
			if ok != (werr == nil) || (ok && !bytes.Equal(got, want)) {
				t.Fatalf("%v corruption %d: callback decoder ok=%v (%s), Go err=%v", c.arch, k, ok, code, werr)
			}
		}

		// The other rule's patch, a map too big for the tables, and a
		// decoder built without relocation: TINYDELTA_ERR_VERSION.
		other := append([]byte{}, patches[0]...)
		other[1] ^= 0x0C // 1 <-> 2
		if _, code, ok := memoryDecode(old, other, cCapacity); ok || code != "-4" {
			t.Errorf("%v: other rule's patch: ok=%v code %s, want -4 (VERSION)", c.arch, ok, code)
		}
		if _, code, ok := runCIO(t, callback, ioConfig{16, 16, CodedTablesSize(codedLayoutA)}, true, old, patches[0], cCapacity); ok || code != "-4" {
			t.Errorf("%v: no room for the map: ok=%v code %s, want -4 (VERSION)", c.arch, ok, code)
		}
	}
	plainCoder := buildCDriver(t, cCoderSources...)
	r2 := rand.New(rand.NewSource(74))
	old, new := fakeFirmware(r2, RelocationThumb, 0x2000, 40)
	p, _ := EncodeWith(old, new, EncodeOptions{CoderTables: 2000, Relocation: RelocationThumb, ImageBase: 0x2000})
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "d"), old, 0o644)
	os.WriteFile(filepath.Join(dir, "p"), p.Bytes, 0o644)
	if msg, err := cCommand(plainCoder, filepath.Join(dir, "d"), filepath.Join(dir, "p"), filepath.Join(dir, "o"), fmt.Sprint(cCapacity)).CombinedOutput(); err == nil || string(bytes.TrimSpace(msg)) != "-4" {
		t.Errorf("decoder without relocation: %v %s, want -4 (VERSION)", err, msg)
	}
}

func TestRelocationStats(t *testing.T) {
	r := rand.New(rand.NewSource(75))
	old, new := fakeFirmware(r, RelocationMSP430X, 0x10000, 60)
	s := RelocationStats(old, new, builtinMSP430X, 0x10000)
	for _, want := range []string{"map: ", "left to the patch:", "differing 16-bit words:", "by 4 KB of the new image", "by the words around them"} {
		if !strings.Contains(s, want) {
			t.Errorf("RelocationStats lacks %q:\n%s", want, s)
		}
	}
}

// TestRelocationVerified: EncodeWith decodes the relocated patch it picks,
// and refuses to return one that doesn't give the target.
func TestRelocationVerified(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	old, new := fakeFirmware(r, RelocationThumb, 0x2000, 20)
	opts := EncodeOptions{CoderTables: 1545, Relocation: RelocationThumb, ImageBase: 0x2000}
	testRelocFault = func(p []byte) { p[len(p)/2] ^= 0x40 }
	defer func() { testRelocFault = nil }()
	if _, err := EncodeWith(old, new, opts); err == nil || !strings.Contains(err.Error(), "internal error") {
		t.Fatalf("a damaged relocated patch: got %v, want an internal error", err)
	}
	testRelocFault = nil
	if p, err := EncodeWith(old, new, opts); err != nil || p.Bytes[1]>>2 == 0 {
		t.Fatalf("undamaged: %v, features %x", err, p.Bytes[1])
	}
}
