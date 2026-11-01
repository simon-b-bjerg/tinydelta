// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// twoPassCase is one update sent in compression mode: inner is the patch
// from the old to the new file, outer is inner encoded again against an
// empty dictionary, which is what goes over the air.
type twoPassCase struct {
	name                string
	dict, target, inner []byte
	outer               []byte
	outerName           string
}

// twoPassCases builds the chain the README's server recipe produces, with
// inner patches of both format versions and outer patches from both
// encoders and with a limited copy distance. Targets stay small enough
// for the C tests' capacity, including in the MSP430 simulator.
func twoPassCases(t *testing.T) []twoPassCase {
	t.Helper()
	r := rand.New(rand.NewSource(21))
	firmware, shifted := shiftedFirmwareN(r, 300)
	text := textFile(r, fileSize)
	updates := []struct {
		name         string
		dict, target []byte
	}{
		{"shifted addresses", firmware, shifted},
		{"small edit", text, splice(text, 900, 3, []byte("xyz"))},
		{"identical", text, text},
		{"empty dict", nil, text},
		{"empty target", text, nil},
	}
	inners := []struct {
		name string
		fn   func(dict, target []byte) []byte
	}{
		{"for compression", func(d, tg []byte) []byte { return mustEncode(t, d, tg, EncodeOptions{ForCompression: true}) }},
		{"for compression, greedy", func(d, tg []byte) []byte {
			return mustEncode(t, d, tg, EncodeOptions{ForCompression: true, Greedy: true})
		}},
		{"version 2", func(d, tg []byte) []byte { return withVersion(FormatVersion2, encodeCovers(d, tg, MaxSize)) }},
	}
	outers := []struct {
		name string
		opts EncodeOptions
	}{
		{"optimal", EncodeOptions{}},
		{"greedy", EncodeOptions{Greedy: true}},
		{"1 KB distance", EncodeOptions{MaxTargetDistance: 1024}},
	}
	var cases []twoPassCase
	for _, u := range updates {
		for _, in := range inners {
			inner := in.fn(u.dict, u.target)
			for _, out := range outers {
				cases = append(cases, twoPassCase{
					name: u.name + "/" + in.name + "/" + out.name,
					dict: u.dict, target: u.target, inner: inner,
					outer: mustEncode(t, nil, inner, out.opts), outerName: out.name,
				})
			}
		}
	}
	return cases
}

// TestTwoPass decodes every chain twice with the Go decoder: outer against
// an empty dictionary gives inner, inner against the old file gives the
// new file. The shifted addresses must come out smaller than one plain pass.
func TestTwoPass(t *testing.T) {
	for _, c := range twoPassCases(t) {
		inner, err := Decode(nil, c.outer)
		if err != nil || !bytes.Equal(inner, c.inner) {
			t.Fatalf("%s: first pass: %v", c.name, err)
		}
		got, err := Decode(c.dict, inner)
		if err != nil || !bytes.Equal(got, c.target) {
			t.Fatalf("%s: second pass: %v", c.name, err)
		}
	}
	r := rand.New(rand.NewSource(22))
	dict, target := shiftedFirmware(r)
	plain := mustEncode(t, dict, target, EncodeOptions{})
	outer := mustEncode(t, nil, mustEncode(t, dict, target, EncodeOptions{ForCompression: true}), EncodeOptions{})
	if len(outer)*2 > len(plain) {
		t.Errorf("shifted addresses: two passes %d B, one plain pass %d B; want at most half", len(outer), len(plain))
	}
}

// TestCTwoPass runs the same chains through the C memory and callback
// decoders, each decoding twice, as a device does. The callback decoder
// runs with a 1-byte buffer (every far copy through target_read) and with
// a 1 KB one, where the outer patch limited to 1 KB needs no target_read.
func TestCTwoPass(t *testing.T) {
	memory := buildCDriver(t)
	callback := buildCIODriver(t)
	dir := t.TempDir()
	memoryDecode := func(dict, patch []byte) ([]byte, error) {
		d, p, o := filepath.Join(dir, "d"), filepath.Join(dir, "p"), filepath.Join(dir, "o")
		os.WriteFile(d, dict, 0o644)
		os.WriteFile(p, patch, 0o644)
		if msg, err := cCommand(memory, d, p, o, fmt.Sprint(cCapacity)).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("%v: %s", err, bytes.TrimSpace(msg))
		}
		return os.ReadFile(o)
	}
	for _, c := range twoPassCases(t) {
		if c.outerName == "greedy" { // an encoder variation, nothing new for a decoder
			continue
		}
		if len(c.target) > cCapacity || len(c.inner) > cCapacity {
			t.Fatalf("%s: does not fit cCapacity", c.name)
		}
		inner, err := memoryDecode(nil, c.outer)
		if err != nil || !bytes.Equal(inner, c.inner) {
			t.Fatalf("%s: memory decoder, first pass: %v", c.name, err)
		}
		if got, err := memoryDecode(c.dict, inner); err != nil || !bytes.Equal(got, c.target) {
			t.Fatalf("%s: memory decoder, second pass: %v", c.name, err)
		}
		for _, cfg := range []ioConfig{{1, 1, 0}, {1024, 256, 0}} {
			label := fmt.Sprintf("%s: callback decoder (buffer %d, page %d)", c.name, cfg.buffer, cfg.page)
			read := cfg.buffer < 1024 || c.outerName != "1 KB distance"
			inner, code, ok := runCIO(t, callback, cfg, read, nil, c.outer, cCapacity)
			if !ok || !bytes.Equal(inner, c.inner) {
				t.Fatalf("%s, first pass: code %s", label, code)
			}
			if got, code, ok := runCIO(t, callback, cfg, true, c.dict, inner, cCapacity); !ok || !bytes.Equal(got, c.target) {
				t.Fatalf("%s, second pass: code %s", label, code)
			}
		}
	}
}
