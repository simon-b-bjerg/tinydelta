// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The rule tables must relocate exactly as the hand-written rules did
// (reloc_legacy_test.go): same bytes for every image and map.
func checkSameAsLegacy(t *testing.T, what string, m *relocMap, dict []byte) {
	t.Helper()
	got, want := m.relocate(dict), legacyRelocate(m, dict)
	if !bytes.Equal(got, want) {
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s (%s): byte %d (address 0x%x): rules 0x%02x, hand-written 0x%02x; %d bytes differ", what, m.rules.name, i, m.base+uint32(i), got[i], want[i], countDiff(got, want))
			}
		}
	}
}

func countDiff(a, b []byte) int {
	n := 0
	for i := range a {
		if a[i] != b[i] {
			n++
		}
	}
	return n
}

// randomMap makes a map over n bytes: contiguous entries with shifts,
// data runs and RAM ranges (for MSP430X).
func randomMap(r *rand.Rand, rules *Rules, base uint32, n int) *relocMap {
	m := &relocMap{rules: rules, base: base}
	for pos := r.Intn(64); pos < n; {
		l := 16 + r.Intn(n/4+1)
		m.entries = append(m.entries, relocEntry{uint32(pos), uint32(min(l, n-pos)), int32(r.Intn(1200)-600) &^ 1})
		pos += l + r.Intn(3)*r.Intn(64)
	}
	if rules.builtin == RelocationMSP430X {
		for b := r.Intn(8); b < (n+relocDataBlock-1)/relocDataBlock; b += 2 + r.Intn(10) {
			m.data = append(m.data, relocRun{uint32(b), uint32(1 + r.Intn(2))})
			b += int(m.data[len(m.data)-1].length)
		}
		for a := uint32(0x1C00 + r.Intn(64)); a < 0x2400; a += uint32(16 + r.Intn(200)) {
			l := uint32(8 + r.Intn(100))
			m.outside = append(m.outside, relocEntry{a, l, int32(r.Intn(40)-20) &^ 1})
			a += l
		}
	}
	return m
}

// randomCode is an image of random words with the instruction forms the
// rules look for sprinkled in.
func randomCode(r *rand.Rand, rules *Rules, base uint32, n int) []byte {
	b := make([]byte, n)
	r.Read(b)
	for i := 0; i+8 <= n; i += 2 {
		if r.Intn(6) != 0 {
			continue
		}
		addr := base + uint32(r.Intn(n))
		if r.Intn(4) == 0 {
			addr = uint32(0x1C00 + r.Intn(0x800)) // RAM
		}
		switch rules.builtin {
		case RelocationMSP430X:
			switch r.Intn(5) {
			case 0:
				binary.LittleEndian.PutUint16(b[i:], uint16(0x13B0|addr>>16&0xF))
				binary.LittleEndian.PutUint16(b[i+2:], uint16(addr))
			case 1:
				binary.LittleEndian.PutUint16(b[i:], uint16(0x0080|uint32(r.Intn(4))<<4|(addr>>16&0xF)<<8|uint32(r.Intn(16))))
				binary.LittleEndian.PutUint16(b[i+2:], uint16(addr))
			case 2: // extension word + two-operand instruction, random modes
				binary.LittleEndian.PutUint16(b[i:], uint16(0x1800|(addr>>16&0xF)<<7|addr>>16&0xF))
				binary.LittleEndian.PutUint16(b[i+2:], uint16(0x4000+r.Intn(0xC000)))
				binary.LittleEndian.PutUint16(b[i+4:], uint16(addr))
				binary.LittleEndian.PutUint16(b[i+6:], uint16(addr+2))
			case 3: // a data pointer
				binary.LittleEndian.PutUint32(b[i:], addr)
			case 4: // small operand words that look like MOVA
				binary.LittleEndian.PutUint16(b[i:], uint16(r.Intn(0x100)))
			}
		case RelocationThumb:
			if r.Intn(2) == 0 {
				off := uint32(int32(int(addr-base)-(i+4))>>1) & 0x3FFFFF
				binary.LittleEndian.PutUint16(b[i:], uint16(0xF000|off>>11&0x7FF))
				binary.LittleEndian.PutUint16(b[i+2:], uint16(0xF800|off&0x7FF))
			} else {
				binary.LittleEndian.PutUint32(b[i&^3:], addr|1)
			}
		}
	}
	return b
}

func TestRulesSameAsHandWritten(t *testing.T) {
	r := rand.New(rand.NewSource(91))
	for _, c := range relocCases {
		rules := rulesFor(c.arch)
		// The synthetic firmware updates the other tests use, with their real maps.
		for seed := 0; seed < 6; seed++ {
			old, new := fakeFirmware(rand.New(rand.NewSource(int64(seed))), c.arch, c.base, 60+20*seed)
			m := relocMapFrom(old, new, rules, c.base).withDataRuns(old, new).withOutside(old, new)
			checkSameAsLegacy(t, fmt.Sprintf("fake firmware %d", seed), m, old)
		}
		// Random images and maps, odd and even bases, every length near the
		// end (TINYDELTA_RULES_ROUNDS for more).
		rounds := 300
		if v, err := strconv.Atoi(os.Getenv("TINYDELTA_RULES_ROUNDS")); err == nil {
			rounds = v
		}
		for k := 0; k < rounds; k++ {
			n := 64 + r.Intn(4096)
			base := c.base + uint32(r.Intn(3))*2
			if k%7 == 0 {
				base++ // an odd base: positions shift parity
			}
			img := randomCode(r, rules, base, n)
			checkSameAsLegacy(t, fmt.Sprintf("random image %d (%d B)", k, n), randomMap(r, rules, base, n), img)
		}
	}
}

// TestRulesSameAsHandWrittenOnCorpus compares on real firmware builds:
// TINYDELTA_RULES_CORPUS names a file of lines "name rule base old new"
// (rule thumb or msp430x).
func TestRulesSameAsHandWrittenOnCorpus(t *testing.T) {
	list := os.Getenv("TINYDELTA_RULES_CORPUS")
	if list == "" {
		t.Skip("TINYDELTA_RULES_CORPUS not set")
	}
	f, err := os.Open(list)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	pairs := 0
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		if len(p) != 5 {
			continue
		}
		rules, err := BuiltinRules(p[1])
		if err != nil {
			t.Fatal(err)
		}
		var base uint32
		fmt.Sscan(p[2], &base)
		old, err1 := os.ReadFile(p[3])
		new, err2 := os.ReadFile(p[4])
		if err1 != nil || err2 != nil {
			t.Fatal(err1, err2)
		}
		m := relocMapFrom(old, new, rules, base).withDataRuns(old, new).withOutside(old, new)
		checkSameAsLegacy(t, p[0], m, old)
		pairs++
	}
	t.Logf("%d pairs relocate identically", pairs)
}

func TestRulesShippedAndNamed(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	old, new := fakeFirmware(r, RelocationMSP430X, 0x10000, 400)
	custom, err := ParseRules(bytes.Replace(mustReadRule(t, "msp430x"), []byte("name: msp430x"), []byte("name: msp430x-copy"), 1))
	if err != nil {
		t.Fatal(err)
	}
	if custom.CRC() != builtinMSP430X.CRC() {
		t.Fatal("the name changed the table") // names aren't in the table
	}
	// A different table: drop the data rule.
	src := mustReadRule(t, "msp430x")
	custom, err = ParseRules(src[:bytes.Index(src, []byte("\ndata:"))+1])
	if err != nil {
		t.Fatal(err)
	}
	tables := 1287 + 64*relocEntrySize + len(custom.Table())
	named, err := EncodeWith(old, new, EncodeOptions{CoderTables: tables, Rules: custom, ImageBase: 0x10000})
	if err != nil {
		t.Fatal(err)
	}
	if named.Bytes[1]>>2&3 != byte(relocationRules) || named.Bytes[1]&codedShippedRules != 0 {
		t.Fatalf("named: features 0x%02x", named.Bytes[1])
	}
	if _, err := Decode(old, named.Bytes); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("a patch naming an unknown table: %v, want ErrUnsupportedVersion", err)
	}
	if got, err := DecodeRules(old, bytes.NewReader(named.Bytes), MaxSize, custom); err != nil || !bytes.Equal(got, new) {
		t.Fatalf("DecodeRules: %v", err)
	}
	shipped, err := EncodeWith(old, new, EncodeOptions{CoderTables: tables, Rules: custom, ShipRules: true, ImageBase: 0x10000})
	if err != nil {
		t.Fatal(err)
	}
	if shipped.Bytes[1]&codedShippedRules == 0 {
		t.Fatalf("shipped: features 0x%02x", shipped.Bytes[1])
	}
	if got, err := Decode(old, shipped.Bytes); err != nil || !bytes.Equal(got, new) {
		t.Fatalf("shipped table: %v", err)
	}
	t.Logf("named %d B, shipped %d B (table %d B)", len(named.Bytes), len(shipped.Bytes), len(custom.Table()))
	for _, p := range [][]byte{named.Bytes, shipped.Bytes} {
		if d := DescribePatch(p, custom); !strings.Contains(d, "relocation map") || !strings.Contains(d, "rule table") {
			t.Errorf("DescribePatch: %s", d)
		}
	}
	// The built-in tables keep their numbers, which the C decoders know.
	builtin, _ := EncodeWith(old, new, EncodeOptions{CoderTables: tables, Relocation: RelocationMSP430X, ImageBase: 0x10000})
	if builtin.Bytes[1]>>2&3 != byte(RelocationMSP430X) {
		t.Fatalf("built-in: features 0x%02x", builtin.Bytes[1])
	}
	if _, err := EncodeWith(old, new, EncodeOptions{Rules: custom, Relocation: RelocationThumb}); err == nil {
		t.Error("Rules and Relocation both accepted")
	}
}

func mustReadRule(t *testing.T, name string) []byte {
	t.Helper()
	b, err := builtinRuleFiles.ReadFile("rules/" + name + ".yaml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRulesTable(t *testing.T) {
	for _, rules := range []*Rules{builtinThumb, builtinMSP430X} {
		again, err := ParseRulesTable(rules.Table())
		if err != nil || !bytes.Equal(again.Table(), rules.Table()) || again.CRC() != rules.CRC() {
			t.Fatalf("%s: table round trip: %v", rules.Name(), err)
		}
		t.Logf("%s: %d-byte table, CRC %04x", rules.Name(), len(rules.Table()), rules.CRC())
		// Every truncation and many corruptions are rejected or yield a
		// valid table that the engine runs without panicking.
		table := rules.Table()
		for cut := 0; cut < len(table); cut++ {
			if _, err := ParseRulesTable(table[:cut]); err == nil {
				t.Errorf("%s: table cut to %d of %d bytes accepted", rules.Name(), cut, len(table))
			}
		}
		r := rand.New(rand.NewSource(3))
		img := randomCode(r, rules, 0x10000, 2048)
		for k := 0; k < 3000; k++ {
			bad := append([]byte{}, table...)
			for j := 0; j <= r.Intn(3); j++ {
				bad[r.Intn(len(bad))] = byte(r.Intn(256))
			}
			if got, err := ParseRulesTable(bad); err == nil {
				m := randomMap(r, rules, 0x10000, len(img))
				m.rules = got
				m.relocate(img) // must not panic or index out of range
			}
		}
	}
}

func TestRulesYAMLErrors(t *testing.T) {
	head := "version: 1\nname: t\nunit: 2\nword: 16\nendian: little\nsync: 0\n"
	cases := map[string]string{
		"unknown key":          head + "colour: red\n",
		"unknown rule key":     head + "code:\n  - name: a\n    match: [\"0000/F000\"]\n    adress: [\"w1[15:0]\"]\n",
		"bad condition":        head + "code:\n  - name: a\n    match: [\"13B/FFF0\"]\n",
		"value outside mask":   head + "code:\n  - name: a\n    match: [\"13B1/FFF0\"]\n",
		"bad slice":            head + "code:\n  - name: a\n    match: [\"13B0/FFF0\"]\n    address: [\"w1[16:0]\"]\n",
		"too wide":             head + "code:\n  - name: a\n    match: [\"13B0/FFF0\"]\n    address: [\"w1[15:0]\", \"w2[15:0]\", \"w3[1:0]\"]\n",
		"unknown unless rule":  head + "code:\n  - name: a\n    match: [\"13B0/FFF0\"]\n    address: [\"w1[15:0]\"]\n    unless: [{ rule: b, at: [2] }]\n",
		"tab":                  head + "code:\n\t- name: a\n",
		"anchor":               head + "code: &x\n",
		"multi-line string":    head + "name2: \"abc\n",
		"duplicate key":        head + "sync: 1\n",
		"sync too deep":        strings.Replace(head, "sync: 0", "sync: 17", 1),
		"relative two parts":   head + "code:\n  - name: a\n    match: [\"13B0/FFF0\"]\n    address: [\"w1[15:0]\"]\n    address2: [\"w2[15:0]\"]\n    relative: { base: 4, sign_extend: 16 }\n",
		"require on relative":  head + "code:\n  - name: a\n    match: [\"13B0/FFF0\"]\n    address: [\"w1[15:0]\"]\n    relative: { base: 4, sign_extend: 16 }\n    require: in-image\n",
		"unaligned unless":     head + "code:\n  - name: a\n    match: [\"13B0/FFF0\"]\n    address: [\"w1[15:0]\"]\n    unless: [{ rule: self, at: [1] }]\n",
		"length in code rule":  head + "code:\n  - name: a\n    match: [\"13B0/FFF0\"]\n    length: 4\n",
		"comment eats a value": head + "code:\n  - name: a #1\n",
	}
	for what, src := range cases {
		if _, err := ParseRules([]byte(src)); err == nil {
			t.Errorf("%s: accepted", what)
		} else if what == "unknown rule key" && !strings.Contains(err.Error(), "line 10") {
			t.Errorf("%s: %v: want the line", what, err)
		}
	}
	// A quoted name with # is fine, and flow mappings with lists inside.
	ok := head + "code:\n  - name: \"CALLA #imm20\"   # a comment\n    match: [\"13B0/FFF0\"]\n    address: [\"w0[3:0]\", 'w1[15:0]']\n    unless: [{ rule: self, at: [-2, 2] }]\n"
	if r, err := ParseRules([]byte(ok)); err != nil || r.lists[ruleCode][0].name != "CALLA #imm20" {
		t.Errorf("valid file: %v", err)
	}
}

// TestRulesCHeaders keeps the C library's built-in tables and the CRCs it
// maps features values 1 and 2 to in step with rules/*.yaml.
func TestRulesCHeaders(t *testing.T) {
	src, err := os.ReadFile("c/tinydelta_reloc.c")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		rules *Rules
		file  string
		crc   string
	}{{builtinThumb, "thumb", "RULES_THUMB_CRC"}, {builtinMSP430X, "msp430x", "RULES_MSP430X_CRC"}} {
		got, err := os.ReadFile("c/tinydelta_rules_" + c.file + ".h")
		if want := c.rules.CHeader("rules/" + c.file + ".yaml"); err != nil || string(got) != want {
			t.Errorf("c/tinydelta_rules_%s.h is out of date: tinydelta rules compile -c c/tinydelta_rules_%s.h rules/%s.yaml", c.file, c.file, c.file)
		}
		if def := fmt.Sprintf("#define %-17s 0x%04Xu", c.crc, c.rules.CRC()); !strings.Contains(string(src), def) {
			t.Errorf("c/tinydelta_reloc.c lacks %q", def)
		}
	}
}

// TestCRelocationRules decodes, with the C decoders, patches that ship
// their rule table and patches that name a table of one's own built in,
// and compares corrupted ones with Go.
func TestCRelocationRules(t *testing.T) {
	src := mustReadRule(t, "msp430x")
	custom, err := ParseRules(src[:bytes.Index(src, []byte("\ndata:"))+1]) // without the data rule
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	header := filepath.Join(dir, "custom_rules.h")
	os.WriteFile(header, []byte(custom.CHeader("custom.yaml")), 0o644)
	build := func(defs ...string) (memory, callback string) {
		memory = buildCDriver(t, append(append(append([]string{}, cCoderSources...), defs...), "c/tinydelta_reloc.c")...)
		d := t.TempDir()
		os.WriteFile(filepath.Join(d, "driver.c"), []byte(cIODriver), 0o644)
		callback = filepath.Join(d, "decode_io")
		cCompile(t, callback, append(append(append([]string{}, cCoderSources...), defs...), "c/tinydelta_reloc.c", "c/tinydelta_io.c", filepath.Join(d, "driver.c"))...)
		return memory, callback
	}
	decode := func(bin string, dict, patch []byte) ([]byte, string, bool) {
		d := t.TempDir()
		dp, pp, op := filepath.Join(d, "d"), filepath.Join(d, "p"), filepath.Join(d, "o")
		os.WriteFile(dp, dict, 0o644)
		os.WriteFile(pp, patch, 0o644)
		msg, err := cCommand(bin, dp, pp, op, fmt.Sprint(cCapacity)).CombinedOutput()
		if err != nil {
			return nil, string(bytes.TrimSpace(msg)), false
		}
		out, _ := os.ReadFile(op)
		return out, "", true
	}
	shippedMem, shippedIO := build("-DTINYDELTA_RELOC=2", "-DTINYDELTA_RELOC_SHIPPED=1")
	ownMem, ownIO := build("-DTINYDELTA_RELOC=3", `-DTINYDELTA_RELOC_RULES_HEADER="`+header+`"`)
	plainMem, _ := build("-DTINYDELTA_RELOC=2")

	r := rand.New(rand.NewSource(77))
	old, new := fakeFirmware(r, RelocationMSP430X, 0x10000, 45)
	tables := CodedTablesSize(codedLayoutA) + 64*relocEntrySize
	var shipped, named [][]byte
	for _, layout := range []int{codedLayoutA, codedLayoutB, codedLayoutC} {
		m := relocMapFrom(old, new, custom, 0x10000).withOutside(old, new)
		rd := m.relocate(old)
		v1, _ := Encode(rd, new)
		named = append(named, encodeCodedReloc(v1, layout, m))
		ms := m.with(m.entries, m.data, m.outside)
		ms.ship = true
		shipped = append(shipped, encodeCodedReloc(v1, layout, ms))
	}
	for i := range shipped {
		if got, err := Decode(old, shipped[i]); err != nil || !bytes.Equal(got, new) {
			t.Fatalf("Go, shipped %d: %v", i, err)
		}
		if got, code, ok := decode(shippedMem, old, shipped[i]); !ok || !bytes.Equal(got, new) {
			t.Fatalf("shipped %d: memory decoder: %s", i, code)
		}
		for _, cfg := range []ioConfig{{1, 1, tables}, {16, 4, tables}, {256, 256, tables}} {
			if got, code, ok := runCIO(t, shippedIO, cfg, true, old, shipped[i], cCapacity); !ok || !bytes.Equal(got, new) {
				t.Fatalf("shipped %d %+v: callback decoder: %s", i, cfg, code)
			}
		}
		if _, code, ok := decode(plainMem, old, shipped[i]); ok || code != "-4" {
			t.Errorf("shipped %d without TINYDELTA_RELOC_SHIPPED: ok=%v %s, want -4", i, ok, code)
		}
		if _, code, ok := decode(shippedMem, old, named[i]); ok || code != "-4" {
			t.Errorf("named %d, built-in msp430x: ok=%v %s, want -4", i, ok, code)
		}
		if got, code, ok := decode(ownMem, old, named[i]); !ok || !bytes.Equal(got, new) {
			t.Fatalf("named %d, own table built in: memory decoder: %s", i, code)
		}
		if got, code, ok := runCIO(t, ownIO, ioConfig{16, 4, tables}, true, old, named[i], cCapacity); !ok || !bytes.Equal(got, new) {
			t.Fatalf("named %d, own table built in: callback decoder: %s", i, code)
		}
	}
	// Corrupted shipped patches, in the header and in the coded table: C
	// accepts and rejects as Go, with the same output.
	patch := shipped[0]
	for k := 0; k < 300; k++ {
		bad := append([]byte{}, patch...)
		if k%3 == 0 {
			bad = bad[:r.Intn(len(bad))]
		} else {
			bad[1+r.Intn(min(len(bad)-1, 60))] ^= byte(1 + r.Intn(255))
		}
		want, werr := Decode(old, bad)
		got, code, ok := decode(shippedMem, old, bad)
		if ok != (werr == nil) || (ok && !bytes.Equal(got, want)) {
			t.Fatalf("corruption %d: memory decoder ok=%v (%s), Go err=%v", k, ok, code, werr)
		}
		got, code, ok = runCIO(t, shippedIO, ioConfig{16, 4, tables}, true, old, bad, cCapacity)
		if ok != (werr == nil) || (ok && !bytes.Equal(got, want)) {
			t.Fatalf("corruption %d: callback decoder ok=%v (%s), Go err=%v", k, ok, code, werr)
		}
	}
}
