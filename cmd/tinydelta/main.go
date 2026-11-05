// SPDX-FileCopyrightText: 2026 Simon Bjerg
// SPDX-License-Identifier: MIT

// Command tinydelta creates and applies tinydelta patches.
//
//	tinydelta encode [-greedy] [-coder-tables N] [-window N] [-two-pass] [-reloc thumb|msp430x | -rules FILE [-ship-rules] -base ADDR] [-max-len N] [-v] <dictionary> <target> [<patch>]
//	tinydelta decode [-two-pass] [-max-size N] [-rules FILE] [-v] <dictionary> <patch> [<target>]
//	tinydelta relocstats [-reloc thumb|msp430x | -rules FILE] -base ADDR [-v] <old image> <new image>
//	tinydelta rules check|compile [-o table.bin] [-c rules.h] <rules.yaml>
//	tinydelta version
//
// An omitted output file, or "-", means standard output; a patch of "-"
// is read from standard input.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/simon-b-bjerg/tinydelta"
)

// version is set for release builds with -ldflags "-X main.version=v1.2.3".
var version = ""

// copyrightText follows the version line of "tinydelta version". The
// version stays alone on the first line so scripts can read just that.
const copyrightText = `Copyright (c) 2026 Simon Bjerg
MIT License: https://github.com/simon-b-bjerg/tinydelta/blob/main/LICENSE
`

const usageText = `tinydelta creates and applies compact binary patches.

Usage:
  tinydelta encode [-greedy] [-coder-tables N] [-window N] [-two-pass] [-reloc thumb|msp430x | -rules FILE [-ship-rules] -base ADDR] [-max-len N] [-v] <dictionary> <target> [<patch>]
  tinydelta decode [-two-pass] [-max-size N] [-rules FILE] [-v] <dictionary> <patch> [<target>]
  tinydelta relocstats [-reloc thumb|msp430x | -rules FILE] -base ADDR [-v] <old image> <new image>
  tinydelta rules check|compile [-o table.bin] [-c rules.h] <rules.yaml>
  tinydelta version

encode writes a patch that turns <dictionary> (the old file) into
<target> (the new file). decode applies it. An omitted output file, or
"-", means standard output; a <patch> of "-" is read from standard input.
relocstats shows what relocation leaves to the patch of a firmware
update (experimental); rules checks or compiles a relocation rule file
(docs/relocation-rules.md). Flags go before the file names. Run
"tinydelta <command> -h" for a command's flags.
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
	case "rules":
		return rulesCommand(args[1:], stdout, stderr)
	case "relocstats":
		return relocstatsCommand(args[1:], stdout, stderr)
	case "decode":
		return decodeCommand(args[1:], stdin, stdout, stderr)
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, "tinydelta", versionString())
		fmt.Fprint(stdout, copyrightText)
		return exitOK
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usageText)
		return exitOK
	default:
		fmt.Fprintf(stderr, "tinydelta: unknown command %q\n\n%s", args[0], usageText)
		return exitUsage
	}
}

// relocstatsCommand prints what relocation leaves to the patch of an
// update (experimental; see tinydelta.RelocationStats).
func relocstatsCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("relocstats", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reloc := fs.String("reloc", "msp430x", "instruction set: thumb or msp430x (built-in rule tables)")
	rulesFile := fs.String("rules", "", "a relocation rule file (YAML) instead of -reloc")
	base := fs.String("base", "0", "the address the image starts at on the device")
	verbose := fs.Bool("v", false, "also list the map and every address field left wrong (numbers only)")
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "Usage: tinydelta relocstats [-reloc thumb|msp430x | -rules FILE] -base ADDR [-v] <old image> <new image>")
		return exitUsage
	}
	rules, code := loadRules(*rulesFile, *reloc, stderr)
	if rules == nil {
		return code
	}
	imageBase, err := strconv.ParseUint(*base, 0, 32)
	if err != nil {
		fmt.Fprintln(stderr, "tinydelta: relocstats: -base ADDR, e.g. 0x10000")
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
	if *verbose {
		fmt.Fprint(stdout, tinydelta.RelocationStatsVerbose(dict, target, rules, uint32(imageBase)))
	} else {
		fmt.Fprint(stdout, tinydelta.RelocationStats(dict, target, rules, uint32(imageBase)))
	}
	return 0
}

func encodeCommand(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("encode", flag.ContinueOnError)
	fs.SetOutput(stderr)
	greedy := fs.Bool("greedy", false, "use the fast greedy encoder instead of the optimal one\n(patches are typically a few percent larger)")
	coderTables := fs.Int("coder-tables", 0, "the decoder's coder tables buffer, in bytes (C: tables_size): allows a\ncoded patch whose tables fit (522, 777 or 1287 bytes; 0: no coder)")
	window := fs.Int("window", 0, "keep copies from the target already produced within this many bytes, so\nthe decoder reads them from its RAM buffer only (C: buffer_size, with\ntarget_read NULL; 0 means no limit)")
	twoPass := fs.Bool("two-pass", false, "allow a patch the device decodes twice (decode -two-pass); prints the\nnumber of passes, 1 or 2, to standard error")
	reloc := fs.String("reloc", "", "firmware images: the decoder relocates addresses for this instruction set\n(C: TINYDELTA_RELOC): thumb (ARM Cortex-M) or msp430x (large code model);\nneeds -coder-tables, with 12 bytes more per map entry")
	rulesFile := fs.String("rules", "", "firmware images: a relocation rule file (YAML; docs/relocation-rules.md)\ninstead of -reloc; the patch names the table by CRC, so the decoder must\nhave it built in, unless -ship-rules")
	shipRules := fs.Bool("ship-rules", false, "with -rules or -reloc: put the rule table in the patch (experimental)")
	base := fs.String("base", "0", "with -reloc or -rules: the address the image starts at on the device, e.g. 0x10000")
	maxLen := fs.Int("max-len", 0, "limit the target bytes any one instruction produces\n(0 means no limit)")
	verbose := fs.Bool("v", false, "print sizes to standard error")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tinydelta encode [-greedy] [-coder-tables N] [-window N] [-two-pass] [-reloc thumb|msp430x | -rules FILE [-ship-rules] -base ADDR] [-max-len N] [-v] <dictionary> <target> [<patch>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}
	if fs.NArg() < 2 || fs.NArg() > 3 || *maxLen < 0 || misplacedFlag(fs) {
		fs.Usage()
		return exitUsage
	}
	relocation := tinydelta.RelocationNone
	switch *reloc {
	case "":
	case "thumb":
		relocation = tinydelta.RelocationThumb
	case "msp430x":
		relocation = tinydelta.RelocationMSP430X
	default:
		fmt.Fprintf(stderr, "tinydelta: -reloc %q: want thumb or msp430x\n", *reloc)
		return exitUsage
	}
	imageBase, err := strconv.ParseUint(*base, 0, 32)
	if err != nil {
		fmt.Fprintf(stderr, "tinydelta: -base %q: want an address such as 0x10000\n", *base)
		return exitUsage
	}
	var rules *tinydelta.Rules
	if *rulesFile != "" {
		if *reloc != "" {
			fmt.Fprintln(stderr, "tinydelta: -reloc and -rules: give one")
			return exitUsage
		}
		var code int
		if rules, code = loadRules(*rulesFile, "", stderr); rules == nil {
			return code
		}
	}

	dict, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return fail(stderr, err)
	}
	target, err := os.ReadFile(fs.Arg(1))
	if err != nil {
		return fail(stderr, err)
	}
	patch, err := tinydelta.EncodeWith(dict, target, tinydelta.EncodeOptions{
		Greedy: *greedy, CoderTables: *coderTables, Window: *window, TwoPass: *twoPass, MaxInstructionLen: *maxLen,
		Relocation: relocation, ImageBase: uint32(imageBase), Rules: rules, ShipRules: *shipRules,
	})
	if err != nil {
		return fail(stderr, err)
	}
	if err := writeOutput(outputName(fs, 2), patch.Bytes, stdout); err != nil {
		return fail(stderr, err)
	}
	if *twoPass {
		fmt.Fprintf(stderr, "passes %d\n", patch.Passes)
	}
	if *verbose {
		fmt.Fprintf(stderr, "dictionary %d B, target %d B -> patch %d B (%s of target)\n",
			len(dict), len(target), len(patch.Bytes), percent(len(patch.Bytes), len(target)))
		fmt.Fprintln(stderr, tinydelta.DescribePatch(patch.Bytes, rules))
	}
	return exitOK
}

func decodeCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("decode", flag.ContinueOnError)
	fs.SetOutput(stderr)
	maxSize := fs.Int("max-size", tinydelta.MaxSize, "fail if the target would exceed this many bytes")
	twoPass := fs.Bool("two-pass", false, "the patch takes two passes (encode -two-pass printed \"passes 2\")")
	verbose := fs.Bool("v", false, "print sizes to standard error")
	rulesFile := fs.String("rules", "", "the relocation rule file (YAML) a patch made with encode -rules names")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tinydelta decode [-two-pass] [-max-size N] [-rules FILE] [-v] <dictionary> <patch> [<target>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return parseExit(err)
	}
	if fs.NArg() < 2 || fs.NArg() > 3 || *maxSize < 1 || misplacedFlag(fs) {
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
	if *twoPass { // first pass: the patch against an empty dictionary gives the patch for the second
		inner, err := tinydelta.DecodeReaderLimit(nil, patch, firstPassLimit(*maxSize))
		if err != nil {
			return fail(stderr, err)
		}
		patch = bytes.NewReader(inner)
	}
	var rules *tinydelta.Rules
	if *rulesFile != "" {
		var code int
		if rules, code = loadRules(*rulesFile, "", stderr); rules == nil {
			return code
		}
	}
	target, err := tinydelta.DecodeRules(dict, patch, *maxSize, rules)
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

// firstPassLimit caps a two-pass patch's first pass, whose output is the
// intermediate patch, for a target of at most maxSize bytes. That patch can
// be larger than the target, but at most 8 bytes per target byte: a 6-byte
// COPY to realign, then an ADD_DIFF of its one byte. So a valid patch always
// fits, and a tiny patch can't expand to far more than -max-size allows.
func firstPassLimit(maxSize int) int {
	if maxSize >= (tinydelta.MaxSize-firstPassSlack)/firstPassFactor {
		return tinydelta.MaxSize
	}
	return maxSize*firstPassFactor + firstPassSlack
}

const (
	firstPassFactor = 8
	firstPassSlack  = 4096
)

// misplacedFlag reports an output name that looks like a flag: flags go
// before the file names, so "encode a b -greedy" would otherwise write a
// file called -greedy. A file really named that can be given as ./-greedy.
func misplacedFlag(fs *flag.FlagSet) bool {
	name := fs.Arg(2)
	return len(name) > 1 && name[0] == '-'
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
	// The library's errors already say where they're from.
	fmt.Fprintln(stderr, "tinydelta:", strings.TrimPrefix(err.Error(), "tinydelta: "))
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

// loadRules reads a rule file, or else the built-in table named by reloc;
// nil and an exit code if neither works.
func loadRules(file, reloc string, stderr io.Writer) (*tinydelta.Rules, int) {
	if file != "" {
		src, err := os.ReadFile(file)
		if err != nil {
			return nil, fail(stderr, err)
		}
		r, err := tinydelta.ParseRules(src)
		if err != nil {
			fmt.Fprintf(stderr, "tinydelta: %s: %v\n", file, err)
			return nil, exitFailure
		}
		return r, exitOK
	}
	r, err := tinydelta.BuiltinRules(reloc)
	if err != nil {
		fmt.Fprintf(stderr, "tinydelta: -reloc %q: want thumb or msp430x\n", reloc)
		return nil, exitUsage
	}
	return r, exitOK
}

// rulesCommand checks or compiles a relocation rule file.
func rulesCommand(args []string, stdout, stderr io.Writer) int {
	usage := func() int {
		fmt.Fprintln(stderr, "Usage: tinydelta rules check <rules.yaml>\n       tinydelta rules compile [-o table.bin] [-c rules.h] <rules.yaml>")
		return exitUsage
	}
	if len(args) == 0 {
		return usage()
	}
	fs := flag.NewFlagSet("rules "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "", "write the binary table to this file")
	header := fs.String("c", "", "write the table as a C header (a const array and its CRC) to this file")
	if err := fs.Parse(args[1:]); err != nil {
		return parseExit(err)
	}
	if fs.NArg() != 1 || (args[0] != "check" && args[0] != "compile") || (args[0] == "check" && (*out != "" || *header != "")) {
		return usage()
	}
	rules, code := loadRules(fs.Arg(0), "", stderr)
	if rules == nil {
		return code
	}
	table := rules.Table()
	fmt.Fprintf(stdout, "%s: %d-byte table, CRC %04x\n", rules.Name(), len(table), rules.CRC())
	if *out != "" {
		if err := os.WriteFile(*out, table, 0o644); err != nil {
			return fail(stderr, err)
		}
	}
	if *header != "" {
		if err := os.WriteFile(*header, []byte(rules.CHeader(fs.Arg(0))), 0o644); err != nil {
			return fail(stderr, err)
		}
	}
	return exitOK
}
