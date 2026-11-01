#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

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
        self.resume_at = 0  # index in the commit list to continue from (keep_only)

    def keep_only(self, commits):
        """Drops cached builds unless every one comes from this commit list,
        in its order: builds from an older corpus or walk would otherwise
        mix into the new series."""
        position = {c: i for i, c in enumerate(commits)}
        order = [position.get(v["commit"], -1) for v in self.versions]
        if -1 in order or order != sorted(order):
            print("%s: cached builds come from another commit list; rebuilding" % self.name, flush=True)
            for v in self.versions:
                try:
                    os.remove(os.path.join(self.dir, v["file"]))
                except FileNotFoundError:
                    pass
            self.versions, self.done_commits = [], set()
            if os.path.exists(self.index):
                os.remove(self.index)
        # Continue after the last cached build, never before it: commits
        # before it were already examined in order (a build identical to
        # the one before it is not recorded), and building them again
        # would append older versions after newer ones.
        self.resume_at = position[self.versions[-1]["commit"]] + 1 if self.versions else 0

    def pending(self, index, commit):
        """Whether the commit at index in the walk still needs building."""
        return not self.full() and index >= self.resume_at and commit not in self.done_commits

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


def run(cmd, cwd, log, env=None):
    with open(log, "a") as f:
        f.write("\n$ %s\n" % " ".join(cmd))
        f.flush()
        return subprocess.run(cmd, cwd=cwd, stdout=f, stderr=subprocess.STDOUT, env=env).returncode == 0


def collect_micropython(cache, name, spec):
    src = os.path.join(cache, "src", name)
    clone(spec["repo"], src)
    items = [(Item(cache, name, it["name"], spec["max_versions"]), it) for it in spec["items"]]
    log = os.path.join(cache, "build-%s.log" % name)
    commits = commits_touching(src, spec["from"], spec["to"], MICROPYTHON_PATHS)
    # The firmware embeds its version (git describe) and build date in its
    # banner. Both are fixed to the range's start, so a commit that leaves
    # the compiled code unchanged gives an identical image and is skipped,
    # instead of counting as an update of a few changed banner bytes.
    stamp = {"MICROPY_GIT_TAG": spec["from"], "MICROPY_GIT_HASH": "bench",
             "SOURCE_DATE_EPOCH": git(src, "log", "-1", "--format=%ct", spec["from"]).strip()}
    for item, _ in items:
        item.keep_only(commits)
    for index, commit in enumerate(commits):
        todo = [(item, it) for item, it in items if item.pending(index, commit)]
        if not todo:
            if all(item.full() for item, _ in items):
                break
            continue
        git(src, "checkout", "-q", "-f", commit)
        # Reproducible builds: the version stamp above, and a fixed seed for
        # Python's string hashes, by which the build scripts order some
        # tables.
        env = dict(os.environ, PYTHONHASHSEED="0", **stamp)
        if not run(["make", "-C", "mpy-cross", "-j", JOBS], src, log, env):
            for item, _ in todo:
                item.skipped.append(commit)
            continue
        for item, it in todo:
            port = os.path.join(src, "ports", it["port"])
            board = ["BOARD=" + it["board"]]
            build = os.path.join(port, "build-" + it["board"])
            shutil.rmtree(build, ignore_errors=True)
            ok = run(["make", "submodules"] + board, port, log, env) and run(["make", "-j", JOBS] + board, port, log, env)
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
        commits = commits_touching(src, spec["from"], spec["to"], [os.path.dirname(dts), "include"],
                                   first_parent=False)
        item.keep_only(commits)
        for index, commit in enumerate(commits):
            if item.full():
                break
            if not item.pending(index, commit):
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
                # Nothing to pair; run.py leaves it out.
                failed.append((name, item))
    annotate("notice", "Corpus", "\n".join(report))
    for name, item in failed:
        annotate("warning", "Left out: %s/%s" % (name, item.name),
                 "%d version(s) in the commit range, too few to compare. Build log (end):\n%s"
                 % (len(item.versions), tail(os.path.join(cache, "build-%s.log" % name))))
    if len(failed) == sum(len(corpus[n]["items"]) for n in collectors if n in corpus):
        sys.exit("collect: no item has two versions to compare")


if __name__ == "__main__":
    main()
