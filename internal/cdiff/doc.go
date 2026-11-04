// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

// Package cdiff links the C library (c/) into Go tests through cgo, so the
// Go fuzzers can compare the Go and C decoders in the same process. It is
// built only with cgo; without it the package is empty and Available is
// false.
package cdiff
