// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

//go:build !cgo

package cdiff

// Available reports whether the C decoders are linked in (they need cgo).
const Available = false

// Decode and DecodeIO are not available without cgo.
func Decode(build Build, dict, patch []byte, capacity, tablesSize int) ([]byte, int) {
	panic("cdiff: built without cgo")
}

// DecodeIO is not available without cgo.
func DecodeIO(build Build, dict, patch []byte, capacity, tablesSize int, cfg IOConfig) ([]byte, int, string) {
	panic("cdiff: built without cgo")
}
