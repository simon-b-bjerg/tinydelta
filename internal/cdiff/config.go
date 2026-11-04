// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package cdiff

// IOConfig sets up the callback decoder (tinydelta_decode_io): its RAM
// buffer and page size (powers of two, the page no larger than the
// buffer), and FailAt, the callback call to fail (0: none).
type IOConfig struct {
	BufferSize, PageSize int
	FailAt               int
}

// OK is TINYDELTA_OK, the C decoders' result on success.
const OK = 0
