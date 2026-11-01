// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"bufio"
	"bytes"
	"math/rand"
	"testing"
)

func TestAppendVarint(t *testing.T) {
	cases := []struct {
		v    uint64
		want []byte
	}{
		{0, []byte{0x00}},
		{127, []byte{0x7f}},
		{128, []byte{0x80, 0x01}},
		{300, []byte{0xAC, 0x02}},                         // 300 = 0b1_0010_1100 -> low7=0101100|cont, hi=10
		{1<<32 - 1, []byte{0xff, 0xff, 0xff, 0xff, 0x0f}}, // the format's maximum
	}
	for _, c := range cases {
		got := appendVarint([]byte{0xAA}, c.v)
		if string(got[1:]) != string(c.want) || got[0] != 0xAA {
			t.Errorf("appendVarint(%d) = % x, want % x", c.v, got[1:], c.want)
		}
		if n := varintLen(c.v); n != len(c.want) {
			t.Errorf("varintLen(%d) = %d, want %d", c.v, n, len(c.want))
		}
		v, n := readVarint(c.want)
		if v != c.v || n != len(c.want) {
			t.Errorf("readVarint(% x) = %d, %d bytes; want %d, %d bytes", c.want, v, n, c.v, len(c.want))
		}
	}
}

func TestVarintRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 10000; i++ {
		v := uint64(r.Uint32()) >> uint(r.Intn(32))
		b := appendVarint(nil, v)
		got, n := readVarint(b)
		if got != v || n != len(b) || n != varintLen(v) {
			t.Fatalf("v=%d: decoded %d using %d bytes, encoded len %d, varintLen %d",
				v, got, n, len(b), varintLen(v))
		}
	}
}

func TestReadVarintTruncated(t *testing.T) {
	cases := [][]byte{
		nil,
		{0x80},       // continuation set, nothing follows
		{0x80, 0x80}, // still continuing
	}
	for _, b := range cases {
		if _, n := readVarint(b); n != -1 {
			t.Errorf("readVarint(% x) = n=%d, want -1", b, n)
		}
	}
}

func TestZigzagRoundTrip(t *testing.T) {
	cases := []struct {
		v    int64
		want uint64
	}{
		{0, 0}, {-1, 1}, {1, 2}, {-2, 3}, {2, 4}, {-64, 127}, {64, 128},
	}
	for _, c := range cases {
		if got := zigzagEncode(c.v); got != c.want {
			t.Errorf("zigzagEncode(%d) = %d, want %d", c.v, got, c.want)
		}
		if got := zigzagDecode(c.want); got != c.v {
			t.Errorf("zigzagDecode(%d) = %d, want %d", c.want, got, c.v)
		}
	}
	r := rand.New(rand.NewSource(5))
	for i := 0; i < 10000; i++ {
		v := int64(r.Uint64())
		if got := zigzagDecode(zigzagEncode(v)); got != v {
			t.Fatalf("zigzagDecode(zigzagEncode(%d)) = %d", v, got)
		}
	}
}

// TestVarint32BitLimit pins down the format's 32-bit limit on every
// varint: the largest value round-trips in maxVarintLen bytes, and both
// readers reject a 5th byte carrying more than bits 28-31, or a 6th byte.
func TestVarint32BitLimit(t *testing.T) {
	max := appendVarint(nil, 1<<32-1)
	if len(max) != maxVarintLen {
		t.Fatalf("2^32-1 encodes in %d bytes, want %d", len(max), maxVarintLen)
	}
	for _, c := range []struct {
		name string
		b    []byte
		ok   bool
	}{
		{"2^32-1", max, true},
		{"5th byte over bit 31", []byte{0xff, 0xff, 0xff, 0xff, 0x10}, false},
		{"6 bytes", []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x00}, false},
		{"2^64-1 (10 bytes)", appendVarint(nil, 1<<64-1), false},
	} {
		_, n := readVarint(c.b)
		if (n > 0) != c.ok {
			t.Errorf("readVarint(%s): n=%d, want ok=%v", c.name, n, c.ok)
		}
		_, err := readVarintFrom(bufio.NewReader(bytes.NewReader(c.b)))
		if (err == nil) != c.ok {
			t.Errorf("readVarintFrom(%s): err=%v, want ok=%v", c.name, err, c.ok)
		}
	}
}
