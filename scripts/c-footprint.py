#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT
"""Code size and worst-case stack of each C decoder configuration, as a
Markdown table.

Each configuration is compiled from the files it needs, for each target.
Size is the objects' text (code and constants, as `size` counts it). The
stack is the deepest path from the decoder's entry point through GCC's call
graph (-fcallgraph-info=su): every function's frame, as the compiler
reports it, summed along the worst chain of calls. It excludes the caller's
own frame and the stack of the callbacks a callback decoder calls (they are
the indirect calls in the graph). A recursive call or a frame of dynamic
size would make that bound meaningless, so either fails the script.

Usage: scripts/c-footprint.py arm|msp430
"""
import os
import re
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
C_DIR = os.path.join(HERE, "..", "c")

CODER = ["-DTINYDELTA_CODER=1"]
# name, files, defines, entry point
CONFIGS = [
    ("memory decoder", ["decode"], [], "tinydelta_decode"),
    ("callback decoder", ["io"], [], "tinydelta_decode_io"),
    ("memory decoder + coder", ["decode", "coder"], CODER, "tinydelta_decode_coded"),
    ("callback decoder + coder", ["io", "coder"], CODER, "tinydelta_decode_io"),
    ("memory decoder + coder + Thumb relocation", ["decode", "coder", "reloc"], CODER + ["-DTINYDELTA_RELOC=1"], "tinydelta_decode_coded"),
    ("callback decoder + coder + Thumb relocation", ["io", "coder", "reloc"], CODER + ["-DTINYDELTA_RELOC=1"], "tinydelta_decode_io"),
    ("memory decoder + coder + MSP430X relocation", ["decode", "coder", "reloc"], CODER + ["-DTINYDELTA_RELOC=2"], "tinydelta_decode_coded"),
    ("callback decoder + coder + MSP430X relocation", ["io", "coder", "reloc"], CODER + ["-DTINYDELTA_RELOC=2"], "tinydelta_decode_io"),
    ("memory decoder + coder + MSP430X relocation + shipped tables", ["decode", "coder", "reloc"],
     CODER + ["-DTINYDELTA_RELOC=2", "-DTINYDELTA_RELOC_SHIPPED=1"], "tinydelta_decode_coded"),
]

TARGETS = {
    "arm": ("arm-none-eabi-gcc", "arm-none-eabi-size",
            [("Cortex-M0", ["-mcpu=cortex-m0", "-mthumb"]), ("Cortex-M3", ["-mcpu=cortex-m3", "-mthumb"]), ("Cortex-M4", ["-mcpu=cortex-m4", "-mthumb"])],
            "Cortex-M0"),
    "msp430": ("msp430-elf-gcc", "msp430-elf-size",
               [("MSP430", ["-mcpu=msp430"]), ("MSP430X", ["-mcpu=msp430x"]), ("MSP430X, `-mlarge`", ["-mcpu=msp430x", "-mlarge"])],
               "MSP430X, `-mlarge`"),
}

EXTERNAL = set()  # library and runtime routines the decoders call
NODE = re.compile(r'node: \{ title: "([^"]+)" label: "[^"\\]*\\n[^"\\]*\\n(\d+) bytes \(([^)]*)\)"')
NODE_ANY = re.compile(r'node: \{ title: "([^"]+)"')
EDGE = re.compile(r'edge: \{ sourcename: "([^"]+)" targetname: "([^"]+)"')


def call_graph(ci_files):
    """Frames by function and calls, from GCC's .ci files. Static
    functions are named file:function, others by name, as GCC writes them."""
    frames, calls = {}, {}
    for path in ci_files:
        with open(path) as f:
            text = f.read()
        for name, size, kind in NODE.findall(text):
            if kind != "static":
                sys.exit(f"{name}: stack frame of {kind} size, no bound")
            frames[name] = int(size)
        for src, dst in EDGE.findall(text):
            calls.setdefault(src, set()).add(dst)
    return frames, calls


def deepest(frames, calls, entry, path=()):
    """The stack of the deepest chain of calls from entry."""
    if entry in path:
        sys.exit("recursion: " + " -> ".join(path + (entry,)))
    if entry == "__indirect_call":
        return 0  # a callback: its own stack is the caller's to count
    if entry not in frames:
        if entry in ("memcpy", "memmove", "memset") or entry.startswith("__"):
            EXTERNAL.add(entry)
            return 0  # the C library's or the compiler's runtime (leaf routines with small frames): listed in the output
        sys.exit(f"{entry}: called but not in the call graph")
    return frames[entry] + max((deepest(frames, calls, c, path + (entry,)) for c in calls.get(entry, ())), default=0)


def main():
    if len(sys.argv) != 2 or sys.argv[1] not in TARGETS:
        sys.exit(__doc__)
    cc, size_tool, targets, stack_target = TARGETS[sys.argv[1]]
    rows = []
    libc = set()
    with tempfile.TemporaryDirectory() as out:
        for name, files, defines, entry in CONFIGS:
            row = [name]
            stack = None
            for label, flags in targets:
                objs, cis = [], []
                for f in files:
                    base = os.path.join(out, f"{len(rows)}_{label.replace(' ', '_').replace(',', '').replace('`', '')}_{f}")
                    subprocess.run([cc, "-std=c99", "-Os", *flags, "-Wall", "-Wextra", "-Werror", *defines, "-I", C_DIR,
                                    "-fcallgraph-info=su", "-dumpbase", base, "-c", os.path.join(C_DIR, f"tinydelta_{f}.c"), "-o", base + ".o"],
                                   check=True)
                    objs.append(base + ".o")
                    cis.append(base + ".ci")
                undefined = subprocess.run([size_tool.replace("size", "nm"), "-u", *objs], check=True, capture_output=True, text=True).stdout
                libc |= {line.split()[-1] for line in undefined.splitlines() if line.split() and line.split()[-1] in ("memcpy", "memmove", "memset")}
                sizes = subprocess.run([size_tool, *objs], check=True, capture_output=True, text=True).stdout.splitlines()[1:]
                row.append(f"{sum(int(line.split()[0]) for line in sizes):,} B")
                if label == stack_target:
                    frames, calls = call_graph(cis)
                    stack = deepest(frames, calls, entry)
            row.append(f"{stack:,} B")
            rows.append(row)
    version = subprocess.run([cc, "--version"], check=True, capture_output=True, text=True).stdout.splitlines()[0]
    print("| configuration | " + " | ".join(label for label, _ in targets) + f" | stack ({stack_target}) |")
    print("|---|" + "---:|" * (len(targets) + 1))
    for row in rows:
        print("| " + " | ".join(row) + " |")
    print()
    print(f"Compiler: {version}; `-Os`. Size: code and constants (`size` text) of the files each configuration needs; "
          f"{', '.join('`' + f + '`' for f in sorted(libc))} come from your C library. "
          "Stack: the deepest chain of calls from the entry point (`-fcallgraph-info=su`), without your callbacks' own stack"
          + (" and without these library and runtime routines': " + ", ".join("`" + f + "`" for f in sorted(EXTERNAL)) if EXTERNAL else "") + ".")


if __name__ == "__main__":
    main()
