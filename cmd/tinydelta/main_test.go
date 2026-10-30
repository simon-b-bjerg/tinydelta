package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type result struct {
	code           int
	stdout, stderr string
}

func runCLI(stdin []byte, args ...string) result {
	var out, errOut bytes.Buffer
	code := run(args, bytes.NewReader(stdin), &out, &errOut)
	return result{code, out.String(), errOut.String()}
}

func writeFiles(t *testing.T) (dir, dict, target string) {
	t.Helper()
	dir = t.TempDir()
	dict = filepath.Join(dir, "old.bin")
	target = filepath.Join(dir, "new.bin")
	os.WriteFile(dict, []byte("the quick brown fox jumps over the lazy dog"), 0o644)
	os.WriteFile(target, []byte("the quick BROWN fox jumps over the lazy dog!"), 0o644)
	return dir, dict, target
}

func TestEncodeDecodeFiles(t *testing.T) {
	for _, flags := range [][]string{nil, {"-greedy"}, {"-max-len", "3"}, {"-greedy", "-max-len", "3"}} {
		dir, dict, target := writeFiles(t)
		patch := filepath.Join(dir, "patch.td")
		out := filepath.Join(dir, "out.bin")

		args := append(append([]string{"encode"}, flags...), "-v", dict, target, patch)
		if r := runCLI(nil, args...); r.code != exitOK || !strings.Contains(r.stderr, "-> patch") {
			t.Fatalf("encode %v: %+v", flags, r)
		}
		if r := runCLI(nil, "decode", "-v", dict, patch, out); r.code != exitOK {
			t.Fatalf("decode: %+v", r)
		}
		want, _ := os.ReadFile(target)
		got, _ := os.ReadFile(out)
		if !bytes.Equal(got, want) {
			t.Fatalf("round trip with %v: got %q, want %q", flags, got, want)
		}
	}
}

// TestEncodeDecodePipes checks the "-" (and omitted) stdin/stdout forms.
func TestEncodeDecodePipes(t *testing.T) {
	_, dict, target := writeFiles(t)
	enc := runCLI(nil, "encode", dict, target)
	if enc.code != exitOK || enc.stdout == "" {
		t.Fatalf("encode to stdout: %+v", enc)
	}
	dec := runCLI([]byte(enc.stdout), "decode", dict, "-", "-")
	want, _ := os.ReadFile(target)
	if dec.code != exitOK || dec.stdout != string(want) {
		t.Fatalf("decode from stdin to stdout: %+v", dec)
	}
}

func TestDecodeMaxSize(t *testing.T) {
	_, dict, target := writeFiles(t)
	patch := runCLI(nil, "encode", dict, target).stdout
	want, _ := os.ReadFile(target)
	// The Python package (python/src/tinydelta) tells TooLargeError apart
	// from other failures by this text, so it must not change.
	r := runCLI([]byte(patch), "decode", "-max-size", "10", dict, "-")
	if r.code != exitFailure || !strings.Contains(r.stderr, "size limit exceeded") {
		t.Errorf("decode with -max-size 10: %+v", r)
	}
	if r := runCLI([]byte(patch), "decode", "-max-size", itoa(len(want)), dict, "-"); r.code != exitOK {
		t.Errorf("decode with -max-size exactly the target size: %+v", r)
	}
}

func TestErrorsAndUsage(t *testing.T) {
	_, dict, _ := writeFiles(t)
	cases := []struct {
		name  string
		stdin []byte
		args  []string
		code  int
	}{
		{"no command", nil, nil, exitUsage},
		{"unknown command", nil, []string{"frobnicate"}, exitUsage},
		{"encode, too few files", nil, []string{"encode", dict}, exitUsage},
		{"decode, too many files", nil, []string{"decode", dict, "a", "b", "c"}, exitUsage},
		{"negative max-len", nil, []string{"encode", "-max-len", "-1", dict, dict}, exitUsage},
		{"unknown flag", nil, []string{"encode", "-nope", dict, dict}, exitUsage},
		{"missing input file", nil, []string{"encode", dict, "/nonexistent/file"}, exitFailure},
		{"corrupt patch", []byte{0x80}, []string{"decode", dict, "-"}, exitFailure},
		{"help", nil, []string{"help"}, exitOK},
		{"command help", nil, []string{"encode", "-h"}, exitOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if r := runCLI(c.stdin, c.args...); r.code != c.code {
				t.Errorf("exit %d, want %d (stderr %q)", r.code, c.code, r.stderr)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	r := runCLI(nil, "version")
	if r.code != exitOK || !strings.HasPrefix(r.stdout, "tinydelta ") {
		t.Fatalf("version: %+v", r)
	}
	version = "v9.9.9"
	defer func() { version = "" }()
	if r := runCLI(nil, "--version"); strings.TrimSpace(r.stdout) != "tinydelta v9.9.9" {
		t.Errorf("version with -ldflags-style version set: %q", r.stdout)
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
