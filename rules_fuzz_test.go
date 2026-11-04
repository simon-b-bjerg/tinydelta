// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// Fuzzing relocation from the Go side: relocated round trips, binary rule
// tables and YAML rule files as untrusted input. The C side has its own
// harnesses (c/fuzz/fuzz_reloc.c, and relocated patches in
// c/fuzz/fuzz_decode.c).

// relocFuzzCase is an update as firmware updates go, from a fuzz input: code
// (gap bytes, a copy of the image's start) inserted at offset at, so
// everything after it moves and every address of it (calls, pointers, in
// code and data) changes; with moveRAM, MSP430X variables from 0x1C10 on
// moving by 8 bytes too; then the edits of script. The insertion is applied
// with the relocation engine itself (relocate), so the old image's
// addresses are rewritten as the new image's would be.
func relocFuzzCase(img, script []byte, rules *Rules, base uint32, at, gap int, moveRAM bool) []byte {
	at = min(at, len(img)) &^ 1
	gap &^= 1
	m := &relocMap{rules: rules, base: base}
	if at > 0 {
		m.entries = append(m.entries, relocEntry{0, uint32(at), 0})
	}
	if at < len(img) {
		m.entries = append(m.entries, relocEntry{uint32(at), uint32(len(img) - at), int32(gap)})
	}
	if moveRAM {
		m.outside = []relocEntry{{0x1C10, 0x3F0, 8}}
	}
	moved := m.relocate(img)
	target := append(append(append([]byte{}, moved[:at]...), img[:min(gap, len(img))]...), moved[at:]...)
	return applyEdits(target, script)
}

// relocFuzzRAM is the tables buffer a fuzzed relocated patch's map may use.
const relocFuzzRAM = 64 * relocEntrySize

// forcedRelocPatch is a relocated patch of the update, built as EncodeWith
// builds its relocated candidates, but kept even where a plain patch would
// be smaller (as for small or unrelated images), so every input exercises
// the relocated format. nil if the map finds nothing to relocate.
func forcedRelocPatch(img, target []byte, rules *Rules, base uint32, ship bool, layout int, dataRuns bool) []byte {
	full := relocMapFrom(img, target, rules, base)
	full.ship = ship
	m := full.withOutside(img, target).limitTo(relocFuzzRAM)
	if m == nil || len(m.entries) == 0 {
		m = full.limitTo(relocFuzzRAM)
	}
	if m == nil || len(m.entries) == 0 {
		return nil
	}
	if md := m.withDataRuns(img, target).limitTo(relocFuzzRAM); dataRuns && md != nil {
		m = md
	}
	v, err := Encode(m.relocate(img), target)
	if err != nil {
		return nil
	}
	return encodeCodedReloc(v, layout, m)
}

// relocFuzzSeeds adds firmware-like seeds: per rule set, an image of code
// (and, for MSP430X, a data run of pointers) with code inserted at a few
// places, built in and shipped.
func relocFuzzSeeds(add func(img, script []byte, flags byte, at uint16, gap byte)) {
	r := rand.New(rand.NewSource(8))
	for i, rules := range []*Rules{builtinThumb, builtinMSP430X} {
		img := randomCode(r, rules, 0x10000, 700)
		add(img, nil, byte(i), 300, 20)
		add(img, []byte{0, 100, 6, 'X'}, byte(i|2), 100, 64)
		add(img, []byte{1, 40, 4, 'Y'}, byte(i|0x14), 500, 2)
		add(img, nil, byte(i|2|0x28), 0, 30)
	}
}

// relocFuzzOptions decodes the flags byte of the relocation fuzzers: bit 0
// MSP430X rules (else Thumb), bit 1 ship them, bits 2-3 coder layout, bit 4
// data runs, bit 5 MSP430X variables move.
func relocFuzzOptions(flags byte) (rules *Rules, relocation Relocation, ship bool, layout int, dataRuns, moveRAM bool) {
	rules, relocation = builtinThumb, RelocationThumb
	if flags&1 != 0 {
		rules, relocation = builtinMSP430X, RelocationMSP430X
	}
	layout = []int{codedLayoutA, codedLayoutC, codedLayoutB, codedLayoutA}[flags>>2&3]
	return rules, relocation, flags&2 != 0, layout, flags&0x10 != 0, flags&0x20 != 0
}

// FuzzRelocRoundTrip encodes an update of a firmware-like image with
// relocation (Thumb or MSP430X, built in or shipped, code inserted so that
// code moves) and decodes it again, both as EncodeWith picks the patch and
// as a relocated patch it would maybe not pick: the target must come back.
// An encode taking over 10 s fails, so the input is saved.
func FuzzRelocRoundTrip(f *testing.F) {
	relocFuzzSeeds(func(img, script []byte, flags byte, at uint16, gap byte) { f.Add(img, script, flags, at, gap) })
	f.Fuzz(func(t *testing.T, img, script []byte, flags byte, at uint16, gap byte) {
		if len(img) > fuzzMaxSize {
			img = img[:fuzzMaxSize]
		}
		const base = 0x10000
		rules, relocation, ship, layout, dataRuns, moveRAM := relocFuzzOptions(flags)
		target := relocFuzzCase(img, script, rules, base, int(at), int(gap), moveRAM)
		opts := EncodeOptions{CoderTables: CodedTablesSize(layout) + relocFuzzRAM, Relocation: relocation, ShipRules: ship, ImageBase: base}
		if ship {
			opts.CoderTables += len(rules.table)
		}
		var patch Patch
		var err error
		done := make(chan struct{})
		go func() { patch, err = EncodeWith(img, target, opts); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("encode took over 10 s (%+v, image %d B, target %d B)", opts, len(img), len(target))
		}
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if got, err := Decode(img, patch.Bytes); err != nil || !bytes.Equal(got, target) {
			t.Fatalf("round trip (%+v): %v (%d B, want %d B)", opts, err, len(got), len(target))
		}
		if forced := forcedRelocPatch(img, target, rules, base, ship, layout, dataRuns); forced != nil {
			if got, err := Decode(img, forced); err != nil || !bytes.Equal(got, target) {
				t.Fatalf("relocated round trip (%+v, data runs %v): %v (%d B, want %d B)", opts, dataRuns, err, len(got), len(target))
			}
		}
	})
}

// FuzzRulesTable checks binary rule tables as a patch could ship them: any
// bytes are parsed without a panic, an accepted table is exactly the bytes
// it was parsed from, and relocating an image with it doesn't panic either.
func FuzzRulesTable(f *testing.F) {
	for _, rules := range []*Rules{builtinThumb, builtinMSP430X} {
		f.Add(rules.Table(), byte(0))
		f.Add(rules.Table()[:len(rules.Table())/2], byte(1))
	}
	r := rand.New(rand.NewSource(9))
	img := randomCode(r, builtinMSP430X, 0x10000, 512)
	m := randomMap(r, builtinMSP430X, 0x10000, len(img))
	f.Fuzz(func(t *testing.T, table []byte, base byte) {
		rules, err := ParseRulesTable(table)
		if err != nil {
			return
		}
		if !bytes.Equal(rules.Table(), table) {
			t.Fatal("an accepted table isn't its own bytes")
		}
		mm := m.with(m.entries, m.data, m.outside)
		mm.rules, mm.base = rules, 0x10000+uint32(base)
		mm.relocate(img)
	})
}

// FuzzParseRules checks YAML rule files: any text is read without a panic,
// and an accepted file compiles to a table that parses back to the same
// bytes.
func FuzzParseRules(f *testing.F) {
	for _, name := range []string{"thumb", "msp430x"} {
		src, err := builtinRuleFiles.ReadFile("rules/" + name + ".yaml")
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(src))
	}
	f.Add("version: 1\nname: t\nunit: 2\nword: 16\nendian: little\nsync: 0\ncode:\n  - name: a\n    match: [\"13B0/FFF0\"]\n    address: [\"w0[3:0]\", \"w1[15:0]\"]\n")
	f.Fuzz(func(t *testing.T, src string) {
		rules, err := ParseRules([]byte(src))
		if err != nil {
			if strings.Contains(err.Error(), "runtime error") {
				t.Fatalf("panic: %v", err)
			}
			return
		}
		again, err := ParseRulesTable(rules.Table())
		if err != nil || !bytes.Equal(again.Table(), rules.Table()) {
			t.Fatalf("an accepted file's table doesn't parse back: %v", err)
		}
	})
}
