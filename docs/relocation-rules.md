# Relocation rules

Status: **draft**. Implemented in Go (rules.go, yamlsubset.go,
`tinydelta rules`, `-rules`, `-ship-rules`) and C (tinydelta_reloc.c, one
table engine for every rule set, with TINYDELTA_RELOC_SHIPPED for shipped
tables). The rule files relocate byte for byte as the hand-written rules
they replace on 66 real firmware updates and 40,000 random images; the C
engine matches Go in the same tests. A CBMC harness for the engine exists
(c/cbmc/reloc.c) but isn't part of scripts/c-cbmc.sh yet: it needs loop
bounds that keep it tractable.

Relocation (README, "Relocation") rewrites the addresses in the old image
before the patch applies, through a map of how code and data moved. Which
bytes hold an address is decided by a *rule*. Until now there were two,
written by hand in Go and C for ARM Thumb and MSP430X. This document
replaces them with **rule tables**: a YAML file says which instruction
forms hold addresses and where their bits are; a compiler turns it into a
compact binary table; one generic engine, in Go for the encoder and in C
for the decoder, applies any table.

Goals:

- a new instruction form, or a new MCU, is a rule file, not new code;
- the encoder and `relocstats` read the YAML directly, so trying a rule is
  edit and rerun, on the developer's machine, without sharing images;
- a patch can carry its own table, so devices in the field can use new
  rules without a new decoder (experimental);
- native speed and no control flow in tables: the engine only matches
  masks and moves bits, so it is easy to bound, review and model-check;
- `rules/thumb.yaml` and `rules/msp430x.yaml` reproduce the hand-written
  rules byte for byte; the hand-written code is removed once that is shown.

## Contents

- [The idea in one example](#the-idea-in-one-example)
- [Rule files](#rule-files)
- [What the engine does](#what-the-engine-does)
- [Binary table](#binary-table)
- [In the patch](#in-the-patch)
- [Limits and validation](#limits-and-validation)
- [Tools](#tools)
- [Covering other MCUs](#covering-other-mcus)
- [Testing](#testing)
- [Open questions](#open-questions)

## The idea in one example

MSP430X `CALLA #imm20` is the word `13Bh` followed by `hhhh`: the 20-bit
address's top 4 bits are in the low nibble of the first word, the rest in
the second. As a rule:

```yaml
- name: CALLA #imm20
  match: ["13B0/FFF0"]                 # w0 AND FFF0 == 13B0
  address: ["w0[3:0]", "w1[15:0]"]     # joined, most significant first
```

For the bytes `b3 13 4c 93`: `w0 = 13B3` matches; the address is
`3` then `934C`, so `0x3934C`. If the map says that code moved +398, the
engine writes `0x394DA` back through the same slices: `w0[3:0] = 3`,
`w1 = 94DA`. Every other bit stays as it was.

## Rule files

YAML, one file per instruction set; [`rules/msp430x.yaml`](../rules/msp430x.yaml)
and [`rules/thumb.yaml`](../rules/thumb.yaml) are complete examples.
Unknown keys are errors. tinydelta reads a strict subset of YAML (its own
reader, no dependency): block mappings and lists by indentation,
single-line `{...}` and `[...]`, plain and quoted strings, `#` comments.
Anchors, tags, multi-line strings and tabs are errors with a line number.
Quote any text with ` #` in it (`name: "CALLA #imm20"`), as YAML reads
the rest as a comment.

### Header

| key | meaning |
|---|---|
| `version` | `1` |
| `name` | free text, shown by the tools |
| `unit` | bytes between positions the engine tries: 1, 2 or 4 |
| `word` | bits per word `w0`, `w1`, ...: 8, 16, 24 or 32 |
| `endian` | `little` or `big`: how the CPU reads a word from bytes |
| `sync` | 0, or how many units back the engine decodes instruction lengths to check that a code rule's position starts an instruction ([Sync](#sync)) |

### Words and conditions

`wN` is the word that starts `N × word/8` bytes after the position,
read in the file's byte order. In the MSP430X file the bytes `b3 13`
are `w0 = 13B3`, exactly as a disassembler shows them.

`match` is a list, one entry per word from `w0`:

| entry | true when |
|---|---|
| `"13B0/FFF0"` | `(w AND FFF0) == 13B0`: the mask's 1 bits must equal the value |
| `"!0000/000F"` | not that: here, the low nibble is not 0 |
| `"0000/FFF0 & !0000/000F"` | both, on the same word |
| `"-"` | anything, but the word must exist |

Values and masks are hex, as wide as a word. A rule only matches if every
word it uses (in `match`, `address` or `size`) lies inside the image.

### Rules

Four lists, each tried top to bottom, first match wins:

| list | used for |
|---|---|
| `prefixes` | instruction lengths: a word that comes before an instruction (MSP430X extension word, STM8 prefix). `skip`: its bytes |
| `lengths` | instruction lengths: `length` in bytes. A position no rule matches has length `unit` |
| `code` | addresses, outside data runs; with `sync`, only where an instruction starts |
| `data` | addresses, inside data runs (the encoder decides where those are) |

A `code` or `data` rule's keys:

| key | meaning | default |
|---|---|---|
| `name` | for messages and `unless` | required |
| `match` | conditions, as above | required |
| `address` | slices of the address, most significant first: `"wN[hi:lo]"` (bits `hi` down to `lo` of word N), or `"0"`/`"1"`, a fixed bit (as RISC-V's always-even offsets) | none: an **exclusion**, a rule that matches and moves nothing, so later rules don't see it |
| `address2` | a second address in the same instruction (MSP430X source and destination) | none |
| `size` | bytes the instruction must have inside the image, beyond the words it uses | the words used |
| `align` | positions only where `(base + position) mod align == 0` | `unit` |
| `relative` | `{ base: B, sign_extend: S }`: the slices hold an offset, not an address; target = position's address + B + offset, offset sign-extended from S bits | absolute |
| `space` | where the address is looked up: `auto` (in the image if it points there, else in the RAM ranges), `code` (the image only), `data` (RAM ranges only) | `auto` |
| `require` | `in-image`: only an address that points into the image counts as one (keeps random constants in data still) | none |
| `unless` | `[{ rule: R, at: [offsets] }]`: not if rule `R` (`self` for this one) matches at these offsets from this position; that check ignores R's own `unless` | none |

### The two files as of now

MSP430X: the extension-word prefix and 18 length rules (the length
classes of format I, II and the address instructions), 34 code rules (5
for `CALLA`/`MOVA`, 15 for the extension-word source/destination
combinations, 6 of them exclusions; 14 for the 16-bit `&abs`, `x(Rn)`
and `#imm` operands of classic one- and two-operand instructions, 4 of
them exclusions for stack offsets and PC-relative operands), 1 data rule
(20-bit pointers).
Thumb: 3 code rules (`BL`, the Thumb-2 tail call `B.W`, and 4-byte-aligned
pointers not overlapping either).

## What the engine does

The decoder relocates a span `[s, e)` of the old image whenever it reads
old bytes (COPY, ADD_DIFF); the encoder relocates the whole image. Both
compute the same bytes: everything below depends only on the raw old
image near a position, the map and the table, never on what was
relocated before. So spans can be relocated in any order and any size.

### Positions

Positions `p` run from `s − reach` to `e − 1`, where `(base + p) mod unit
== 0`. `reach` is the largest `size` (or words used) of any rule, less
one, so an instruction that starts before the span but holds an address
inside it is still found.

At each `p` the engine takes the `data` list if `p` is in a data run,
else the `code` list, and finds the first rule that matches (its `align`,
and its conditions, on words inside the image; with `require`, also its
address). A rule that matches but whose `size` runs past the end of the
image (an instruction cut off) is still the one chosen, and moves
nothing. Then:

1. no rule, or an exclusion: nothing at `p`;
2. a code rule with `sync > 0` and `p` not an [instruction start](#sync):
   nothing;
3. an `unless` that holds: nothing;
4. each address of the rule is [moved](#moving-an-address).

### Sync

Data that looks like an instruction would make the engine see addresses
that aren't. With `sync: N`, a code rule only applies at `p` if decoding
instruction lengths forward from `N` units before `p` lands on `p`:

```text
covered = 0                                  # bit i: the unit i units on is inside an instruction
for i = N down to 1:
    if bit 0 of covered is clear and p − i·unit ≥ 0:
        L = length(p − i·unit)               # bytes; 0 if that position is in a data run
        covered |= ((1 << (L / unit)) − 1) & ~1
    covered >>= 1
p starts an instruction  ⇔  bit 0 of covered is clear
```

`length(q)` is: if a `prefixes` rule matches at `q` and the bytes after
it are inside the image, its `skip` plus the length of what follows
(matched as if it started there; at most one prefix); else the first
`lengths` rule matching at `q`; else `unit`.

### Moving an address

For an absolute address `a` (the slices joined):

- if `a` points into the old image (`base ≤ a < base + length`), its
  shift is that of the map entry holding `a − base`; with `space: data`
  it isn't moved;
- else its shift is that of the RAM range holding `a`; with `space: code`
  it isn't moved;
- the new address is `a + shift`; it is written back if it isn't `a`, is
  not negative and fits the slices (fixed bits included), else the field
  stays as it is and the patch carries the difference.

For a relative field, the offset `o` (slices joined, sign-extended from
`sign_extend` bits) gives the target `t = base + p + B + o`. Only a target
inside the image counts: data that looks like a call (a Thumb `BL`) aims
far outside it, and a real call outside the image (into a ROM, say) is
left to the patch. The new offset is `o + shift(t) − shift(base + p)`,
each shift looked up as above; it is written back if it changed and fits
(in `sign_extend` bits, fixed bits included).

Writing back puts each slice's bits into its word and leaves every other
bit alone, so where two overlapping matches both move addresses (data
that happens to look like two instructions), each keeps the other's
bits; only bits both write go to the later position. The hand-written
MSP430X rule rewrote the whole first word from the raw image there, and
was changed to match. The decoder writes only the bytes inside the span.

## Binary table

The compiler (`tinydelta rules compile`) turns the YAML into a table the
C engine reads in place: byte-aligned, no pointers, values and masks
little-endian. Each rule starts with its own length, so the engine steps
from rule to rule without decoding the ones it skips.

```text
table   = version (1)
        + mode (1: bits 0-1 log2 unit, 2-3 word bytes − 1, 4 big-endian)
        + sync (1)
        + prefixes (1) + lengths (1) + code (1) + data (1)        rule counts
        + rules, in that order

rule    = length (1: the rule's bytes, this one included)
        + flags (1: bits 0-1 addresses 0-2, 2-4 conditions 0-4, 5 relative, 6 require, 7 unless follows)
        + attr (1: bits 0-1 space, 2-3 log2 align)
        + n (1: prefixes skip, lengths length, code and data size)
        + conditions × ( word (1: bits 0-6 index, bit 7 negate) + value (word bytes) + mask (word bytes) )
        + addresses × ( slices (1) + slices × slice )
        + [relative: base (1, signed) + sign_extend (1)]
        + [unless: entries (1) + entries × ( rule index (1) + offsets (1) + offsets × offset (1, signed) )]

slice   = (1: bits 0-4 lowest bit, 5-7 word) + (1: bits 0-4 width − 1)
          or fixed bits: (1: their value) + (1: 0x80 | count − 1)
```

The CRC is CRC-16/CCITT-FALSE (polynomial 0x1021, initial 0xFFFF) over
the whole table.

Size: `rules/msp430x.yaml` compiles to 833 B, `rules/thumb.yaml` to 73 B.
(A layout without the per-rule length, attr and size bytes was about 20% smaller,
but the C engine then had to decode every rule it skipped; 717 B with
every field in every rule.)

## In the patch

The coded header's features byte:

| bits | meaning |
|---|---|
| 0-1 | the coder's table layout (1 C, 2 B, 3 A) |
| 2-3 | relocation: 0 none; 1 with the decoder's built-in rule table; 2 with a rule table in the stream; 3 reserved |
| 4-7 | reserved, 0 |

The whole coded patch, with relocation:

```text
byte 0     0x81 or 0x82: coded, instruction set of version 1 or 2
byte 1     features (above)
table      1: the CRC-16 of the table the encoder used (2 bytes, little-endian)
           2: the shipped table's length (varint)
map        base (varint), then three lists, each a count (varint) and its records:
             entries      gap, length, shift (zigzag)      old-image offsets that moved
             data runs    gap, length                      in 256-byte blocks
             RAM ranges   gap, length, shift (zigzag)      addresses outside the image that moved
           (gap: from the previous record's end, or from 0 for the first)
n          the uncoded instruction stream's length (varint)
stream     range-coded: [2: the shipped table's bytes], then the instructions
```

With 1, the CRC must equal the decoder's built-in table's, else
`TINYDELTA_ERR_VERSION`: a patch made for other rules is refused, never
applied with the wrong ones. Empty lists cost one byte each. A shipped table is coded at the start of the
stream, each byte by a bit tree of 255 states that start at even odds
(the decoder borrows the coder's tables for them and clears them before
the instructions): a table costs about two thirds of its size in the
patch (350 B for the MSP430X table when it was 536 B). The map stays uncoded: per patch it is too small for an adaptive
model to learn (coding the maps of 17 MSP430X updates gave 575 B for
605 B raw, and 624 B with a model per field).

In the decoder's tables buffer: the coder's tables (522, 777 or 1287 B),
then a shipped table (its length), then the map (12 B per entry and per
RAM range, 8 B per data run). A shipped table is only for experiments: a
table worth keeping becomes the built-in one.

## Limits and validation

Limits keep every loop bounded and every index checkable:

| | max |
|---|---:|
| rules per list | 64 |
| conditions per rule | 4 |
| words a rule uses (`w0`…) | 8 |
| `size` | 16 bytes |
| slices per address | 6, at most 32 bits in all |
| `unless` entries per rule / offsets per entry | 2 / 4, each within ±16 bytes |
| `sync` | 16 units |
| prefix `skip` | 4 bytes |

The C engine checks a shipped table against all of these before using
it (`TINYDELTA_ERR_MALFORMED` if any fails), and a built-in one at
compile time. `tinydelta rules check` reports the same in YAML terms,
with line numbers, plus warnings the device doesn't need: a rule that an
earlier one always hides, a field bit used twice, slices that cover a
condition's bits.

Within these limits the work per position is bounded: at most 64
condition checks per list, `sync` length decodes, and two `unless`
evaluations of one rule each.

## Tools

```sh
tinydelta rules check  rules/msp430x.yaml
tinydelta rules compile rules/msp430x.yaml -o msp430x.bin        # the binary table
tinydelta rules compile rules/msp430x.yaml -c msp430x_rules.h    # a const C array and its CRC, for the decoder build

tinydelta encode   -rules rules/msp430x.yaml -base 0xDF80 [-ship-rules] old.bin new.bin update.td
tinydelta relocstats -rules rules/msp430x.yaml -base 0xDF80 old.bin new.bin
```

`-reloc thumb` and `-reloc msp430x` stay as shorthands for the two files,
which are built into the CLI; `decode -rules FILE` decodes a patch that
names a table by CRC. Go: `ParseRules`, `BuiltinRules`,
`EncodeOptions.Rules` and `ShipRules`, `DecodeRules`; Python: `rules=`
(a path) and `ship_rules=` in `encode`, `rules=` in `decode`.

The C decoder is built with `TINYDELTA_RELOC` = `TINYDELTA_RELOC_THUMB`
or `TINYDELTA_RELOC_MSP430X` (the built-in tables,
`c/tinydelta_rules_*.h`), or `TINYDELTA_RELOC_RULES` with
`TINYDELTA_RELOC_RULES_HEADER` naming a header from `tinydelta rules
compile -c` for a table of your own. `TINYDELTA_RELOC_SHIPPED = 1` adds
accepting tables shipped in patches, and the checker they need; without
it the table is trusted (it is part of the firmware) and not checked.

## Covering other MCUs

The same building blocks describe the usual address forms of small MCUs.
Some, for later rule files:

```yaml
# RISC-V (word: 32, unit: 2 for the compressed extension)
- name: JAL
  match: ["0000006F/0000007F"]
  address: ["w0[31]", "w0[19:12]", "w0[20]", "w0[30:21]", "0"]
  relative: { base: 0, sign_extend: 21 }

# AVR (word: 16): CALL k, a 22-bit word address; "0" makes it a byte address
- name: CALL
  match: ["940E/FE0E"]
  address: ["w0[8:4]", "w0[0]", "w1[15:0]", "0"]
  space: code

# 8051 (unit: 1, word: 8, endian: big): LCALL addr16
- name: LCALL
  match: ["12/FF"]
  address: ["w1[7:0]", "w2[7:0]"]
```

Not covered yet, each a small addition when a target needs it: Thumb-2
`BL`'s J1/J2 bits (each `NOT(J XOR S)`), RISC-V `AUIPC`+`JALR` pairs (two
parts summed, the low one signed), 8051 `ACALL` (page-relative). Never
covered: an address split over instructions that are far apart or shared
(PIC16 `PCLATH`, distant `MOVW`/`MOVT`, AVR `LDI` pairs); those stay in
the patch as differences.

## Testing

1. Go engine against the hand-written Go rules, on the synthetic images of
   `reloc_test.go`, the MSP430X mbedtls builds (consecutive and 4-5
   builds apart), the Thumb MicroPython builds, and random images with
   random maps (differential fuzzing): identical relocated images.
2. Patch sizes unchanged on those corpora but for the header (the CRC,
   and the empty lists Thumb now carries).
3. C engine against Go, as the existing relocation tests do; corrupted
   and random shipped tables accepted or rejected as Go does; CBMC on
   the engine with small symbolic tables and images.
4. Decode time and code size on MSP430X `-mlarge` (GNU simulator) against
   the hand-written rule.

## Open questions

1. **Shipped table size.** About 600 B for MSP430X, mostly the length and
   extension-word combinations. Fine for experiments? It could later be
   range-coded with the patch, or the lengths list could get a denser
   form.
2. **`address2`.** Two keys, or one `addresses:` list of slice lists?
3. **Build options.** `TINYDELTA_RELOC_SHIPPED` separate, so a product
   that never accepts shipped tables doesn't carry the validator?
