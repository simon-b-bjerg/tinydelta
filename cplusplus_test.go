// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

package tinydelta

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCHeadersFromCPlusPlus checks that C++ firmware can include the C
// headers and link against the decoders built as C.
func TestCHeadersFromCPlusPlus(t *testing.T) {
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("no C++ compiler")
	}
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler")
	}
	const source = `#include "tinydelta_io.h"
int main() {
    uint8_t byte = 0;
    uint32_t length = 0;
    tinydelta_io_t io = {};
    tinydelta_result_t coded = tinydelta_decode_coded(&byte, 0, &byte, 0, &byte, 0, &byte, 0, &length);
    tinydelta_result_t plain = tinydelta_decode(&byte, 0, &byte, 0, &byte, 0, &length);
    return (coded == plain) && (tinydelta_decode_io(&io, &length) != TINYDELTA_OK) ? 0 : 1;
}
`
	dir := t.TempDir()
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"-Ic", "-DTINYDELTA_CODER=1", main}
	for _, name := range []string{"tinydelta_decode", "tinydelta_io", "tinydelta_coder"} {
		object := filepath.Join(dir, name+".o")
		if out, err := exec.Command(cc, "-std=c99", "-Ic", "-DTINYDELTA_CODER=1", "-c", filepath.Join("c", name+".c"), "-o", object).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		args = append(args, object)
	}
	program := filepath.Join(dir, "main")
	if out, err := exec.Command(cxx, append(args, "-o", program)...).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if out, err := exec.Command(program).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}
