// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package cdiff

// Build picks one of the two builds of the C library linked in.
type Build int

const (
	// MSP430X: the MSP430X rules built in, shipped tables accepted.
	MSP430X Build = 0
	// Thumb: the Thumb rules built in, no shipped tables (thumb.h).
	Thumb Build = 1
)

// IOConfig sets up the callback decoder (tinydelta_decode_io): its RAM
// buffer and page size (powers of two, the page no larger than the
// buffer), and FailAt, the callback call to fail (0: none).
type IOConfig struct {
	BufferSize, PageSize int
	FailAt               int
}

// OK is TINYDELTA_OK, the C decoders' result on success.
const OK = 0
