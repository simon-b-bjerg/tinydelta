#!/usr/bin/env python3
"""Encodes every benchmark pair with every tool and writes the sizes.

    python3 bench/run.py <cache dir> <results.csv> [--tools a,b,...]

Pairs are versions <step> apart within one item (steps from corpus.json),
built by bench/collect.py, encoded in parallel ($BENCH_JOBS, default: one
per CPU). Every patch is decoded again and compared with the target, so a
tool that produces a wrong patch stops the run instead of reporting a size.

Tools (set the binaries with environment variables; defaults are on PATH):
    tinydelta          tinydelta encode, as the library does it: optimal,
                       falling back to greedy above roughly 100 KB   $TINYDELTA
    tinydelta_greedy   tinydelta encode -greedy
    tinydelta_optimal  optimal at any size, through the test binary from
                       `go test -c` (TestEncodeUnlimited)   $TINYDELTA_OPTIMAL
                       Not run by default: a few hundred MB and a minute or
                       more per firmware pair; add it with --tools.
    tinydelta_zlib     the tinydelta patch (library default), zlib level 9
    tinydelta_cz_zlib  tinydelta encode -for-compression (format version 2
                       where it compresses better), zlib level 9
    tinydelta_cz_tuz4k the same patch compressed with tinyuz -c-4k: the
                       compressor and dictionary HPatchLite 4k uses   $TINYUZ
    openvcdiff         open-vcdiff encode -target_matches     $OPENVCDIFF
    openvcdiff_zlib   the open-vcdiff patch, zlib level 9
    xdelta3           xdelta3 -9 -S none (VCDIFF, no secondary compression)
    bsdiff            bsdiff 4 (with bzip2 inside)
    detools           detools create_patch -c heatshrink (sequential, the
                      embedded bsdiff variant)                $DETOOLS
    hpatchlite_4k     HPatchLite hdiffi -c-tuz-4k (tinyuz, 4 KB dictionary:
                      the decoder's RAM)                       $HDIFFI, $HPATCHI
    hpatchlite_32k    HPatchLite hdiffi -c-tuz-32k (its default dictionary)
    zstd              zstd -19 --patch-from (a general reference: its decoder
                      needs the whole old file as its window)
"""

import concurrent.futures
import csv
import json
import os
import subprocess
import sys
import tempfile
import zlib

HERE = os.path.dirname(os.path.abspath(__file__))
TINYDELTA = os.environ.get("TINYDELTA", "tinydelta")
TINYDELTA_OPTIMAL = os.environ.get("TINYDELTA_OPTIMAL", "tinydelta-optimal.test")
OPENVCDIFF = os.environ.get("OPENVCDIFF", "vcdiff")
DETOOLS = os.environ.get("DETOOLS", "detools")
HDIFFI = os.environ.get("HDIFFI", "hdiffi")
HPATCHI = os.environ.get("HPATCHI", "hpatchi")
TINYUZ = os.environ.get("TINYUZ", "tinyuz")
ALL_TOOLS = ["tinydelta", "tinydelta_greedy", "tinydelta_optimal", "tinydelta_zlib",
             "tinydelta_cz_zlib", "tinydelta_cz_tuz4k",
             "openvcdiff", "openvcdiff_zlib", "xdelta3", "bsdiff",
             "detools", "hpatchlite_4k", "hpatchlite_32k", "zstd"]
# Run by default: all but tinydelta_optimal, which takes a few hundred MB
# and a minute or more per firmware pair (--tools adds it).
DEFAULT_TOOLS = [t for t in ALL_TOOLS if t != "tinydelta_optimal"]


class Failure(Exception):
    pass


def sh(*cmd, stdin=None, env=None):
    result = subprocess.run(list(cmd), input=stdin, capture_output=True, env=env)
    if result.returncode != 0:
        raise Failure("%s failed: %s" % (" ".join(cmd), (result.stderr or result.stdout).decode(errors="replace")[-500:]))
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
            raise Failure("%s patch for %s does not decode to the target" % (tool, new))

    for greedy in (False, True):
        tool = "tinydelta_greedy" if greedy else "tinydelta"
        if tool in tools or (tool == "tinydelta" and "tinydelta_zlib" in tools):
            sh(*([TINYDELTA, "encode"] + (["-greedy"] if greedy else []) + [old, new, patch]))
            check(tool, sh(TINYDELTA, "decode", old, patch))
            sizes[tool] = os.path.getsize(patch)
            if tool == "tinydelta":
                sizes["tinydelta_zlib"] = len(zlib.compress(read(patch), 9))
    if "tinydelta_cz_zlib" in tools or "tinydelta_cz_tuz4k" in tools:
        sh(TINYDELTA, "encode", "-for-compression", old, new, patch)
        check("tinydelta_cz", sh(TINYDELTA, "decode", old, patch))
        raw = read(patch)
        sizes["tinydelta_cz_zlib"] = len(zlib.compress(raw, 9))
        packed = os.path.join(work, "packed")
        fresh(packed, out)
        sh(TINYUZ, "-c-4k", patch, packed)
        sh(TINYUZ, "-d", packed, out)
        if read(out) != raw:
            raise Failure("tinyuz did not restore the tinydelta patch for %s" % new)
        sizes["tinydelta_cz_tuz4k"] = os.path.getsize(packed)
    if "tinydelta_optimal" in tools:
        env = dict(os.environ, TINYDELTA_ENCODE_UNLIMITED="%s %s %s" % (old, new, patch))
        sh(TINYDELTA_OPTIMAL, "-test.run", "^TestEncodeUnlimited$", env=env)
        check("tinydelta_optimal", sh(TINYDELTA, "decode", old, patch))
        sizes["tinydelta_optimal"] = os.path.getsize(patch)
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
    if "detools" in tools:
        fresh(patch, out)
        sh(DETOOLS, "create_patch", "-c", "heatshrink", old, new, patch)
        sh(DETOOLS, "apply_patch", old, patch, out)
        check("detools", read(out))
        sizes["detools"] = os.path.getsize(patch)
    for tool, dictionary in (("hpatchlite_4k", "4k"), ("hpatchlite_32k", "32k")):
        if tool in tools:
            fresh(patch, out)
            sh(HDIFFI, "-c-tuz-" + dictionary, old, new, patch)
            sh(HPATCHI, old, patch, out)
            check(tool, read(out))
            sizes[tool] = os.path.getsize(patch)
    if "zstd" in tools:
        sh("zstd", "-q", "-f", "-19", "--patch-from=" + old, new, "-o", patch)
        sh("zstd", "-q", "-f", "-d", "--patch-from=" + old, patch, "-o", out)
        check("zstd", read(out))
        sizes["zstd"] = os.path.getsize(patch)
    return {t: sizes[t] for t in tools}


def fresh(*paths):
    """Removes output files, for tools that refuse to overwrite them."""
    for path in paths:
        if os.path.exists(path):
            os.remove(path)


def encode_pair(tools, old, new):
    with tempfile.TemporaryDirectory(prefix="tinydelta-bench-") as work:
        return encode_all(tools, old, new, work)


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
    if "detools" in tools:
        versions["detools"] = "detools " + first_line(DETOOLS, "--version")
    if "hpatchlite_4k" in tools or "hpatchlite_32k" in tools:
        versions["hpatchlite"] = os.environ.get("HPATCHLITE_VERSION", "unknown")
    if "tinydelta_cz_tuz4k" in tools:
        versions["tinyuz"] = first_line(TINYUZ)
    if "zstd" in tools:
        versions["zstd"] = first_line("zstd", "--version").strip("* ").split(",")[0]
    return versions


def main():
    args = sys.argv[1:]
    tools = DEFAULT_TOOLS
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

    pairs = []
    for name in sorted(os.listdir(builds)):
        kind = corpus[name]["kind"]
        for item in sorted(os.listdir(os.path.join(builds, name))):
            folder = os.path.join(builds, name, item)
            index = os.path.join(folder, "versions.json")
            versions = json.load(open(index)) if os.path.exists(index) else []
            if len(versions) < 2:
                continue  # nothing to pair (reported by collect.py)
            for step in corpus["steps"]:
                for i in range(len(versions) - step):
                    a, b = versions[i], versions[i + step]
                    pairs.append(({"corpus": name, "kind": kind, "item": item, "step": step,
                                   "from": a["commit"][:12], "to": b["commit"][:12],
                                   "dict_bytes": a["size"], "target_bytes": b["size"]},
                                  os.path.join(folder, a["file"]), os.path.join(folder, b["file"])))

    jobs = int(os.environ.get("BENCH_JOBS", os.cpu_count() or 1))
    rows = [None] * len(pairs)
    with concurrent.futures.ProcessPoolExecutor(max_workers=jobs) as pool:
        futures = {pool.submit(encode_pair, tools, old, new): i for i, (_, old, new) in enumerate(pairs)}
        for done, future in enumerate(concurrent.futures.as_completed(futures), 1):
            i = futures[future]
            try:
                sizes = future.result()
            except Failure as e:
                pool.shutdown(cancel_futures=True)
                sys.exit("run: %s" % e)
            rows[i] = dict(pairs[i][0], **sizes)
            if done % 20 == 0 or done == len(pairs):
                print("%d/%d pairs" % (done, len(pairs)), flush=True)

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
