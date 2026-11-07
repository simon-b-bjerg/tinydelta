// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

// Package cdiff links the C library (c/) into Go tests through cgo, so the
// Go fuzzers can compare the Go and C decoders in the same process. It
// links three builds (Build): MSP430X built in with shipped tables accepted
// (the table interpreted), and Thumb or MSP430X built in only, as a device
// has it (the rules matched by code generated with the table). It is
// built only with cgo; without it the package is empty and Available is
// false.
//
// The C files here #include the library's sources from c/, which Go's build
// cache doesn't track: after changing them, run go clean -cache (or go test
// -a) before trusting a local result. CI builds from scratch.
package cdiff
