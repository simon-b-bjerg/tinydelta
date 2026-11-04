#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

"""Turns the benchmark CSV into a summary and graphs.

    python3 bench/plot.py <results.csv> <output dir>

Writes to <output dir>:
    summary.md        per kind of data and commit step: how tinydelta's patch
                      size compares with each other tool (geometric mean,
                      median, 10th/90th percentile of the size ratio, share
                      of pairs where tinydelta is smaller), and each tool's
                      patch size as a share of the target
    forest.svg        the headline: per kind of data and tool, the geometric
                      mean of tinydelta's patch size divided by the tool's,
                      with its 10th-90th percentile range, for each step
    (forest, pairs and tradeoff leave out SECONDARY_KINDS)
    pairs.svg         every pair, one row per kind of data: tinydelta's patch
                      size against open-vcdiff's, HPatchLite's (4 KB),
                      detools', bsdiff's and zstd's; coded on the left,
                      without the coder on the right
    tradeoff.svg      each tool's typical patch size against its decoder's
                      footprint, code + RAM buffers, stack excluded
                      (from bench/decoders.json)
    ratio_ecdf_<kind>.svg  distribution of tinydelta's patch size divided by
                      each other tool's, one graph per kind of data
    patch_share_<kind>.svg each tool's patch size as a share of the target,
                      one graph per kind of data
Each graph is also written as a PNG for slides. Every comparison uses one
baseline, "tinydelta": the coded patch with 777 B of coder tables (the
smaller of the plain patch and the coded ones that fit, as the encoder
sends it), when measured; else the smaller of the plain patch (the
optimal encoder's, when measured) and the two-pass one. The trade-off
graph shows both, each with its decoder's footprint; every variant is
listed on its own in the patch-size tables.
Needs matplotlib.
"""

import csv
import json
import math
import os
import statistics
import sys

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402
from matplotlib import ticker  # noqa: E402
from matplotlib.transforms import Bbox  # noqa: E402

plt.rcParams.update({
    # Text stays text in the SVGs, so browsers draw it hinted and sharp.
    # Layout is measured with DejaVu Sans, which ships with matplotlib, so
    # it is the same on every machine; a viewer without it falls back to
    # Arial or Helvetica, which are narrower, so nothing collides.
    "svg.fonttype": "none",
    "font.family": "sans-serif",
    "font.sans-serif": ["DejaVu Sans", "Arial", "Helvetica"],
    # The same input gives byte-identical SVGs.
    "svg.hashsalt": "tinydelta",
})

# The other tools in a fixed order: categorical slots 1-8 (validated with
# the dataviz palette checks), a line style each, and whether the patch is
# compressed (which groups the distribution graph's rows).
OTHERS = [
    ("openvcdiff", "open-vcdiff", "#2a78d6", "-", False),
    ("openvcdiff_zlib", "open-vcdiff, zlib-compressed", "#eb6834", "--", True),
    ("xdelta3", "xdelta3", "#1baf7a", "-.", False),
    ("bsdiff", "bsdiff", "#eda100", ":", True),
    ("detools", "detools (heatshrink)", "#e87ba4", "-", True),
    ("hpatchlite_4k", "HPatchLite, 4 KB dict.", "#008300", "--", True),
    ("hpatchlite_32k", "HPatchLite, 32 KB dict.", "#6250d6", "-.", True),
    ("zstd", "zstd --patch-from", "#e34948", ":", True),
]
SURFACE, INK, MUTED, GRID = "#fcfcfb", "#0b0b0b", "#6b6a64", "#e4e3de"
# tinydelta without the coder: per pair, the smaller of the plain patch
# and the two-pass one (a column load() adds).
BEST = "tinydelta_best"
CZ = "tinydelta_cz_twice"
# tinydelta with the coder, the one every comparison uses when measured.
CODED = "tinydelta_coded_b"
# tinydelta's variants, the comparison's first; the others appear in the
# patch-size tables.
TINY_VARIANTS = [
    (CODED, "tinydelta, coded, 777 B tables", INK),
    (BEST, "tinydelta, plain or two-pass", "#8a8983"),
    ("tinydelta_optimal", "tinydelta, plain (optimal)", "#8a8983"),
    ("tinydelta", "tinydelta, plain", "#8a8983"),
    ("tinydelta_greedy", "tinydelta, plain (greedy)", "#8a8983"),
    (CZ, "tinydelta, two-pass", "#8a8983"),
    ("tinydelta_cz_twice_1k", "tinydelta, two-pass, 1 KB window", "#8a8983"),
    ("tinydelta_coded_c", "tinydelta, coded, 522 B tables", "#8a8983"),
    ("tinydelta_coded_a", "tinydelta, coded, 1287 B tables", "#8a8983"),
    ("tinydelta_reloc", "tinydelta, coded and relocated (firmware), 1545 B tables", "#8a8983"),
    ("tinydelta_reloc_shipped", "tinydelta, coded and relocated, rule table in the patch", "#8a8983"),
    ("tinydelta_window_256", "tinydelta, plain, copies at most 256 B back", "#8a8983"),
    ("tinydelta_window_1k", "tinydelta, plain, copies at most 1 KB back", "#8a8983"),
    ("tinydelta_window_4k", "tinydelta, plain, copies at most 4 KB back", "#8a8983"),
]


def load(path):
    with open(path, newline="") as f:
        rows = list(csv.DictReader(f))
    for r in rows:
        for k, v in r.items():
            if k not in ("corpus", "kind", "item", "from", "to"):
                r[k] = int(v)
    if rows and CZ in rows[0]:
        plain = plain_key(rows)
        for r in rows:
            r[BEST] = min(r[plain], r[CZ])
    return rows


def percentile(values, p):
    s = sorted(values)
    k = (len(s) - 1) * p / 100
    lo, hi = math.floor(k), math.ceil(k)
    return s[lo] + (s[hi] - s[lo]) * (k - lo)


def geomean(values):
    return math.exp(sum(math.log(v) for v in values) / len(values))


# The kinds of data, in the order the graphs show them; others after, by
# name. Each kind's title gives the size of its files.
KIND_ORDER = ["firmware", "configuration", "large configuration"]
# Kinds left out of the headline graphs (forest, pairs, tradeoff): larger
# than what tinydelta is made for. They stay in the summary and in their
# own distribution graphs.
SECONDARY_KINDS = ["large configuration"]


def kinds_of(rows):
    kinds = {r["kind"] for r in rows}
    return [k for k in KIND_ORDER if k in kinds] + sorted(kinds - set(KIND_ORDER))


def kb(n):
    return "%.1f" % (n / 1e3) if n < 10e3 else "%d" % round(n / 1e3)


def kind_label(rows, kind):
    """A kind of data with the size of its files: "config file, 3.1-5.8 KB"."""
    sizes = [r["target_bytes"] for r in rows if r["kind"] == kind]
    lo, hi = kb(min(sizes)), kb(max(sizes))
    return "%s, %s KB" % (kind, lo if lo == hi else lo + "–" + hi)


def plain_key(rows):
    """tinydelta's plain patch: optimal at any size if measured, else the
    library's default."""
    return "tinydelta_optimal" if "tinydelta_optimal" in rows[0] else "tinydelta"


def tiny_key(rows):
    """The tinydelta column the comparisons use: coded when measured, else
    the smaller of plain and two-pass when both are, else plain."""
    return CODED if CODED in rows[0] else uncoded_key(rows)


def uncoded_key(rows):
    """tinydelta without the coder: the smaller of plain and two-pass when
    both are measured, else plain."""
    return BEST if BEST in rows[0] else plain_key(rows)


def ratios(rows, other, tiny):
    # Both sizes are at least a few bytes for real pairs; guard anyway.
    return [max(r[tiny], 1) / max(r[other], 1) for r in rows if other in r and tiny in r]


def stats(rs):
    return "%.2f | %.2f | %.2f-%.2f | %d%%" % (
        geomean(rs), statistics.median(rs), percentile(rs, 10), percentile(rs, 90),
        round(100 * sum(1 for x in rs if x < 1) / len(rs)))


def log_axis(axis, unit=""):
    """Log scale with plain labels at 1-2-5 steps (0.2, 0.5, 1, 2...)."""
    axis.set_major_locator(ticker.LogLocator(base=10, subs=(1.0, 2.0, 5.0)))
    axis.set_major_formatter(ticker.FuncFormatter(lambda x, _: "%g%s" % (x, unit)))
    axis.set_minor_formatter(ticker.NullFormatter())


def style(ax):
    ax.set_facecolor(SURFACE)
    for side in ("top", "right"):
        ax.spines[side].set_visible(False)
    for side in ("left", "bottom"):
        ax.spines[side].set_color(GRID)
    ax.tick_params(colors=MUTED, labelsize=9)
    ax.grid(True, color=GRID, linewidth=0.8)
    ax.set_axisbelow(True)


def save(fig, out, name):
    """Writes a figure as SVG, for the repository, and PNG, for slides."""
    fig.savefig(os.path.join(out, name + ".svg"), facecolor=SURFACE, metadata={"Date": None})
    fig.savefig(os.path.join(out, name + ".png"), facecolor=SURFACE, dpi=200)
    plt.close(fig)


def against(rows, compressed):
    """The tinydelta column every tool is compared with: one baseline, the
    patch as sent (compressed is kept for the callers' grouping only)."""
    return tiny_key(rows)


def summary(rows, tools_file, out):
    kinds = kinds_of(rows)
    lines = ["# tinydelta patch sizes", ""]
    if os.path.exists(tools_file):
        versions = json.load(open(tools_file))
        lines += ["Tools: " + "; ".join("%s (%s)" % (k, v) for k, v in versions.items()), ""]
    tiny = tiny_key(rows)
    plain = "the optimal encoder at any size" if plain_key(rows) == "tinydelta_optimal" else "the library default"
    if tiny == CODED:
        what = ("tinydelta is its coded patch with 777 B of coder tables: per pair, the smaller of the "
                "plain patch and the coded ones that fit, as the encoder sends it. Every variant is "
                "listed on its own in the patch-size tables.")
    elif tiny == BEST:
        what = ("tinydelta is, per pair, the smaller of its plain patch (%s) and its two-pass "
                "patch, the one an update server would send. Both are listed on their own in the "
                "patch-size tables." % plain)
    else:
        what = "tinydelta is %s, as it is sent." % plain
    lines += [
        "Ratio = tinydelta's patch size / the other tool's; below 1, tinydelta is smaller. "
        "Geometric mean is the right average for ratios; \"smaller\" is the share of pairs where "
        "tinydelta is smaller. " + what,
        "",
    ]
    for kind in kinds:
        for step in sorted({r["step"] for r in rows if r["kind"] == kind}):
            sel = [r for r in rows if r["kind"] == kind and r["step"] == step]
            items = sorted({r["corpus"] + "/" + r["item"] for r in sel})
            lines += ["## %s, %d commit%s apart" % (kind_label(rows, kind), step, "" if step == 1 else "s"), "",
                      "%d pairs from %s; median target %s bytes." % (
                          len(sel), ", ".join(items), "{:,}".format(int(statistics.median(r["target_bytes"] for r in sel)))),
                      ""]
            if BEST in sel[0]:
                lines += ["Two-pass is the smaller in %d%% of these pairs." % round(
                    100 * sum(r[CZ] < r[plain_key(rows)] for r in sel) / len(sel)), ""]
            lines += [
                      "| tinydelta vs | geo. mean | median | p10-p90 | tinydelta smaller |",
                      "|---|---:|---:|---:|---:|"]
            for key, label, _, _, _ in OTHERS:
                rs = ratios(sel, key, tiny)
                if rs:
                    lines.append("| %s | %s |" % (label, stats(rs)))
            lines += ["", "Patch size as a share of the target (median, p10-p90):", "",
                      "| tool | median | p10 | p90 |", "|---|---:|---:|---:|"]
            for key, label in [(k, l) for k, l, _ in TINY_VARIANTS] + [(k, l) for k, l, _, _, _ in OTHERS]:
                if key not in sel[0]:
                    continue
                share = [100 * r[key] / max(r["target_bytes"], 1) for r in sel]
                lines.append("| %s | %.2f%% | %.2f%% | %.2f%% |" % (
                    label, statistics.median(share), percentile(share, 10), percentile(share, 90)))
            lines += ["", "Patch size in bytes (median, p10-p90), what goes over the air:", "",
                      "| tool | median | p10 | p90 |", "|---|---:|---:|---:|"]
            for key, label in [(k, l) for k, l, _ in TINY_VARIANTS] + [(k, l) for k, l, _, _, _ in OTHERS]:
                if key not in sel[0]:
                    continue
                size = [r[key] for r in sel]
                lines.append("| %s | {:,} | {:,} | {:,} |".format(
                    round(statistics.median(size)), round(percentile(size, 10)), round(percentile(size, 90))) % label)
            lines.append("")
    lines += relocation_summary(rows)
    with open(os.path.join(out, "summary.md"), "w") as f:
        f.write("\n".join(lines))


def relocation_summary(rows):
    """Firmware with relocation rules (corpus.json "relocation"): per item
    and commit step, the patches summed, coded and coded and relocated."""
    corpus_file = os.environ.get("BENCH_CORPUS", os.path.join(os.path.dirname(os.path.abspath(__file__)), "corpus.json"))
    corpus = json.load(open(corpus_file)) if os.path.exists(corpus_file) else {}
    relocated = {n for n, c in corpus.items() if isinstance(c, dict) and "relocation" in c}
    sel = [r for r in rows if r["corpus"] in relocated and "tinydelta_reloc" in r]
    if not sel:
        return []
    cols = [k for k in ("tinydelta_coded_b", "tinydelta_coded_a", "tinydelta_reloc", "tinydelta_reloc_shipped") if k in sel[0]]
    labels = {"tinydelta_coded_b": "coded, 777 B", "tinydelta_coded_a": "coded, 1287 B",
              "tinydelta_reloc": "relocated, 1545 B", "tinydelta_reloc_shipped": "relocated, table shipped"}
    lines = ["## Relocation", "",
             "Firmware with relocation rules: patch sizes summed over every pair, in bytes, map and "
             "header included. Tables are the decoder's tables buffer: the coder's tables, and for "
             "relocation its map (12 B per entry), plus the rule table when the patch ships it. "
             "The change is relocated against coded with 1287 B of tables.", "",
             "| item | rules, base | step | pairs | " + " | ".join(labels[c] for c in cols) + " | change |",
             "|---|---|---:|---:|" + "---:|" * len(cols) + "---:|"]
    for key in sorted({(r["corpus"], r["item"], r["step"]) for r in sel}):
        group = [r for r in sel if (r["corpus"], r["item"], r["step"]) == key]
        sums = {c: sum(r[c] for r in group) for c in cols}
        reloc = corpus[key[0]]["relocation"]
        base = sums.get("tinydelta_coded_a") or sums.get("tinydelta_coded_b")
        change = "%+.0f%%" % (100 * (sums["tinydelta_reloc"] - base) / base) if base else ""
        lines.append("| %s/%s | %s, %s | %d | %d | %s | %s |" % (
            key[0], key[1], reloc["rules"], reloc["base"], key[2], len(group),
            " | ".join("{:,}".format(sums[c]) for c in cols), change))
    return lines + [""]


# The comparisons the distribution graphs show: the VCDIFF reference, the
# closest embedded alternative, and the strongest on size. The rest is in
# the forest graph.
KEY_TOOLS = ["openvcdiff", "hpatchlite_4k", "bsdiff"]


def slug(kind):
    """A kind of data as part of a file name: "device tree" -> "device_tree"."""
    return kind.replace(" ", "_")


def titled(fig, title, subtitle):
    """Title and reading line, placed in inches from the top so they hold
    for any figure height; leaves the rest to the axes."""
    height = fig.get_figheight()
    fig.text(0.02, 1 - 0.15 / height, title, ha="left", va="top", fontsize=13, color=INK)
    fig.text(0.02, 1 - 0.45 / height, subtitle, ha="left", va="top", fontsize=9, color=MUTED)
    fig.tight_layout(rect=(0, 0, 1, 1 - 0.7 / height))


def ratio_ecdf(rows, out):
    """Every pair, one graph per kind: for each ratio along the x axis, the
    share of pairs at or below it. Where a line crosses 1 is the share of
    pairs where tinydelta is smaller; the legend states it."""
    tiny = tiny_key(rows)
    tools = [t for t in OTHERS if t[0] in KEY_TOOLS and t[0] in rows[0]]
    for kind in kinds_of(rows):
        sel = [r for r in rows if r["kind"] == kind]
        fig, ax = plt.subplots(figsize=(7.2, 4.8), facecolor=SURFACE)
        style(ax)
        ax.axvline(1, color=MUTED, linewidth=1.2)
        for key, label, color, dash, _ in tools:
            rs = sorted(ratios(sel, key, tiny))
            if not rs:
                continue
            ys = [100 * (i + 1) / len(rs) for i in range(len(rs))]
            wins = round(100 * sum(1 for x in rs if x < 1) / len(rs))
            ax.step(rs, ys, where="post", color=color, linestyle=dash, linewidth=2,
                    label="%s: tinydelta smaller in %d%%" % (label, wins))
        ax.set_xscale("log")
        log_axis(ax.xaxis)
        ax.set_ylim(0, 100)
        ax.set_xlabel("tinydelta's patch size ÷ the tool's (log scale)", fontsize=9, color=MUTED)
        ax.set_ylabel("% of pairs at or below this ratio", fontsize=9, color=MUTED)
        ax.legend(frameon=False, fontsize=9, loc="upper left", labelcolor=INK)
        titled(fig, "%s: how often, and by how much, tinydelta is smaller (%d pairs)" % (kind_label(rows, kind), len(sel)),
               "Each line climbs through every pair, smallest ratio first; its height at 1 is the share where tinydelta is smaller.")
        save(fig, out, "ratio_ecdf_" + slug(kind))


def patch_share(rows, out):
    """Each tool's patch size as a share of the target, one graph per kind."""
    # The copy-distance limits are in the summary only: they differ from
    # plain by about 1%, too little to see here.
    tools = ([(k, l, c) for k, l, c in TINY_VARIANTS if k in rows[0] and not k.startswith("tinydelta_window")] +
             [(k, l, c) for k, l, c, _, _ in OTHERS if k in rows[0]])
    for kind in kinds_of(rows):
        sel = [r for r in rows if r["kind"] == kind]
        fig, ax = plt.subplots(figsize=(8.4, 1.4 + 0.42 * len(tools)), facecolor=SURFACE)
        style(ax)
        data = [[100 * r[k] / max(r["target_bytes"], 1) for r in sel] for k, _, _ in tools]
        box = ax.boxplot(data, orientation="horizontal", widths=0.5, patch_artist=True, showfliers=True,
                         flierprops={"markersize": 3, "markeredgecolor": MUTED})
        for patch, (_, _, color) in zip(box["boxes"], tools):
            patch.set(facecolor=color, alpha=0.35, edgecolor=color)
        for median, (_, _, color) in zip(box["medians"], tools):
            median.set(color=color, linewidth=2)
        for line in box["whiskers"] + box["caps"]:
            line.set(color=MUTED)
        ax.set_yticks(range(1, len(tools) + 1), [label for _, label, _ in tools], fontsize=9, color=INK)
        ax.invert_yaxis()
        ax.set_xscale("log")
        # Decades only: the range spans five or more.
        ax.xaxis.set_major_locator(ticker.LogLocator(base=10))
        ax.xaxis.set_major_formatter(ticker.FuncFormatter(lambda x, _: "%g%%" % x))
        ax.xaxis.set_minor_formatter(ticker.NullFormatter())
        ax.set_xlabel("patch size as % of target (log scale)", fontsize=9, color=MUTED)
        titled(fig, "%s: patch size as a share of the target (%d pairs)" % (kind_label(rows, kind), len(sel)),
               "Box: middle half of the pairs, thick line the median; whiskers to 1.5 box lengths, dots beyond.")
        save(fig, out, "patch_share_" + slug(kind))


NICE = [m * 10 ** e for e in range(-3, 3) for m in (1, 2, 5)]  # 1-2-5 axis ends
# The forest axis never starts left of this: a tool far behind tinydelta
# (detools on firmware) would otherwise squeeze every other row; its mean
# is then marked at the axis edge with its value.
FOREST_FLOOR = 0.05


def forest_range(rows, tools, kinds, steps):
    """The forest graph's axis: 1-2-5 values just wide enough for every
    geometric mean and 90th percentile, and for 1, but starting no further
    left than FOREST_FLOOR (lines may run off the left, ending in an
    arrowhead)."""
    tiny = tiny_key(rows)
    lows, highs = [1.0], [1.0]
    for key, *_ in tools:
        for kind in kinds:
            for step in steps:
                rs = ratios([r for r in rows if r["kind"] == kind and r["step"] == step], key, tiny)
                if rs:
                    lows.append(geomean(rs))
                    highs.append(percentile(rs, 90))
    lo = max([v for v in NICE if v <= min(lows) / 1.15] + [FOREST_FLOOR])
    hi = min([v for v in NICE if v >= max(highs) * 1.15] or NICE[-1:])
    return lo, hi


# The forest graph's tools, in the README table's order: what a reader
# would otherwise use, the two embedded alternatives included. The rest
# (HPatchLite's 32 KB dictionary, close to its 4 KB one, and open-vcdiff's
# patch zlib-compressed) is in the summary and the other graphs.
FOREST_TOOLS = ["openvcdiff", "xdelta3", "hpatchlite_4k", "detools", "bsdiff", "zstd"]


def forest(rows, out):
    kinds = kinds_of(rows)
    steps = sorted({r["step"] for r in rows})
    others = {t[0]: t for t in OTHERS}
    tools = [others[k] for k in FOREST_TOOLS if k in rows[0]]
    FOREST_MIN, FOREST_MAX = forest_range(rows, tools, kinds, steps)
    fig, axes = plt.subplots(len(kinds), 1, figsize=(8.4, 1.6 + 0.48 * len(tools) * len(kinds) + 0.6 * len(kinds)),
                             squeeze=False, sharex=True, facecolor=SURFACE)
    offsets = {step: (i - (len(steps) - 1) / 2) * 0.22 for i, step in enumerate(steps)}
    for (ax,), kind in zip(axes, kinds):
        style(ax)
        ax.grid(True, axis="x", color=GRID, linewidth=0.8)
        ax.grid(False, axis="y")
        ax.axvline(1, color=MUTED, linewidth=1.2)
        labels = []
        for row, (key, label, color, _, compressed) in enumerate(tools):
            tiny = against(rows, compressed)
            labels.append(label)
            for step in steps:
                rs = ratios([r for r in rows if r["kind"] == kind and r["step"] == step], key, tiny)
                if not rs:
                    continue
                y = row + offsets[step]
                lo, hi = percentile(rs, 10), percentile(rs, 90)
                ax.plot([max(lo, FOREST_MIN), min(hi, FOREST_MAX)], [y, y], color=color, linewidth=2,
                        solid_capstyle="round")
                # A line running past the axis ends in an arrowhead there.
                if lo < FOREST_MIN:
                    ax.plot([FOREST_MIN], [y], marker="<", markersize=6, color=color)
                if hi > FOREST_MAX:
                    ax.plot([FOREST_MAX], [y], marker=">", markersize=6, color=color)
                filled = step == steps[0]
                mean = geomean(rs)
                if mean < FOREST_MIN:  # off the axis: marked at its edge, with the value
                    ax.annotate("mean %.2g" % mean, (FOREST_MIN, y), xytext=(8, 0), textcoords="offset points",
                                ha="left", va="center", fontsize=9, color=color,
                                bbox=dict(boxstyle="round,pad=0.2", facecolor=SURFACE, edgecolor=color, linewidth=0.8))
                    continue
                ax.plot([mean], [y], marker="o", markersize=7, color=color,
                        markerfacecolor=color if filled else SURFACE, markeredgewidth=2 if not filled else 1.5,
                        markeredgecolor=color if not filled else SURFACE)
        ax.set_yticks(range(len(tools)), labels, fontsize=9, color=INK)
        ax.invert_yaxis()
        ax.set_xscale("log")
        ax.set_xlim(FOREST_MIN, FOREST_MAX)
        log_axis(ax.xaxis)
        n = len([r for r in rows if r["kind"] == kind])
        ax.set_title("%s (%d pairs)" % (kind_label(rows, kind), n), fontsize=11, color=INK, loc="left")
    handles = [plt.Line2D([], [], marker="o", linestyle="", markersize=7, color=MUTED,
                          markerfacecolor=MUTED if step == steps[0] else SURFACE, markeredgewidth=2,
                          label="%d commit%s apart" % (step, "" if step == 1 else "s")) for step in steps]
    axes[0][0].legend(handles=handles, frameon=False, fontsize=9, loc="lower right", labelcolor=INK,
                      bbox_to_anchor=(1.0, 1.0), ncol=len(steps))
    axes[-1][0].set_xlabel("tinydelta's patch size ÷ the tool's (log scale)   ← tinydelta smaller · larger →\n"
                           "dot: geometric mean · line: 10th-90th percentile · arrowhead: it runs past the axis",
                           fontsize=9, color=MUTED, labelpad=8)
    # Title and reading line, placed in inches from the top so they hold
    # for any figure height.
    height = fig.get_figheight()
    fig.text(0.02, 1 - 0.15 / height, "tinydelta's patch size compared with other tools",
             ha="left", va="top", fontsize=13, color=INK)
    fig.text(0.02, 1 - 0.45 / height, "Each row: tinydelta's patch divided by that tool's, over every pair. "
             "Below 1, tinydelta's patch is smaller.", ha="left", va="top", fontsize=9, color=MUTED)
    fig.tight_layout(rect=(0, 0, 1, 1 - 0.7 / height))
    save(fig, out, "forest")


KIND_MARKERS = {"firmware": "o", "configuration": "^"}  # other kinds: "s"
# The pair graph's tools, one panel each: VCDIFF, the standard format
# (open-vcdiff), the two embedded alternatives, and the smallest patches of
# all (bsdiff). zstd, whose decoder needs the whole old file in RAM, is in
# the trade-off graph and the summary.
PAIR_TOOLS = ["openvcdiff", "hpatchlite_4k", "detools", "bsdiff"]
RELOC = "tinydelta_reloc"


def relocated_corpora():
    """The corpora with relocation rules (corpus.json "relocation"): the
    only ones whose relocated patches differ from the coded ones."""
    path = os.environ.get("BENCH_CORPUS", os.path.join(os.path.dirname(os.path.abspath(__file__)), "corpus.json"))
    corpus = json.load(open(path)) if os.path.exists(path) else {}
    return {n for n, c in corpus.items() if isinstance(c, dict) and "relocation" in c}


# Where the pair graph draws dashed lines: tinydelta's patch this share of the tool's.
PAIR_FRACTIONS = ((0.5, "½"), (0.25, "¼"), (0.125, "⅛"))


def pair_scatter(rows, out, name):
    """Every pair, as a matrix: a row per tinydelta variant measured (plain,
    coded with 777 B tables, coded + relocated for firmware with relocation
    rules), a column per tool (PAIR_TOOLS); that tool's patch size across,
    tinydelta's up, the same axes in every panel. Marker is the kind of
    data; in each panel, the share of pairs where tinydelta is smaller, and
    dashed lines where its patch is a half, a quarter and an eighth of the
    tool's."""
    relocated = relocated_corpora()
    variants = [(plain_key(rows), "plain", "#2a78d6", rows),
                (CODED, "coded", INK, rows),
                (RELOC, "coded + relocated", "#e8710a", [r for r in rows if r["corpus"] in relocated])]
    variants = [v for v in variants if v[3] and v[0] in v[3][0]]
    labels = {k: label for k, label, _, _, _ in OTHERS}
    tools = [k for k in PAIR_TOOLS if k in rows[0]]
    if not tools or not variants:
        return
    kinds = kinds_of(rows)
    sizes = [r[k] for k in tools for r in rows] + [r[v[0]] for v in variants for r in v[3]]
    lo, hi = max(1, min(sizes)) / 1.5, max(sizes) * 1.5
    decades = [10 ** e for e in range(math.ceil(math.log10(lo)), math.floor(math.log10(hi)) + 1, 2)]
    fig, axes = plt.subplots(len(variants), len(tools), figsize=(0.6 + 2.6 * len(tools), 1.5 + 2.6 * len(variants)),
                             squeeze=False, sharex=True, sharey=True, facecolor=SURFACE)
    for i, (key, label, color, sel) in enumerate(variants):
        for j, tool in enumerate(tools):
            ax = axes[i][j]
            style(ax)
            ax.plot([lo, hi], [lo, hi], color=MUTED, linewidth=0.9)
            for fraction, mark in PAIR_FRACTIONS:
                ax.plot([lo, hi], [lo * fraction, hi * fraction], color=MUTED, linewidth=0.6, linestyle=(0, (3, 3)), alpha=0.6)
                if i == 0 and j == len(tools) - 1:
                    ax.text(hi / 1.3, hi * fraction / 1.6, mark, color=MUTED, fontsize=8, ha="right", va="top")
            for kind in kinds:
                pick = [r for r in sel if r["kind"] == kind]
                ax.scatter([r[tool] for r in pick], [r[key] for r in pick], s=8, marker=KIND_MARKERS.get(kind, "s"),
                           color=color, alpha=0.55, linewidths=0)
            smaller = sum(r[key] < r[tool] for r in sel)
            ax.text(0.95, 0.05, "%d%%" % round(100 * smaller / len(sel)), transform=ax.transAxes, ha="right",
                    va="bottom", fontsize=10, color=INK, weight="bold")
            ax.set_xscale("log")
            ax.set_yscale("log")
            ax.set_xlim(lo, hi)
            ax.set_ylim(lo, hi)
            ax.set_aspect("equal")
            for axis in (ax.xaxis, ax.yaxis):
                axis.set_major_locator(ticker.FixedLocator(decades))
                axis.set_major_formatter(ticker.FuncFormatter(
                    lambda x, _: "%gK" % (x / 1000) if x >= 1000 else "%g" % x))
                axis.set_minor_formatter(ticker.NullFormatter())
            ax.tick_params(labelsize=8)
            if i == 0:
                ax.set_title(labels.get(tool, tool), fontsize=10.5, color=INK)
            if j == 0:
                ax.set_ylabel("tinydelta %s\n(%d pairs)" % (label, len(sel)), fontsize=9.5, color=color)
    height = fig.get_figheight()
    titled(fig, "tinydelta against other tools, every pair",
           "Rows: tinydelta's variants; columns: the tools. Patch sizes in bytes (log scale), below the line tinydelta's is "
           "smaller.\nBold: the share of pairs where it is. Dashed: tinydelta's patch a half, a quarter and an eighth of the "
           "tool's. ● firmware  ▲ configuration.")
    fig.tight_layout(rect=(0, 0, 1, 1 - 0.95 / height))
    save(fig, out, name)


def gap(extent, point):
    """Distance from a point to a label's box, in display units."""
    dx = max(extent.x0 - point[0], 0, point[0] - extent.x1)
    dy = max(extent.y0 - point[1], 0, point[1] - extent.y1)
    return (dx * dx + dy * dy) ** 0.5


def place_labels(fig, ax, points):
    """Labels each point at the first of a few positions around it where
    the label overlaps no other label, no point and stays inside the panel;
    the least crowded position otherwise."""
    fig.canvas.draw()
    renderer = fig.canvas.get_renderer()
    box = ax.get_window_extent(renderer)
    dots = [ax.transData.transform((x, y)) for x, y, _ in points]
    placed = []
    lines = []  # points along the leader lines drawn so far
    offsets = [(dx * (1 + (reach > 1)), dy * reach, ha, va) for reach in (1, 3, 5, 8, 12)
               for dx, dy, ha, va in [(8, 4, "left", "bottom"), (8, -4, "left", "top"),
                                      (-8, 4, "right", "bottom"), (-8, -4, "right", "top"),
                                      (0, 3, "center", "bottom"), (0, -3, "center", "top")]]
    # A point with another close by gets its label set off with a leader
    # line, so the line, not proximity, says which point it is. Those
    # labels can go almost anywhere, so they are placed last.
    def crowded_at(dot):
        return any(d is not dot and gap(Bbox([d, d]), dot) < 20 for d in dots)
    order = sorted(zip(points, dots), key=lambda pd: crowded_at(pd[1]))
    for (x, y, label), dot in order:
        best = None
        crowded = crowded_at(dot)
        for dx, dy, ha, va in (offsets[6:] if crowded else offsets):
            text = ax.annotate(label, (x, y), xytext=(dx, dy), textcoords="offset points",
                               ha=ha, va=va, fontsize=9, color=INK)
            extent = text.get_window_extent(renderer).expanded(1.05, 1.15)
            cost = 10 * sum(extent.overlaps(other) for other in placed)
            cost += 4 * sum(1 for d in dots if d is not dot and extent.contains(*d))
            # A label nearer another point than its own reads as that point's,
            # leader line or not: the worst place of all.
            own = gap(extent, dot)
            cost += 8 * any(gap(extent, d) < own for d in dots if d is not dot)
            # A leader line running through another label is hard to follow.
            corner = (extent.x0 if ha == "left" else extent.x1 if ha == "right" else (extent.x0 + extent.x1) / 2,
                      extent.y0 if va == "bottom" else extent.y1)
            line = [(dot[0] + (corner[0] - dot[0]) * t / 10, dot[1] + (corner[1] - dot[1]) * t / 10) for t in range(1, 10)]
            cost += 3 * sum(any(other.expanded(1.1, 2.0).contains(*q) for q in line) for other in placed)
            cost += 3 * any(extent.expanded(1.1, 2.0).contains(*q) for q in lines)
            cost += 0 if (extent.x0 >= box.x0 and extent.x1 <= box.x1 and extent.y0 >= box.y0
                          and extent.y1 <= box.y1) else 5
            if best is None or cost < best[0]:
                if best:
                    best[1].remove()
                best = (cost, text, extent)
            else:
                text.remove()
            if cost == 0:
                break
        cost, text, extent = best
        if crowded or abs(text.xyann[0]) > 8 or abs(text.xyann[1]) > 4:  # not right beside its point: a leader line
            ha, va = text.get_ha(), text.get_va()
            corner = (extent.x0 if ha == "left" else extent.x1 if ha == "right" else (extent.x0 + extent.x1) / 2,
                      extent.y0 if va == "bottom" else extent.y1)
            lines += [(dot[0] + (corner[0] - dot[0]) * t / 10, dot[1] + (corner[1] - dot[1]) * t / 10)
                      for t in range(1, 10)]
            text.remove()
            ax.annotate(label, (x, y), xytext=text.xyann, textcoords="offset points", ha=text.get_ha(),
                        va=text.get_va(), fontsize=9, color=INK,
                        arrowprops=dict(arrowstyle="-", color=MUTED, linewidth=0.6, shrinkA=0, shrinkB=4))
        placed.append(extent)


# The trade-off graph's footprint axis ends here; a decoder above it (bsdiff:
# its 2 MB+ of buffers) sits at the edge, labelled with its footprint.
TRADEOFF_XMAX = 500e3


def tradeoff(rows, out):
    """Firmware: typical patch size against the decoder's footprint on the
    device, code plus the RAM buffers it needs, stack excluded
    (decoders.json). tinydelta is a staircase of the decoders it can be
    built as: plain, coded, coded and relocating; two-pass hangs below plain,
    hollow: the same decoder, run twice, with the intermediate patch stored
    in between. For a decoder that holds the whole old file, the median old
    file is added. A point whose code size is unknown, or that holds the
    old file, is a lower bound, drawn as an arrow."""
    sel = [r for r in rows if r["kind"] == "firmware"]
    if not sel:
        return
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "decoders.json")
    decoders = {k: v for k, v in json.load(open(path)).items() if not k.startswith("_")}
    colors = {k: c for k, _, c, _, _ in OTHERS}
    colors["openvcdiff_interleaved"] = colors["openvcdiff"]  # tiny-vcdiff: the same format family
    # tinydelta's decoders (decoders.json) and the patches each decodes.
    stairs = [(k, c) for k, c in (("tinydelta", plain_key(rows)), ("tinydelta_coded", CODED), ("tinydelta_reloc", RELOC))
              if k in decoders and c in sel[0]]
    # HPatchLite's 32 KB dictionary is left out to keep the graph readable
    # (it is close to the 4 KB one); it is in the summary.
    others = [k for k in decoders if not k.startswith("tinydelta") and k in sel[0] and k != "hpatchlite_32k"]
    old = statistics.median(r["dict_bytes"] for r in sel)

    def footprint(key):
        d = decoders[key]
        return (d["code"] or 0) + d["buffers"] + (old if d["plus_old_file"] else 0)

    def share(column):
        return statistics.median(100 * r[column] / max(r["target_bytes"], 1) for r in sel)

    # One panel, at most ~830 px wide, the width of GitHub's README column.
    fig, ax = plt.subplots(figsize=(7.2, 5.9), facecolor=SURFACE)
    style(ax)
    points = []
    for key in others:
        d = decoders[key]
        lower_bound = d["code"] is None or d["plus_old_file"]
        x, y = footprint(key), share(key)
        label = d["label"] + (" (at least)" if lower_bound else "")
        if x > TRADEOFF_XMAX:  # off the axis (bsdiff's 2 MB+): at its edge, with the footprint
            x, label = TRADEOFF_XMAX, "%s, %s" % (label, "%.1f MB" % (footprint(key) / 1e6))
        ax.plot([x], [y], marker=">" if lower_bound else "o", markersize=9, color=colors.get(key, MUTED),
                markeredgecolor=SURFACE, markeredgewidth=1.5, linestyle="", clip_on=False)
        points.append((x, y, label))
    xs, ys = [footprint(k) for k, _ in stairs], [share(c) for _, c in stairs]
    ax.plot(xs, ys, color=INK, linewidth=2, zorder=3)
    ax.plot(xs, ys, marker="o", markersize=10, color=INK, markeredgecolor=SURFACE, markeredgewidth=1.5, linestyle="", zorder=4)
    points += [(x, y, decoders[k]["label"]) for (k, _), x, y in zip(stairs, xs, ys)]
    ax.set_xscale("log")
    ax.set_yscale("log")
    ax.margins(y=0.15)
    ax.set_xlim(min(p[0] for p in points) / 1.6, TRADEOFF_XMAX)
    place_labels(fig, ax, points)
    log_axis(ax.xaxis)
    ax.xaxis.set_major_formatter(ticker.FuncFormatter(
        lambda x, _: "%g MB" % (x / 1e6) if x >= 1e6 else "%g KB" % (x / 1e3) if x >= 1e3 else "%g B" % x))
    log_axis(ax.yaxis, "%")
    ax.set_title(kind_label(rows, "firmware") + " (%d pairs)" % len(sel), fontsize=11, color=INK, loc="left")
    ax.set_xlabel("decoder footprint: code + RAM buffers, stack excluded (log scale)", fontsize=9, color=MUTED)
    ax.set_ylabel("median patch size, % of target (log scale)", fontsize=9, color=MUTED)
    height = fig.get_figheight()
    fig.text(0.02, 1 - 0.15 / height, "Firmware: patch size against what the decoder costs on the device",
             ha="left", va="top", fontsize=13, color=INK)
    fig.text(0.02, 1 - 0.45 / height, "Lower left is better. Black: tinydelta's decoders, each step adding to the last.\n"
             "Arrows are lower bounds: code size unknown, or the whole old file in RAM; at the right edge, off the axis.", ha="left", va="top", fontsize=9, color=MUTED)
    if RELOC in sel[0]:
        fig.text(0.02, 0.012, "Relocated: firmware without relocation rules gets the coded patch.", ha="left",
                 va="bottom", fontsize=8.5, color=MUTED)
    fig.tight_layout(rect=(0, 0.03, 1, 1 - 0.95 / height))
    save(fig, out, "tradeoff")


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    results, out = sys.argv[1:]
    os.makedirs(out, exist_ok=True)
    rows = load(results)
    if not rows:
        sys.exit("plot: no rows in %s" % results)
    summary(rows, os.path.splitext(results)[0] + "-tools.json", out)
    primary = [r for r in rows if r["kind"] not in SECONDARY_KINDS] or rows
    forest(primary, out)
    pair_scatter(primary, out, "pairs")
    tradeoff(primary, out)
    ratio_ecdf(rows, out)
    patch_share(rows, out)


if __name__ == "__main__":
    main()
