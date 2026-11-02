# Ideas beyond coded patches

A sketch: what could make patches smaller still, measured before anything
changes. Nothing here changes the format or the decoders; the
experiments are in [`ideas_test.go`](../ideas_test.go) and
[`ideas_header_test.go`](../ideas_header_test.go), built only with
`-tags ideas`.

The goals, in order: decoder footprint, patch size per footprint,
simplicity. So each idea is judged by what it adds to the decoder (code,
RAM, flash) against what it saves on the air.

## Summary

| idea | decoder cost | firmware (ARM) | firmware (MSP430X) | configuration | verdict |
|---|---|---:|---:|---:|---|
| [Predicted difference byte](#1-predicted-difference-byte) | ~120 B code (Cortex-M0), ~270 B (MSP430X), 4 states + 4 B | **−7.3%** | **−11.3%** | 0 | **do it** |
| … plus [16-bit carry](#2-carry-within-16-bit-words) in ADD_DIFF | a few instructions | −7.3% | **−13.9%** | 0 | do it, with 1 |
| [Short header and flush](#3-short-header-and-flush) | none to a few bytes | 0 | 0 | **−2.0%** | do it |
| [Trained starting states](#4-trained-starting-states) | 777 B flash (or 267 B) | −2.1% | – | **−6.3%** (with 3) | worth it for small files, best trained on your own |
| Difference tables per lane | +765 B RAM | −6.6% | −4.8% | 0 | no: 1 does better for 4 B |
| Matched literal (LZMA-style) | +510 B RAM | −0.1% | +0.2% | −0.1% | no |
| Thumb branch filter (xz BCJ) | a filter on dictionary reads | +3.2% | +2.0% | 0 | no |
| Repeated-delta flag (LZMA rep0) | 1 state | 0 | 0 | +2.4% | no |
| Folding short unchanged runs into ADD_DIFF | none (encoder) | +0.2% or worse | 0 | −0.1% | no |
| Cover switch margin, retuned for the coder | none (encoder) | 4 and 8 within 0.2% | – | – | no |

Percentages are against today's coded patch (layout B, 777 B tables, the
better of instruction sets 1 and 2 per update); 0 means within ±0.1%.

## Data and method

- **Firmware, ARM:** 10 consecutive MicroPython builds' updates for two
  SAMD21 boards (Cortex-M0+, about 185 KB), from the benchmark corpus.
- **Firmware, MSP430X:** mbedtls 2.28's crypto (AES, GCM, SHA-256, ECDSA,
  RSA, X.509, PK; about 176 KB) built with `msp430-elf-gcc` 14.2,
  `-mcpu=msp430x -mlarge -Os`, and linked as one image, for every
  release-branch commit that changed the library; 4 updates so far
  (most commits leave the crypto unchanged). Not a meter's firmware, but
  real MSP430X code generation, layout and address moves.
- **Configuration:** 126 consecutive versions of six Linux
  `arch/arm/configs` files (3–6 KB), from the benchmark corpus.

The experiments code each variant with the coder's own adaptive model
(the same states, bit trees and update rule as `coded.go`) and count the
ideal code length, which the range coder matches to within 0.05% here
(26,129 B estimated against 26,142 B real on the first sample). Changes
the model can't express, such as the header, were measured on the real
coder.

## 1. Predicted difference byte

This is the address prediction. When code moves, every pointer into it
moves by the same amount, so the same few ADD_DIFF bytes repeat at the
same place in a word: the low byte gets the shift, the next byte 0 or a
carry. Today the coder learns which byte values are common, but not
*where* in a word they are.

The idea: before an ADD_DIFF byte, the decoder codes one bit, "this is
the same as the last ADD_DIFF byte in this lane", where the lane is the
output position mod 2 (or mod 4). If the bit is 1 the byte is known; if 0,
the byte follows through the difference table as today. The bit's state
is chosen by the lane and by whether that lane's last prediction hit:
4 states for 16-bit lanes, 8 for 32-bit.

| | ARM, 10 updates | MSP430X, 4 updates |
|---|---:|---:|
| today | 44,019 B | 5,690 B |
| 32-bit lanes | 40,319 B (−8.4%) | 5,097 B (−10.4%) |
| 16-bit lanes | 40,786 B (−7.3%) | 5,046 B (−11.3%) |
| no lanes | 45,004 B (+2.2%) | 5,739 B (+0.9%) |

The lanes matter: without them the prediction is worse than none.
16-bit lanes do about as well on ARM as 32-bit ones and better on
MSP430X, so 16-bit lanes are the one choice for both.

Cost in the decoder, measured with a prototype of the C coder: about
**+120 B of code on Cortex-M0** (coder +108 B, each decoder +12–16 B, to
pass the lane) and **+270 B on MSP430X `-mlarge`**, plus 4–8 bytes of
states after the tables and 6 bytes in the coder's struct (the last byte
per lane and the hit bits). The encoder needs no new search, only the
same model.

Format: a features bit (bit 2) in the coded header, so patches without
it stay as they are and the encoder only sets it when it is smaller.

## 2. Carry within 16-bit words

Today ADD_DIFF adds byte by byte, mod 256. When a moved address's low
byte overflows, the next byte's difference is 1 instead of 0, so the
same move gives two different patterns. With a carry from the low byte
into the high byte of each 16-bit word (by output position), the
pattern is the same either way.

On its own it does little (ARM +0.1%, MSP430X −1.5%), but with the
prediction it brings MSP430X from −11.3% to **−13.9%**, and leaves ARM
the same (−7.3%). Carries across 32-bit words do no better.

Cost: a carry variable in the ADD_DIFF loop, a few instructions. It
changes what ADD_DIFF means, so it would be a features bit too (or come
with bit 2).

## 3. Short header and flush

A coded patch carries about 6 bytes the plain one doesn't: the
features byte, the stream-length varint, and a 4-byte flush at the end
of the range coder. On a 30-byte configuration update that decides
whether coding pays at all.

- **Shortest flush:** end the range coder with the value in its final
  interval that has the most trailing zero bytes, and stop there; the
  decoder reads missing bytes as 0. Typically 1–2 bytes instead of 4.
- **Layout in the version byte:** `0x80 | layout << 2 | instruction set`
  instead of a separate features byte, leaving 2 bits for future
  features.

Together: configuration −2.0%, coded patches chosen in 24 of 126 updates
instead of 11; firmware unchanged. The decoder cost is a fetch that
returns 0 past the end instead of an error, and a different header
parse; about neutral in code.

## 4. Trained starting states

Every state starts at even odds and learns as the patch decodes. A long
firmware patch learns quickly; a 30-byte configuration patch ends before
it has learned much. Starting the states from a table trained on typical
patches (in flash: 777 B for layout B, or 267 B for the opcode and length
trees only) saves that learning:

| | firmware, ARM | configuration |
|---|---:|---:|
| even odds | 43,962 B | 3,404 B |
| trained, all states | 43,024 B (−2.1%) | – |
| trained on configuration, with the short header (3) | – | 3,191 B (−6.3%), coded in 45 of 126 updates |

Each update was coded with states trained on the *other* items (leave
one item out), so this is not the table remembering the test data.

The catch is what to train on. One table trained on firmware and
configuration together was worse for configuration than none: firmware
patches are far longer and dominate it. So the table should be trained
on the kind of data the device receives, and a device's own
configuration files are the best possible training set. Then the table
is part of the device's firmware: the encoder must use the same one, so
the coded header would name it (a table number in the features byte),
and a device that doesn't have that table rejects the patch rather than
misdecoding it.

## What didn't help

- **Difference tables per lane** (four ADD_DIFF tables by position mod 4):
  −6.6% on ARM, but for 765 B more RAM; the predicted byte does better
  for 4 bytes.
- **Matched literal:** coding ADD literals with the old byte at the
  cursor as context, as LZMA does after a match. Under 0.2% either way:
  where tinydelta adds literals, the old byte says little about the new.
- **Thumb branch filter:** converting `BL` targets from relative to
  absolute before diffing (xz's ARM-Thumb filter), so that calls to moved
  code keep their bytes. Worse on both: as many calls change one way as
  the other, and the coder already handles the relative form. It would
  also need the decoder to filter dictionary reads.
- **Repeated delta:** a flag for "same COPY delta as last time", as
  LZMA's rep0. Deltas rarely repeat in tinydelta's streams.
- **Folding unchanged runs into ADD_DIFF** (as zeros, instead of SKIP):
  worse with the coder, as it was with deflate.
- **Cover switch margin:** tuned for deflate (8); 4 is 0.1% better with
  the coder, 16 and up worse. Not worth a change.

## Not measured yet

- **Cost-aware parsing (encoder only).** The optimal encoder minimizes
  plain bytes; for a coded patch it could minimize coded bits instead,
  by parsing once, taking the coder's statistics, and parsing again with
  those costs (as LZMA's optimal parser does). No decoder change at all;
  likely a few percent on configuration, where instruction choice is
  most of the patch.
- **Your own data.** All of the above should be checked on the meter's
  firmware and configuration files: the MSP430X numbers here come from
  four mbedtls updates, which may not move code the way the meter's
  firmware does. `go test -tags ideas -run TestIdeas -v . -args
  -firmware <pairs> -config <pairs>` with a pairs file of `name old new`
  lines runs every experiment.

## Proposed next step

One small format addition, "coded patches, version 2": the predicted
difference byte with 16-bit lanes and the 16-bit carry (1 and 2), and
the short header and flush (3). About 120–270 B more decoder code and
about 10 bytes of RAM, for patches 7% (ARM) to 14% (MSP430X) smaller on
firmware and 2% on configuration. Trained starting states (4) as a
separate, optional table, once there is device data to train on.
