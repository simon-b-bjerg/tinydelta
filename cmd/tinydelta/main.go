// Command tinydelta creates and applies tinydelta patches.
//
//	tinydelta encode [-greedy] [-for-compression] [-max-len N] [-max-distance N] [-v] <dictionary> <target> [<patch>]
//	tinydelta decode [-max-size N] [-v] <dictionary> <patch> [<target>]
//	tinydelta version
//
// An omitted output file, or "-", means standard output; a patch of "-"
// is read from standard input.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/simon-b-bjerg/tinydelta"
)

// version is set for release builds with -ldflags "-X main.version=v1.2.3".
var version = ""

const usageText = `tinydelta creates and applies compact binary patches.

Usage:
  tinydelta encode [-greedy] [-for-compression] [-max-len N] [-max-distance N] [-v] <dictionary> <target> [<patch>]
  tinydelta decode [-max-size N] [-v] <dictionary> <patch> [<target>]
  tinydelta version

encode writes a patch that turns <dictionary> (the old file) into
<target> (the new file). decode applies it. An omitted output file, or
"-", means standard output; a <patch> of "-" is read from standard input.
Flags go before the file names. Run "tinydelta <command> -h" for a
command's flags.
`

// Exit codes.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return exitUsage
	}
	switch args[0] {
	case "encode":
		return encodeCommand(args[1:], stdout, stderr)
	case "decode":
		return decodeCommand(args[1:], stdin, stdout, stderr)
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, "tinydelta", versionString())
		return exitOK
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usageText)
		return exitOK
	default:
		fmt.Fprintf(stderr, "tinydelta: unknown command %q\n\n%s", args[0], usageText)
		return exitUsage
	}
}

func encodeCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	fs.SetOutput(stderr)
	greedy := fs.Bool("greedy", false, "use the fast greedy encoder instead of the optimal one\n(patches are typically a few percent larger)")
	forCompression := fs.Bool("for-compression", false, "make a patch to encode again against an empty dictionary\n(compression mode, see the README); may be format version 2")
	maxDistance := fs.Int("max-distance", 0, "limit how far back a copy from the target may read, so a decoder\nonly needs that many bytes of recent output (0 means no limit)")
	maxLen := fs.Int("max-len", 0, "limit the target bytes any one instruction produces\n(0 means no limit)")
	verbose := fs.Bool("v", false, "print sizes to standard error")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tinydelta encode [-greedy] [-for-compression] [-max-len N] [-max-distance N] [-v] <dictionary> <target> [<patch>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}
	if fs.NArg() < 2 || fs.NArg() > 3 || *maxLen < 0 {
		fs.Usage()
		return exitUsage
	}

	dict, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return fail(stderr, err)
	}
	target, err := os.ReadFile(fs.Arg(1))
	if err != nil {
		return fail(stderr, err)
	}
	patch, err := tinydelta.EncodeWith(dict, target, tinydelta.EncodeOptions{Greedy: *greedy, ForCompression: *forCompression, MaxInstructionLen: *maxLen, MaxTargetDistance: *maxDistance})
	if err != nil {
		return fail(stderr, err)
	}
	if err := writeOutput(outputName(fs, 2), patch, stdout); err != nil {
		return fail(stderr, err)
	}
	if *verbose {
		fmt.Fprintf(stderr, "dictionary %d B, target %d B -> patch %d B (%s of target)\n",
			len(dict), len(target), len(patch), percent(len(patch), len(target)))
	}
	return exitOK
}

func decodeCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("decode", flag.ContinueOnError)
	fs.SetOutput(stderr)
	maxSize := fs.Int("max-size", tinydelta.MaxSize, "fail if the target would exceed this many bytes")
	verbose := fs.Bool("v", false, "print sizes to standard error")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tinydelta decode [-max-size N] [-v] <dictionary> <patch> [<target>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}
	if fs.NArg() < 2 || fs.NArg() > 3 {
		fs.Usage()
		return exitUsage
	}

	dict, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return fail(stderr, err)
	}
	patch := stdin
	if name := fs.Arg(1); name != "-" {
		f, err := os.Open(name)
		if err != nil {
			return fail(stderr, err)
		}
		defer f.Close()
		patch = f
	}
	target, err := tinydelta.DecodeReaderLimit(dict, patch, *maxSize)
	if err != nil {
		return fail(stderr, err)
	}
	if err := writeOutput(outputName(fs, 2), target, stdout); err != nil {
		return fail(stderr, err)
	}
	if *verbose {
		fmt.Fprintf(stderr, "dictionary %d B -> target %d B\n", len(dict), len(target))
	}
	return exitOK
}

// outputName returns the optional output argument at index i, or "-".
func outputName(fs *flag.FlagSet, i int) string {
	if fs.NArg() > i {
		return fs.Arg(i)
	}
	return "-"
}

func writeOutput(name string, data []byte, stdout io.Writer) error {
	if name == "-" {
		_, err := stdout.Write(data)
		return err
	}
	return os.WriteFile(name, data, 0o644)
}

func parseExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	return exitUsage
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, "tinydelta:", err)
	return exitFailure
}

func percent(part, whole int) string {
	if whole == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(part)/float64(whole))
}

// versionString is the release version, else the module version when
// installed with "go install ...@vX.Y.Z", else "dev".
func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
