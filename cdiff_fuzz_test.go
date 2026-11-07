// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/simon-b-bjerg/tinydelta/internal/cdiff"
)

// FuzzRelocGoVsC is differential fuzzing of the decoders on relocated
// patches: the Go decoder and the C memory and callback decoders, linked in
// through cgo (internal/cdiff), must accept and reject exactly the same
// patches, with the same output. Each input is a relocated update as
// FuzzRelocRoundTrip makes them (relocFuzzCase, forcedRelocPatch), then
// maybe damaged (bytes flipped, cut short) or decoded with one byte too
// little room. A patch with the Thumb table built in goes to the C build
// with Thumb built in (as on a Cortex-M device), others to the build with
// MSP430X built in and shipped tables accepted. A build refuses a patch
// naming a built-in table it hasn't, or shipping one when it takes none,
// which the Go decoder (knowing every table) may accept.
// Skipped without cgo.
//
// flags: as relocFuzzOptions. io: bit 0 one byte too little room, bits 1-2
// the callback decoder's buffer (16 << 2n), bit 3 its page a quarter of it.
// damage: pairs of (position, xor); a first byte with bit 7 set cuts the
// patch at damage[1]/256 of its length instead.
func FuzzRelocGoVsC(f *testing.F) {
	if !cdiff.Available {
		f.Skip("the C decoders need cgo")
	}
	relocFuzzSeeds(func(img, script []byte, flags byte, at uint16, gap byte) {
		f.Add(img, script, flags, at, gap, byte(0), []byte{})
		f.Add(img, script, flags, at, gap, byte(6), []byte{7, 0x40, 30, 0x01})
		f.Add(img, script, flags, at, gap, byte(9), []byte{0x80, 120})
	})
	f.Fuzz(func(t *testing.T, img, script []byte, flags byte, at uint16, gap byte, io byte, damage []byte) {
		if len(img) > fuzzMaxSize {
			img = img[:fuzzMaxSize]
		}
		const base = 0x10000
		rules, relocation, ship, layout, dataRuns, moveRAM := relocFuzzOptions(flags)
		builds, buildRules := []cdiff.Build{cdiff.MSP430X}, builtinMSP430X
		switch {
		case relocation == RelocationThumb && !ship:
			builds, buildRules = []cdiff.Build{cdiff.Thumb}, builtinThumb
		case !ship:
			builds = append(builds, cdiff.MSP430XOnly) // the interpreter, and the code generated for the table
		}
		target := relocFuzzCase(img, script, rules, base, int(at), int(gap), moveRAM)
		patch := forcedRelocPatch(img, target, rules, base, ship, layout, dataRuns)
		if patch == nil {
			return
		}
		if len(damage) >= 2 && damage[0]&0x80 != 0 {
			patch = patch[:len(patch)*int(damage[1])/256]
		} else {
			for i := 0; i+1 < len(damage); i += 2 {
				patch[int(damage[i])%len(patch)] ^= damage[i+1]
			}
		}
		capacity := len(target)
		if io&1 != 0 && capacity > 1 {
			capacity--
		}
		// Room for any map a damaged patch can describe: each record takes
		// at least 3 bytes of the patch and at most 12 of the tables buffer.
		tablesSize := CodedTablesSize(codedLayoutA) + 4096 + 4*len(patch)
		buffer := 16 << (2 * (io >> 1 & 3))
		cfg := cdiff.IOConfig{BufferSize: buffer, PageSize: buffer >> (2 * (io >> 3 & 1))}

		want, goErr := DecodeRules(img, bytes.NewReader(patch), max(capacity, 1), builtinMSP430X)
		for _, build := range builds {
			mem, memRC := cdiff.Decode(build, img, patch, max(capacity, 1), tablesSize)
			cb, cbRC, violation := cdiff.DecodeIO(build, img, patch, max(capacity, 1), tablesSize, cfg)
			if violation != "" {
				t.Fatalf("callback decoder (%+v): %s", cfg, violation)
			}
			// A coded patch's features: bits 2-3 1 for a built-in table named by
			// CRC (next two bytes), 2 for a shipped one.
			coded := len(patch) >= 2 && (patch[0] == 0x81 || patch[0] == 0x82) && patch[1]&0xF0 == 0 && patch[1]&3 != 0
			otherTable := coded && patch[1]>>2&3 == 1 && len(patch) >= 4 && uint16(patch[2])|uint16(patch[3])<<8 != buildRules.crc
			shipped := coded && patch[1]>>2&3 == 2 && build != cdiff.MSP430X
			if otherTable || shipped {
				if memRC == cdiff.OK || cbRC == cdiff.OK {
					t.Fatalf("a patch with a table the C build doesn't take was accepted: memory %d, callback %d", memRC, cbRC)
				}
				continue
			}
			if (goErr == nil) != (memRC == cdiff.OK) || (goErr == nil && !bytes.Equal(want, mem)) {
				t.Fatalf("memory decoder (C build %d): result %d, Go: %v (outputs equal: %v)", build, memRC, goErr, bytes.Equal(want, mem))
			}
			if (goErr == nil) != (cbRC == cdiff.OK) || (goErr == nil && !bytes.Equal(want, cb)) {
				t.Fatalf("callback decoder (C build %d, %+v): result %d, Go: %v (outputs equal: %v)", build, cfg, cbRC, goErr, bytes.Equal(want, cb))
			}
		}
		intact := len(damage) < 2 && capacity == len(target)
		if intact && (goErr != nil || !bytes.Equal(want, target)) {
			t.Fatalf("an intact patch didn't decode to its target: %v", goErr)
		}
	})
}

// TestMSP430X16BitAddresses checks the rules for classic MSP430 instructions'
// 16-bit addresses, hand-assembled: those into a RAM range that moved move
// with it, stack offsets, PC-relative operands and the constant generator
// stay; and the C decoder relocates them exactly as Go does.
func TestMSP430X16BitAddresses(t *testing.T) {
	type insn struct {
		what        string
		words, want []uint16
	}
	const shift = 0x20 // RAM from 0x1C00 moves up by 0x20
	insns := []insn{
		{"MOV.W &0x1C10,R12", []uint16{0x421C, 0x1C10}, []uint16{0x421C, 0x1C30}},
		{"MOV.B #1,0x1C20(R10)", []uint16{0x43DA, 0x1C20}, []uint16{0x43DA, 0x1C40}},
		{"MOV.W #0x1C30,R12", []uint16{0x403C, 0x1C30}, []uint16{0x403C, 0x1C50}},
		{"MOV.W &0x1C10,&0x1C12", []uint16{0x4292, 0x1C10, 0x1C12}, []uint16{0x4292, 0x1C30, 0x1C32}},
		{"MOV.W #0x1C30,0x1C40(R9)", []uint16{0x40B9, 0x1C30, 0x1C40}, []uint16{0x40B9, 0x1C50, 0x1C60}},
		{"PUSH &0x1C14", []uint16{0x1212, 0x1C14}, []uint16{0x1212, 0x1C34}},
		{"PUSH #0x1C16", []uint16{0x1230, 0x1C16}, []uint16{0x1230, 0x1C36}},
		{"ADD.W 0x1C18(R4),R5", []uint16{0x5415, 0x1C18}, []uint16{0x5415, 0x1C38}},
		{"MOV.W 0x1C08(SP),R12 (stack offset)", []uint16{0x411C, 0x1C08}, []uint16{0x411C, 0x1C08}},
		{"MOV.W 0x1C00(PC),R12 (PC-relative)", []uint16{0x401C, 0x1C00}, []uint16{0x401C, 0x1C00}},
		{"MOV.W R12,0x1C00(PC) (PC-relative)", []uint16{0x4C80, 0x1C00}, []uint16{0x4C80, 0x1C00}},
		{"MOV.W #0x1C02,0x1C04(SP)", []uint16{0x40B1, 0x1C02, 0x1C04}, []uint16{0x40B1, 0x1C22, 0x1C04}},
		{"MOV.W 0x1C06(SP),&0x1C0A", []uint16{0x4192, 0x1C06, 0x1C0A}, []uint16{0x4192, 0x1C06, 0x1C2A}},
		{"MOV.W #1,&0x1C0C (constant generator)", []uint16{0x4392, 0x1C0C}, []uint16{0x4392, 0x1C2C}},
		{"MOV.W #0x0123,R12 (not in a RAM range)", []uint16{0x403C, 0x0123}, []uint16{0x403C, 0x0123}},
		{"RETI", []uint16{0x1300}, []uint16{0x1300}},
	}
	var img, want []byte
	for _, in := range insns {
		for i, w := range in.words {
			img = binary.LittleEndian.AppendUint16(img, w)
			want = binary.LittleEndian.AppendUint16(want, in.want[i])
		}
	}
	m := &relocMap{rules: builtinMSP430X, base: 0x4400, entries: []relocEntry{{0, uint32(len(img)), 0}},
		outside: []relocEntry{{0x1C00, 0x800, shift}}}
	got := m.relocate(img)
	at := 0
	for _, in := range insns {
		n := 2 * len(in.words)
		if !bytes.Equal(got[at:at+n], want[at:at+n]) {
			t.Errorf("%s: % x, want % x", in.what, got[at:at+n], want[at:at+n])
		}
		at += n
	}
	if !cdiff.Available {
		return
	}
	v, err := Encode(got, want)
	if err != nil {
		t.Fatal(err)
	}
	patch := encodeCodedReloc(v, codedLayoutA, m)
	out, rc := cdiff.Decode(cdiff.MSP430X, img, patch, len(want), CodedTablesSize(codedLayoutA)+256)
	if rc != cdiff.OK || !bytes.Equal(out, want) {
		t.Fatalf("C decoder: result %d, output equal %v", rc, bytes.Equal(out, want))
	}
}
