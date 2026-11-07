#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT
"""Decode time of the C decoders on Cortex-M and MSP430, as a Markdown table.

The inputs are written by TestTimingInputs (go test): a firmware-like image
for each instruction set, about 7 KB, its update, and plain, coded and
relocated patches between them. c/timing/timing.c decodes one of them on
the target and checks the result.

  Cortex-M0, Cortex-M4  arm-none-eabi-gcc -Os, run in the Unicorn emulator:
                        instructions executed by the decode (not cycles:
                        Unicorn has no timing; a Cortex-M0 takes 1-3 cycles
                        for most, more for loads on slow flash).
  MSP430                MSP430 CC (default msp430-elf-gcc; clang works
                        too) -Os for classic MSP430, run in mspdebug's
                        simulator: exact cycles, the run's less a run that
                        skips the decode, with a 16-bit hardware multiplier
                        (most MSP430s have one; without, the coder's
                        multiplies cost far more). MSP430X -mlarge code is
                        somewhat larger and slower.

Usage: scripts/c-time.py arm|msp430 [inputs dir]   (default: generate them)
Needs: arm: arm-none-eabi-gcc, python3 unicorn (pip install unicorn).
       msp430: msp430-elf-gcc (or MSP430_CC=clang with binutils-msp430),
       mspdebug.
"""
import os
import re
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.join(HERE, "..")
C_DIR = os.path.join(ROOT, "c")
T_DIR = os.path.join(C_DIR, "timing")

# label, input kind, defines
CASES = [
    ("plain patch, memory decoder", "plain", []),
    ("coded patch, memory decoder", "coded", ["-DTINYDELTA_CODER=1"]),
    ("relocated patch, memory decoder", "relocated", ["-DTINYDELTA_CODER=1", "-DTINYDELTA_RELOC={reloc}"]),
    ("relocated patch, callback decoder (256 B buffer, 64 B pages)", "relocated", ["-DTINYDELTA_CODER=1", "-DTINYDELTA_RELOC={reloc}", "-DCALLBACK=1"]),
]
ARCHES = [("Thumb", "thumb", 1), ("MSP430X", "msp430x", 2)]


def run(cmd, **kw):
    done = subprocess.run(cmd, capture_output=True, text=True, **kw)
    if done.returncode != 0:
        sys.exit(f"{' '.join(cmd)}\n{done.stdout}{done.stderr}")
    return done.stdout


def inputs(directory):
    if directory:
        return directory
    directory = tempfile.mkdtemp()
    env = dict(os.environ, TINYDELTA_TIMING_OUT=directory)
    subprocess.run(["go", "test", "-count=1", "-run", "^TestTimingInputs$", "."], cwd=ROOT, env=env, check=True, capture_output=True)
    return directory


def sources(defines, data):
    files = [os.path.join(T_DIR, "timing.c"), os.path.join(T_DIR, "libc.c"), data]
    files.append(os.path.join(C_DIR, "tinydelta_io.c" if "-DCALLBACK=1" in defines else "tinydelta_decode.c"))
    if "-DTINYDELTA_CODER=1" in defines:
        files.append(os.path.join(C_DIR, "tinydelta_coder.c"))
    if any(d.startswith("-DTINYDELTA_RELOC") for d in defines):
        files.append(os.path.join(C_DIR, "tinydelta_reloc.c"))
    return files


# ---- Cortex-M: Unicorn --------------------------------------------------

def arm_count(elf):
    from unicorn import Uc, UC_ARCH_ARM, UC_MODE_THUMB, UC_MODE_MCLASS, UC_HOOK_BLOCK
    from unicorn.arm_const import UC_ARM_REG_SP, UC_ARM_REG_PC

    syms = {}
    for line in run(["arm-none-eabi-nm", elf]).splitlines():
        parts = line.split()
        if len(parts) == 3:
            syms[parts[2]] = int(parts[0], 16)
    binary = elf + ".bin"
    run(["arm-none-eabi-objcopy", "-O", "binary", "-j", ".text", "-j", ".data", elf, binary])
    image = open(binary, "rb").read()
    uc = Uc(UC_ARCH_ARM, UC_MODE_THUMB | UC_MODE_MCLASS)
    uc.mem_map(0, 0x40000)
    uc.mem_map(0x20000000, 0x10000)
    uc.mem_write(0, image)  # .text then .data's load image, as the startup expects
    sp = int.from_bytes(image[0:4], "little")
    reset = int.from_bytes(image[4:8], "little")
    start, stop, done = syms["timing_start"] & ~1, syms["timing_stop"] & ~1, syms["timing_done"] & ~1
    lengths = {}  # instructions per basic block (Thumb: 16- or 32-bit each)
    state = {"on": False, "count": 0}

    def block(uc, address, size, _):
        if address == start:
            state["on"] = True
        elif address == stop:
            state["on"] = False
        elif address == done:
            uc.emu_stop()
        if state["on"]:
            n = lengths.get((address, size))
            if n is None:
                code, n, i = uc.mem_read(address, size), 0, 0
                while i < size:
                    half = code[i] | code[i + 1] << 8
                    i += 4 if (half >> 11) in (0b11101, 0b11110, 0b11111) else 2
                    n += 1
                lengths[(address, size)] = n
            state["count"] += n

    uc.hook_add(UC_HOOK_BLOCK, block)
    uc.reg_write(UC_ARM_REG_SP, sp)
    uc.emu_start(reset | 1, done, timeout=600 * 1000 * 1000)
    result = int.from_bytes(uc.mem_read(syms["timing_result"], 4), "little", signed=True)
    return result, state["count"]


def time_arm(directory):
    cpus = [("Cortex-M0", "cortex-m0"), ("Cortex-M4", "cortex-m4")]
    rows = []
    with tempfile.TemporaryDirectory() as out:
        for arch, prefix, reloc in ARCHES:
            for label, kind, defines in CASES:
                defines = [d.format(reloc=reloc) for d in defines]
                data = os.path.join(directory, f"{prefix}_{kind}.c")
                row = [f"{arch} image, {label}"]
                for _, cpu in cpus:
                    elf = os.path.join(out, f"{prefix}_{kind}_{cpu}.elf")
                    run(["arm-none-eabi-gcc", "-std=c99", "-Os", f"-mcpu={cpu}", "-mthumb", "-ffreestanding", "-nostdlib", "-I", C_DIR, *defines,
                         "-T", os.path.join(T_DIR, "arm.ld"), os.path.join(T_DIR, "start_arm.c"), *sources(defines, data), "-lgcc", "-o", elf])
                    result, count = arm_count(elf)
                    if result != 0:
                        sys.exit(f"{elf}: decode failed ({result})")
                    row.append(count)
                rows.append(row)
    size = old_size(directory)
    print("| decode of a 7 KB update | " + " | ".join(f"{name}: instructions | per old-image byte" for name, _ in cpus) + " |")
    print("|---|" + "---:|---:|" * len(cpus))
    for row in rows:
        print(f"| {row[0]} | " + " | ".join(f"{n:,} | {n / size[row[0].split()[0]]:,.0f}" for n in row[1:]) + " |")
    version = run(["arm-none-eabi-gcc", "--version"]).splitlines()[0]
    print(f"\n{version}, `-Os`; instructions executed in the Unicorn emulator from the decoder's call to its return.")


# ---- MSP430: mspdebug's simulator ---------------------------------------

LAST_OUTPUT = ""

def msp430_cycles(elf):
    out = subprocess.run(["mspdebug", "-q", "sim", f"prog {elf}", "simio add tracer t", "simio add hwmult m", "step 1000000000", "md timing_result 2", "simio info t"],
                         stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=3600).stdout
    globals()["LAST_OUTPUT"] = out
    result = re.search(r"^\s*[0-9a-f]+: ([0-9a-f]{2}) ([0-9a-f]{2})\s+\|", out, re.M)  # md's dump line, not the disassembly
    cycles = re.search(r"^MCLK:\s+(\d+)", out, re.M)
    if not result or not cycles:
        sys.exit(f"{elf}: no result from mspdebug:\n{out[-2000:]}")
    value = int(result.group(2) + result.group(1), 16)
    return (value - 0x10000 if value >= 0x8000 else value), int(cycles.group(1))


def time_msp430(directory):
    cc = os.environ.get("MSP430_CC", "msp430-elf-gcc")
    clang = os.path.basename(cc).startswith("clang")
    if clang:  # no C library headers nor link driver: a string.h, our helpers, binutils' linker
        flags = ["--target=msp430", "-ffreestanding", "-fno-builtin", "-I", os.path.join(T_DIR, "clang")]
    else:
        flags = ["-mcpu=msp430", "-mhwmult=16bit", "-ffreestanding"]
    rows = []
    with tempfile.TemporaryDirectory() as out:
        for arch, prefix, reloc in ARCHES:
            for label, kind, defines in CASES:
                defines = [d.format(reloc=reloc) for d in defines]
                data = os.path.join(directory, f"{prefix}_{kind}.c")
                cycles = []
                for skip in (0, 1):
                    elf = os.path.join(out, f"{prefix}_{kind}_{len(rows)}_{skip}.elf")
                    objs = []
                    for src in [os.path.join(T_DIR, "start_msp430.c"), *sources(defines, data)] + ([os.path.join(T_DIR, "mspabi_clang.S")] if clang else []):
                        objs.append(f"{elf}.{len(objs)}.o")
                        run([cc, "-std=c99", "-Os", *flags, "-I", C_DIR, *defines, f"-DSKIP_DECODE={skip}", "-c", src, "-o", objs[-1]])
                    if clang:
                        run(["msp430-ld", "-T", os.path.join(T_DIR, "msp430.ld"), *objs, "-o", elf])
                    else:
                        run([cc, *flags, "-nostdlib", "-nostartfiles", "-T", os.path.join(T_DIR, "msp430.ld"), *objs, "-lmul_16", "-lgcc", "-o", elf])  # flags: libgcc for MSP430, not MSP430X; libmul_16: its hardware multiplier routines
                    result, n = msp430_cycles(elf)
                    if result != 0:
                        sys.exit(f"{elf}: decode failed ({result})\n{LAST_OUTPUT[-1500:]}")
                    cycles.append(n)
                rows.append((f"{arch} image, {label}", cycles[0] - cycles[1]))
    size = old_size(directory)
    print("| decode of a 7 KB update | MSP430: cycles | per old-image byte | at 8 MHz |")
    print("|---|---:|---:|---:|")
    for name, n in rows:
        print(f"| {name} | {n:,} | {n / size[name.split()[0]]:,.0f} | {n / 8e6 * 1000:,.0f} ms |")
    version = run([cc, "--version"]).splitlines()[0]
    print(f"\n{version}, `-Os`, classic MSP430 (16-bit addresses); exact cycles in mspdebug's simulator, "
          "the run's less a run without the decode; with a hardware multiplier. MSP430X `-mlarge` code takes somewhat more.")


def old_size(directory):
    sizes = {}
    for arch, prefix, _ in ARCHES:
        text = open(os.path.join(directory, f"{prefix}_plain.c")).read()
        sizes[arch] = int(re.search(r"old_image_len = (\d+)", text).group(1))
    return sizes


def main():
    if len(sys.argv) not in (2, 3) or sys.argv[1] not in ("arm", "msp430"):
        sys.exit(__doc__)
    directory = inputs(sys.argv[2] if len(sys.argv) == 3 else None)
    (time_arm if sys.argv[1] == "arm" else time_msp430)(directory)


if __name__ == "__main__":
    main()
