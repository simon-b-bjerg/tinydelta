#!/usr/bin/env python3
"""Builds the benchmark versions listed in bench/corpus.json.

    python3 bench/collect.py [cache dir]        (default bench/.cache)

For each corpus it clones the repository (partially: blobs are fetched
only as needed), walks the commits in the configured range that touch the
item's sources, builds the item at each one, and keeps every build whose
output differs from the previous one, up to max_versions per item:

    <cache>/builds/<corpus>/<item>/<NN>-<commit>.bin
    <cache>/builds/<corpus>/<item>/versions.json   (commit, file, size, sha256)

Commits that fail to build are logged and skipped. Needs git; for
MicroPython arm-none-eabi-gcc and make; for device trees cpp and dtc.
"""

import hashlib
import json
import os
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
JOBS = str(os.cpu_count() or 2)

# Paths whose changes can change a MicroPython stm32 build.
MICROPYTHON_PATHS = ["py", "extmod", "shared", "drivers", "lib", "ports/stm32", "mpy-cross"]


def git(src, *args, capture=True):
    result = subprocess.run(["git", "-C", src] + list(args), check=True, text=True,
                            stdout=subprocess.PIPE if capture else None)
    return result.stdout.strip() if capture else ""


def clone(repo, src):
    if not os.path.isdir(os.path.join(src, ".git")):
        subprocess.run(["git", "clone", "-q", "--filter=blob:none", "--no-checkout", repo, src], check=True)
    git(src, "fetch", "-q", "--tags", "origin")


def commits_touching(src, first, last, paths, first_parent=True):
    """Commits in first..last that change paths, oldest first. With
    first_parent, only the main line; without, every non-merge commit, in
    topological order (a branch's commits stay together)."""
    walk = ["--first-parent"] if first_parent else ["--no-merges", "--topo-order"]
    out = git(src, "rev-list", "--reverse", *walk, "%s..%s" % (first, last), "--", *paths)
    return out.split() if out else []


class Item:
    """Collects the distinct builds of one item."""

    def __init__(self, cache, corpus, name, max_versions):
        self.dir = os.path.join(cache, "builds", corpus, name)
        self.name = name
        self.max_versions = max_versions
        os.makedirs(self.dir, exist_ok=True)
        self.index = os.path.join(self.dir, "versions.json")
        self.versions = json.load(open(self.index)) if os.path.exists(self.index) else []
        self.done_commits = {v["commit"] for v in self.versions}
        self.skipped = []

    def full(self):
        return len(self.versions) >= self.max_versions

    def add(self, commit, data):
        digest = hashlib.sha256(data).hexdigest()
        if self.versions and self.versions[-1]["sha256"] == digest:
            return False  # unchanged by this commit
        name = "%02d-%s.bin" % (len(self.versions), commit[:12])
        with open(os.path.join(self.dir, name), "wb") as f:
            f.write(data)
        self.versions.append({"commit": commit, "file": name, "size": len(data), "sha256": digest})
        self.done_commits.add(commit)
        with open(self.index, "w") as f:
            json.dump(self.versions, f, indent=1)
        return True


def run(cmd, cwd, log):
    with open(log, "a") as f:
        f.write("\n$ %s\n" % " ".join(cmd))
        f.flush()
        return subprocess.run(cmd, cwd=cwd, stdout=f, stderr=subprocess.STDOUT).returncode == 0


def collect_micropython(cache, name, spec):
    src = os.path.join(cache, "src", name)
    clone(spec["repo"], src)
    items = [(Item(cache, name, it["name"], spec["max_versions"]), it) for it in spec["items"]]
    log = os.path.join(cache, "build-%s.log" % name)
    for commit in commits_touching(src, spec["from"], spec["to"], MICROPYTHON_PATHS):
        todo = [(item, it) for item, it in items if not item.full() and commit not in item.done_commits]
        if not todo:
            if all(item.full() for item, _ in items):
                break
            continue
        git(src, "checkout", "-q", "-f", commit)
        if not run(["make", "-C", "mpy-cross", "-j", JOBS], src, log):
            for item, _ in todo:
                item.skipped.append(commit)
            continue
        for item, it in todo:
            port = os.path.join(src, "ports", it["port"])
            board = ["BOARD=" + it["board"]]
            build = os.path.join(port, "build-" + it["board"])
            shutil.rmtree(build, ignore_errors=True)
            ok = run(["make", "submodules"] + board, port, log) and run(["make", "-j", JOBS] + board, port, log)
            parts = sorted(f for f in os.listdir(build) if f.startswith("firmware") and f.endswith(".bin")) if ok else []
            # Boards with separate flash regions get firmware0.bin, firmware1.bin...;
            # the image compared is their concatenation, as flashed.
            numbered = [f for f in parts if f[len("firmware"):-len(".bin")].isdigit()]
            parts = numbered or [f for f in parts if f == "firmware.bin"]
            if not parts:
                item.skipped.append(commit)
                continue
            item.add(commit, b"".join(open(os.path.join(build, f), "rb").read() for f in parts))
        print("%s %s: %s" % (name, commit[:12], ", ".join("%s %d" % (i.name, len(i.versions)) for i, _ in items)), flush=True)
    return [item for item, _ in items]


def collect_dtb(cache, name, spec):
    src = os.path.join(cache, "src", name)
    clone(spec["repo"], src)
    log = os.path.join(cache, "build-%s.log" % name)
    items = []
    for it in spec["items"]:
        item = Item(cache, name, it["name"], spec["max_versions"])
        items.append(item)
        dts = it["dts"]
        # Not first-parent: the kernel's main line brings a release's device
        # tree changes in one or two large merges, so its steps are huge.
        for commit in commits_touching(src, spec["from"], spec["to"], [os.path.dirname(dts), "include"],
                                       first_parent=False):
            if item.full():
                break
            if commit in item.done_commits:
                continue
            git(src, "checkout", "-q", "-f", commit)
            if not os.path.exists(os.path.join(src, dts)):
                item.skipped.append(commit)
                continue
            out = os.path.join(cache, "out.dtb")
            includes = ["-I", "include", "-I", "src", "-I", os.path.dirname(dts)]
            pre = subprocess.run(["cpp", "-nostdinc", "-undef", "-D__DTS__", "-x", "assembler-with-cpp"]
                                 + includes + [dts], cwd=src, capture_output=True)
            ok = pre.returncode == 0 and subprocess.run(
                ["dtc", "-q", "-I", "dts", "-O", "dtb", "-i", os.path.dirname(dts), "-i", "src", "-o", out, "-"],
                cwd=src, input=pre.stdout, capture_output=True).returncode == 0
            if not ok:
                with open(log, "a") as f:
                    f.write("%s %s: build failed\n%s\n" % (it["name"], commit, pre.stderr.decode(errors="replace")[-2000:]))
                item.skipped.append(commit)
                continue
            item.add(commit, open(out, "rb").read())
        print("%s %s: %d versions, %d commits skipped" % (name, item.name, len(item.versions), len(item.skipped)), flush=True)
    return items


def annotate(level, title, text):
    """Prints a GitHub Actions annotation (harmless elsewhere)."""
    print("::%s title=%s::%s" % (level, title, text.replace("%", "%25").replace("\n", "%0A")), flush=True)


def tail(path, lines=40):
    if not os.path.exists(path):
        return "(no log)"
    with open(path, errors="replace") as f:
        return "".join(f.readlines()[-lines:])


def main():
    cache = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, ".cache"))
    corpus = json.load(open(os.environ.get("BENCH_CORPUS", os.path.join(HERE, "corpus.json"))))
    collectors = {"micropython": collect_micropython, "dtb": collect_dtb}
    report, failed = [], []
    for name, collector in collectors.items():
        if name not in corpus:
            continue
        for item in collector(cache, name, corpus[name]):
            report.append("%s/%s: %d versions, %d commits skipped (did not build)%s" % (
                name, item.name, len(item.versions), len(item.skipped),
                ": " + " ".join(c[:12] for c in item.skipped[:10]) if item.skipped else ""))
            if len(item.versions) < 2:
                failed.append(name)
    annotate("notice", "Corpus", "\n".join(report))
    if failed:
        for name in sorted(set(failed)):
            annotate("error", "Build log of %s (end)" % name, tail(os.path.join(cache, "build-%s.log" % name)))
        sys.exit("collect: too few versions to compare; see the build logs in %s" % cache)


if __name__ == "__main__":
    main()
