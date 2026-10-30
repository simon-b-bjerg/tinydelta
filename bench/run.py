#!/usr/bin/env python3
"""Encodes every benchmark pair with every tool and writes the sizes.

    python3 bench/run.py <cache dir> <results.csv> [--tools a,b,...]

Pairs are versions <step> apart within one item (steps from corpus.json),
built by bench/collect.py. Every patch is decoded again and compared with
the target, so a tool that produces a wrong patch stops the run instead
of reporting a size.

Tools (set the binaries with environment variables; defaults are on PATH):
    tinydelta         tinydelta encode (optimal; falls back to greedy for
                      large targets, as the library does)   $TINYDELTA
    tinydelta_greedy  tinydelta encode -greedy
    openvcdiff        open-vcdiff encode -target_matches     $OPENVCDIFF
    openvcdiff_zlib   the open-vcdiff patch, zlib level 9
    xdelta3           xdelta3 -9 -S none (VCDIFF, no secondary compression)
    bsdiff            bsdiff 4 (with bzip2 inside)
"""

import csv
import json
import os
import subprocess
import sys
import tempfile
import zlib

HERE = os.path.dirname(os.path.abspath(__file__))
TINYDELTA = os.environ.get("TINYDELTA", "tinydelta")
OPENVCDIFF = os.environ.get("OPENVCDIFF", "vcdiff")
ALL_TOOLS = ["tinydelta", "tinydelta_greedy", "openvcdiff", "openvcdiff_zlib", "xdelta3", "bsdiff"]


def sh(*cmd, stdin=None):
    result = subprocess.run(list(cmd), input=stdin, capture_output=True)
    if result.returncode != 0:
        sys.exit("run: %s failed: %s" % (" ".join(cmd), result.stderr.decode(errors="replace")[-500:]))
    return result.stdout


def read(path):
    with open(path, "rb") as f:
        return f.read()


def encode_all(tools, old, new, work):
    """Returns {tool: patch size}, after checking each patch decodes to new."""
    target = read(new)
    patch = os.path.join(work, "patch")
    out = os.path.join(work, "out")
    sizes = {}

    def check(tool, decoded):
        if decoded != target:
            sys.exit("run: %s patch for %s does not decode to the target" % (tool, new))

    for greedy in (False, True):
        tool = "tinydelta_greedy" if greedy else "tinydelta"
        if tool in tools:
            sh(*([TINYDELTA, "encode"] + (["-greedy"] if greedy else []) + [old, new, patch]))
            check(tool, sh(TINYDELTA, "decode", old, patch))
            sizes[tool] = os.path.getsize(patch)
    if "openvcdiff" in tools or "openvcdiff_zlib" in tools:
        sh(OPENVCDIFF, "encode", "-dictionary", old, "-target", new, "-delta", patch, "-target_matches")
        sh(OPENVCDIFF, "decode", "-dictionary", old, "-delta", patch, "-target", out)
        check("openvcdiff", read(out))
        sizes["openvcdiff"] = os.path.getsize(patch)
        sizes["openvcdiff_zlib"] = len(zlib.compress(read(patch), 9))
    if "xdelta3" in tools:
        sh("xdelta3", "-e", "-f", "-9", "-S", "none", "-s", old, new, patch)
        sh("xdelta3", "-d", "-f", "-s", old, patch, out)
        check("xdelta3", read(out))
        sizes["xdelta3"] = os.path.getsize(patch)
    if "bsdiff" in tools:
        sh("bsdiff", old, new, patch)
        sh("bspatch", old, out, patch)
        check("bsdiff", read(out))
        sizes["bsdiff"] = os.path.getsize(patch)
    return {t: sizes[t] for t in tools}


def first_line(*cmd):
    """The first line a command prints (stdout, else stderr), for versions."""
    result = subprocess.run(list(cmd), capture_output=True)
    text = (result.stdout or result.stderr).decode(errors="replace").strip()
    return text.splitlines()[0] if text else "unknown"


def tool_versions(tools):
    versions = {"tinydelta": first_line(TINYDELTA, "version")}
    if "openvcdiff" in tools or "openvcdiff_zlib" in tools:
        versions["openvcdiff"] = os.environ.get("OPENVCDIFF_VERSION", "unknown")
    if "xdelta3" in tools:
        versions["xdelta3"] = first_line("xdelta3", "-V").split(",")[0]  # drop the copyright
    if "bsdiff" in tools:
        versions["bsdiff"] = os.environ.get("BSDIFF_VERSION", "system package")
    return versions


def main():
    args = sys.argv[1:]
    tools = ALL_TOOLS
    if "--tools" in args:
        i = args.index("--tools")
        tools = [t for t in args[i + 1].split(",") if t]
        unknown = set(tools) - set(ALL_TOOLS)
        if unknown:
            sys.exit("run: unknown tools %s" % ", ".join(sorted(unknown)))
        del args[i:i + 2]
    if len(args) != 2:
        sys.exit(__doc__)
    cache, results = args
    corpus = json.load(open(os.environ.get("BENCH_CORPUS", os.path.join(HERE, "corpus.json"))))
    builds = os.path.join(cache, "builds")

    rows = []
    with tempfile.TemporaryDirectory(prefix="tinydelta-bench-") as work:
        for name in sorted(os.listdir(builds)):
            kind = corpus[name]["kind"]
            for item in sorted(os.listdir(os.path.join(builds, name))):
                folder = os.path.join(builds, name, item)
                versions = json.load(open(os.path.join(folder, "versions.json")))
                for step in corpus["steps"]:
                    for i in range(len(versions) - step):
                        a, b = versions[i], versions[i + step]
                        old, new = os.path.join(folder, a["file"]), os.path.join(folder, b["file"])
                        row = {"corpus": name, "kind": kind, "item": item, "step": step,
                               "from": a["commit"][:12], "to": b["commit"][:12],
                               "dict_bytes": a["size"], "target_bytes": b["size"]}
                        row.update(encode_all(tools, old, new, work))
                        rows.append(row)
                print("%s/%s: %d versions" % (name, item, len(versions)), flush=True)

    os.makedirs(os.path.dirname(os.path.abspath(results)), exist_ok=True)
    with open(results, "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0]))
        w.writeheader()
        w.writerows(rows)
    with open(os.path.splitext(results)[0] + "-tools.json", "w") as f:
        json.dump(tool_versions(tools), f, indent=1)
    print("%d pairs -> %s" % (len(rows), results))


if __name__ == "__main__":
    main()
