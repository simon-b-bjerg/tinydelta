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
    pairs.svg         every pair, one panel per kind of data: tinydelta's patch
                      size against open-vcdiff's, HPatchLite's (4 KB),
                      detools', bsdiff's and zstd's
    tradeoff.svg      each tool's typical patch size against its decoder's
                      footprint, code + RAM buffers, stack excluded
                      (from bench/decoders.json)
    ratio_ecdf_<kind>.svg  distribution of tinydelta's patch size divided by
                      each other tool's, one graph per kind of data
    patch_share_<kind>.svg each tool's patch size as a share of the target,
                      one graph per kind of data
Each graph is also written as a PNG for slides. Every comparison uses one
baseline, "tinydelta": per pair, the smaller of tinydelta's plain patch
(the optimal encoder's, when measured) and its compression-mode patch,
the one an update server would send. Both are listed on their own in
the patch-size tables.
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
# The one tinydelta every comparison uses: per pair, the smaller of the
# plain patch and compression mode decoded twice (a column load() adds).
BEST = "tinydelta_best"
CZ = "tinydelta_cz_twice"
# tinydelta's variants, the comparison's first; the others appear in the
# patch-size tables.
TINY_VARIANTS = [
    (BEST, "tinydelta", INK),
    ("tinydelta_optimal", "tinydelta, plain (optimal)", "#8a8983"),
    ("tinydelta", "tinydelta, plain", "#8a8983"),
    ("tinydelta_greedy", "tinydelta, plain (greedy)", "#8a8983"),
    (CZ, "tinydelta, compression mode, decoded twice", "#8a8983"),
    ("tinydelta_cz_twice_1k", "tinydelta, compression mode, decoded twice, 1 KB window", "#8a8983"),
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
    """The tinydelta column the comparisons use: the smaller of plain and
    compression mode when both are measured, else plain."""
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
    if tiny == BEST:
        what = ("tinydelta is, per pair, the smaller of its plain patch (%s) and its compression-mode "
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
            if tiny == BEST:
                lines += ["Compression mode is the smaller in %d%% of these pairs." % round(
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
    with open(os.path.join(out, "summary.md"), "w") as f:
        f.write("\n".join(lines))


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


STEP_MARKERS = ["o", "^", "s", "D"]  # by commit step: 1 apart, 5 apart, ...
# The pair graph's tools: the VCDIFF reference, the two embedded
# alternatives, and the strongest compressors on size.
PAIR_TOOLS = ["openvcdiff", "hpatchlite_4k", "detools", "bsdiff", "zstd"]


def pair_scatter(rows, out, name):
    """Every pair, once per tool, one panel per kind of data: that tool's
    patch size across, tinydelta's up. Colour is the tool, marker the
    commit step."""
    tiny = tiny_key(rows)
    tools = [t for t in OTHERS if t[0] in PAIR_TOOLS and t[0] in rows[0]]
    if not tools:
        return
    kinds = kinds_of(rows)
    steps = sorted({r["step"] for r in rows})
    fig, axes = plt.subplots(len(kinds), 1, figsize=(6.0, 0.8 + 5.0 * len(kinds)), squeeze=False, facecolor=SURFACE)
    for ax, kind in zip(axes[:, 0], kinds):
        sel = [r for r in rows if r["kind"] == kind]
        style(ax)
        sizes = [r[k] for r in sel for k in [tiny] + [t[0] for t in tools]]
        lo, hi = max(1, min(sizes)) / 1.5, max(sizes) * 1.5
        ax.plot([lo, hi], [lo, hi], color=MUTED, linewidth=1)
        for key, label, color, _, _ in tools:
            for step, marker in zip(steps, STEP_MARKERS):
                pick = [r for r in sel if r["step"] == step]
                ax.scatter([r[key] for r in pick], [r[tiny] for r in pick], s=22, marker=marker, color=color,
                           alpha=0.7, edgecolors=SURFACE, linewidths=0.5)
        ax.set_xscale("log")
        ax.set_yscale("log")
        for axis in (ax.xaxis, ax.yaxis):  # decades only: the range spans several
            axis.set_major_locator(ticker.LogLocator(base=10))
            axis.set_major_formatter(ticker.FuncFormatter(lambda x, _: "{:,.0f}".format(x)))
            axis.set_minor_formatter(ticker.NullFormatter())
        ax.set_xlim(lo, hi)
        ax.set_ylim(lo, hi)
        ax.set_aspect("equal")
        ax.set_title("%s (%d pairs)" % (kind_label(rows, kind), len(sel)), fontsize=11, color=INK, loc="left")
        ax.set_xlabel("the other tool's patch (bytes, log scale)", fontsize=9, color=MUTED)
        ax.set_ylabel("tinydelta's patch (bytes, log scale)", fontsize=9, color=MUTED)
        ax.text(0.97, 0.05, "below the line:\ntinydelta smaller", transform=ax.transAxes, ha="right",
                fontsize=9, color=MUTED)
    # Two legends, in the first panel: colour for the tool, marker for the step.
    tool_handles = [plt.Line2D([], [], marker="o", linestyle="", markersize=6, color=color, label=label)
                    for _, label, color, _, _ in tools]
    step_handles = [plt.Line2D([], [], marker=marker, linestyle="", markersize=6, color=MUTED,
                               label="%d commit%s apart" % (step, "" if step == 1 else "s"))
                    for step, marker in zip(steps, STEP_MARKERS)]
    first = axes[0][0].legend(handles=tool_handles, frameon=False, fontsize=9, loc="upper left", labelcolor=INK,
                              title="tool", title_fontsize=9, alignment="left")
    axes[0][0].add_artist(first)
    axes[0][0].legend(handles=step_handles, frameon=False, fontsize=9, loc="lower right", labelcolor=INK,
                      bbox_to_anchor=(1.0, 0.12), alignment="left")
    height = fig.get_figheight()
    fig.text(0.02, 1 - 0.15 / height, "tinydelta against other tools, every pair",
             ha="left", va="top", fontsize=13, color=INK)
    fig.text(0.02, 1 - 0.45 / height, "Each pair once per tool; below the line, tinydelta's patch is smaller.",
             ha="left", va="top", fontsize=9, color=MUTED)
    fig.tight_layout(rect=(0, 0, 1, 1 - 0.7 / height))
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


def tradeoff(rows, out):
    """Typical patch size against the decoder's footprint on the device:
    code plus the RAM buffers it needs, stack excluded (decoders.json).
    For a decoder that holds the whole old file, the median old file of that
    kind is added. A point whose code size is unknown, or that holds the old
    file, is a lower bound, drawn as an arrow."""
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "decoders.json")
    decoders = {k: v for k, v in json.load(open(path)).items() if not k.startswith("_")}
    colors = {k: c for k, _, c, _, _ in OTHERS}
    colors["openvcdiff_interleaved"] = colors["openvcdiff"]  # tiny-vcdiff: the same format family
    colors.update({k: c for k, _, c in TINY_VARIANTS})
    # decoders.json's "tinydelta" stands for whichever tinydelta column the
    # comparisons use (optimal, when measured).
    column = {k: (tiny_key(rows) if k == "tinydelta" else k) for k in decoders}
    # HPatchLite's 32 KB dictionary is left out to keep the graph readable
    # (it is close to the 4 KB one); it is in the summary.
    tools = [k for k in decoders if column[k] in rows[0] and k != "hpatchlite_32k"]
    kinds = kinds_of(rows)
    # One panel per row: at most ~830 px wide, the width of GitHub's README
    # column, so it is never shrunk there.
    fig, axes = plt.subplots(len(kinds), 1, figsize=(7.2, 0.8 + 4.2 * len(kinds)), squeeze=False, facecolor=SURFACE)
    for ax, kind in zip(axes[:, 0], kinds):
        sel = [r for r in rows if r["kind"] == kind]
        old = statistics.median(r["dict_bytes"] for r in sel)
        style(ax)
        points = []
        for key in tools:
            d = decoders[key]
            footprint = (d["code"] or 0) + d["buffers"] + (old if d["plus_old_file"] else 0)
            lower_bound = d["code"] is None or d["plus_old_file"]
            share = statistics.median(100 * r[column[key]] / max(r["target_bytes"], 1) for r in sel)
            ax.plot([footprint], [share], marker=">" if lower_bound else "o", markersize=9,
                    color=colors.get(column[key], INK), markeredgecolor=SURFACE, markeredgewidth=1.5, linestyle="")
            points.append((footprint, share, d["label"] + (" (at least)" if lower_bound else "")))
        ax.set_xscale("log")
        ax.set_yscale("log")
        ax.margins(x=0.22, y=0.22)
        place_labels(fig, ax, points)
        ax.xaxis.set_major_locator(ticker.LogLocator(base=10))
        ax.xaxis.set_major_formatter(ticker.FuncFormatter(
            lambda x, _: "%g MB" % (x / 1e6) if x >= 1e6 else "%g KB" % (x / 1e3) if x >= 1e3 else "%g B" % x))
        ax.xaxis.set_minor_formatter(ticker.NullFormatter())
        log_axis(ax.yaxis, "%")
        ax.set_title(kind_label(rows, kind), fontsize=11, color=INK, loc="left")
        ax.set_xlabel("decoder footprint: code + RAM buffers, stack excluded (log scale)", fontsize=9, color=MUTED)
        ax.set_ylabel("median patch size, % of target (log scale)", fontsize=9, color=MUTED)
    height = fig.get_figheight()
    fig.text(0.02, 1 - 0.15 / height, "Patch size against what the decoder costs on the device",
             ha="left", va="top", fontsize=13, color=INK)
    fig.text(0.02, 1 - 0.45 / height, "Lower left is better. Arrows are lower bounds: code size unknown, "
             "or the whole old file in RAM.", ha="left", va="top", fontsize=9, color=MUTED)
    fig.tight_layout(rect=(0, 0, 1, 1 - 0.7 / height))
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
