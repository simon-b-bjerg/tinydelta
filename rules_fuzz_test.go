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

// FuzzRelocRoundTrip encodes an update of a firmware-like image with
// relocation (Thumb or MSP430X, built in or shipped) and decodes it again:
// the target must come back, whatever the image, the edit and the base.
func FuzzRelocRoundTrip(f *testing.F) {
	r := rand.New(rand.NewSource(8))
	for i, rules := range []*Rules{builtinThumb, builtinMSP430X} {
		img := randomCode(r, rules, 0x10000, 600)
		f.Add(img, []byte{0, 100, 6, 'X'}, byte(i))
		f.Add(img, []byte{1, 40, 4, 'Y', 2, 200, 8, 0}, byte(i|2))
		f.Add(img, []byte{1, 10, 2, 'Z'}, byte(i|4))
	}
	f.Fuzz(func(t *testing.T, img, script []byte, flags byte) {
		if len(img) > fuzzMaxSize {
			img = img[:fuzzMaxSize]
		}
		target := applyEdits(img, script)
		opts := EncodeOptions{CoderTables: 1287 + 1024, Relocation: RelocationThumb, ShipRules: flags&2 != 0, ImageBase: 0x10000 + uint32(flags>>2&7)}
		if flags&1 != 0 {
			opts.Relocation = RelocationMSP430X
		}
		if opts.ShipRules {
			opts.CoderTables += len(rulesFor(opts.Relocation).table)
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
		got, err := Decode(img, patch.Bytes)
		if err != nil {
			t.Fatalf("decode (%+v): %v", opts, err)
		}
		if !bytes.Equal(got, target) {
			t.Fatalf("round trip (%+v): %d B, want %d B", opts, len(got), len(target))
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
