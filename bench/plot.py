#!/usr/bin/env python3
"""Turns the benchmark CSV into a summary and two graphs.

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
    pairs.svg         every pair: tinydelta's patch size against open-vcdiff's,
                      HPatchLite's (4 KB) and bsdiff's
    tradeoff.svg      each tool's typical patch size against its decoder's
                      footprint, code + RAM buffers, stack excluded
                      (from bench/decoders.json)
    ratio_ecdf.svg    distribution of tinydelta's patch size divided by each
                      other tool's, per kind of data
    patch_share.svg   each tool's patch size as a share of the target
Each graph is also written as a PNG for slides. Every comparison uses one
baseline: tinydelta's patch as it is sent, uncompressed (the optimal
encoder's, when measured).
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

# The other tools in a fixed order: tinydelta's compressed variants (grays) and categorical slots 1-8 (validated with the dataviz palette
# checks), a line style each, and whether the patch is compressed (which
# groups the distribution graph's rows).
OTHERS = [
    ("tinydelta_cz_twice", "tinydelta, compression mode, decoded twice", "#6b6a64", "-", True),
    ("tinydelta_cz_twice_1k", "tinydelta, compression mode, decoded twice, 1 KB window", "#6b6a64", "--", True),
    ("openvcdiff", "open-vcdiff", "#2a78d6", "-", False),
    ("openvcdiff_zlib", "open-vcdiff + zlib", "#eb6834", "--", True),
    ("xdelta3", "xdelta3", "#1baf7a", "-.", False),
    ("bsdiff", "bsdiff", "#eda100", ":", True),
    ("detools", "detools (heatshrink)", "#e87ba4", "-", True),
    ("hpatchlite_4k", "HPatchLite, 4 KB dict.", "#008300", "--", True),
    ("hpatchlite_32k", "HPatchLite, 32 KB dict.", "#6250d6", "-.", True),
    ("zstd", "zstd --patch-from", "#e34948", ":", True),
]
SURFACE, INK, MUTED, GRID = "#fcfcfb", "#0b0b0b", "#6b6a64", "#e4e3de"
# tinydelta's variants, best first: optimal at any size is "tinydelta" in
# the comparisons when present; the others appear in the patch-size tables.
TINY_VARIANTS = [
    ("tinydelta_optimal", "tinydelta (optimal)", INK),
    ("tinydelta", "tinydelta (library default)", INK),
    ("tinydelta_greedy", "tinydelta (greedy)", "#8a8983"),
]


def load(path):
    with open(path, newline="") as f:
        rows = list(csv.DictReader(f))
    for r in rows:
        for k, v in r.items():
            if k not in ("corpus", "kind", "item", "from", "to"):
                r[k] = int(v)
    return rows


def percentile(values, p):
    s = sorted(values)
    k = (len(s) - 1) * p / 100
    lo, hi = math.floor(k), math.ceil(k)
    return s[lo] + (s[hi] - s[lo]) * (k - lo)


def geomean(values):
    return math.exp(sum(math.log(v) for v in values) / len(values))


KIND_ORDER = ["firmware", "device tree"]  # firmware first; others after, by name


def kinds_of(rows):
    kinds = {r["kind"] for r in rows}
    return [k for k in KIND_ORDER if k in kinds] + sorted(kinds - set(KIND_ORDER))


def tiny_key(rows):
    """The tinydelta column the comparisons use: optimal at any size if
    measured, else the library's default."""
    return "tinydelta_optimal" if "tinydelta_optimal" in rows[0] else "tinydelta"


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
    fig.savefig(os.path.join(out, name + ".svg"), facecolor=SURFACE)
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
    lines += [
        "Ratio = tinydelta's patch size / the other tool's; below 1, tinydelta is smaller. "
        "Geometric mean is the right average for ratios; \"smaller\" is the share of pairs where "
        "tinydelta is smaller. tinydelta is %s, as it is sent; its compression mode, decoded twice, "
        "is compared like any other tool." % (
            "the optimal encoder at any size" if tiny == "tinydelta_optimal" else "the library default"),
        "",
    ]
    for kind in kinds:
        for step in sorted({r["step"] for r in rows if r["kind"] == kind}):
            sel = [r for r in rows if r["kind"] == kind and r["step"] == step]
            items = sorted({r["corpus"] + "/" + r["item"] for r in sel})
            lines += ["## %s, %d commit%s apart" % (kind, step, "" if step == 1 else "s"), "",
                      "%d pairs from %s; median target %s bytes." % (
                          len(sel), ", ".join(items), "{:,}".format(int(statistics.median(r["target_bytes"] for r in sel)))),
                      "",
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


# The comparisons the distribution and pair graphs show: what compression
# would add, the format tinydelta replaces, the closest embedded
# alternative, and the strongest on size. The rest is in the forest graph.
KEY_TOOLS = ["openvcdiff", "hpatchlite_4k", "bsdiff"]


def ratio_ecdf(rows, out):
    """Every pair, per kind: for each ratio along the x axis, the share of
    pairs at or below it. Where a line crosses 1 is the share of pairs
    where tinydelta is smaller; the legend states it."""
    kinds = kinds_of(rows)
    tiny = tiny_key(rows)
    tools = [t for t in OTHERS if t[0] in KEY_TOOLS and t[0] in rows[0]]
    fig, axes = plt.subplots(1, len(kinds), figsize=(5.6 * len(kinds), 4.8), squeeze=False, sharex=True,
                             facecolor=SURFACE)
    for ax, kind in zip(axes[0], kinds):
        sel = [r for r in rows if r["kind"] == kind]
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
        ax.set_title("%s (%d pairs)" % (kind, len(sel)), fontsize=11, color=INK, loc="left")
        ax.set_xlabel("tinydelta's patch size ÷ the tool's (log scale)", fontsize=9, color=MUTED)
        ax.set_ylabel("% of pairs at or below this ratio", fontsize=9, color=MUTED)
        ax.legend(frameon=False, fontsize=8, loc="upper left", labelcolor=INK)
    height = fig.get_figheight()
    fig.text(0.02, 1 - 0.15 / height, "How often, and by how much, tinydelta's patch is smaller",
             ha="left", va="top", fontsize=13, color=INK)
    fig.text(0.02, 1 - 0.45 / height, "Each line climbs through every pair, smallest ratio first. "
             "Where it crosses 1: the share of pairs where tinydelta's patch is smaller.",
             ha="left", va="top", fontsize=9, color=MUTED)
    fig.tight_layout(rect=(0, 0, 1, 1 - 0.7 / height))
    save(fig, out, "ratio_ecdf")


def patch_share(rows, out):
    kinds = kinds_of(rows)
    tools = [(k, l, c) for k, l, c in TINY_VARIANTS if k in rows[0]] + [(k, l, c) for k, l, c, _, _ in OTHERS if k in rows[0]]
    fig, axes = plt.subplots(1, len(kinds), figsize=(5.6 * len(kinds), 1.2 + 0.5 * len(tools)),
                             squeeze=False, facecolor=SURFACE)
    for ax, kind in zip(axes[0], kinds):
        sel = [r for r in rows if r["kind"] == kind]
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
        log_axis(ax.xaxis, "%")
        ax.set_title("%s: patch size as %% of target" % kind, fontsize=11, color=INK, loc="left")
        ax.set_xlabel("patch size as % of target (log scale)", fontsize=9, color=MUTED)
    fig.tight_layout()
    save(fig, out, "patch_share")


FOREST_MIN, FOREST_MAX = 0.1, 10  # the forest graph's axis; lines beyond end in an arrowhead


def forest(rows, out):
    kinds = kinds_of(rows)
    steps = sorted({r["step"] for r in rows})
    # tinydelta's own compressed variants are compared with the others in
    # the trade-off graph and the summary, not here.
    tools = [t for t in OTHERS if t[0] in rows[0] and not t[0].startswith("tinydelta_cz")]
    fig, axes = plt.subplots(len(kinds), 1, figsize=(7.2, 0.9 + 0.42 * len(tools) * len(kinds) + 0.6 * len(kinds)),
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
                ax.plot([geomean(rs)], [y], marker="o", markersize=7, color=color,
                        markerfacecolor=color if filled else SURFACE, markeredgewidth=2 if not filled else 1.5,
                        markeredgecolor=color if not filled else SURFACE)
        ax.set_yticks(range(len(tools)), labels, fontsize=9, color=INK)
        ax.invert_yaxis()
        ax.set_xscale("log")
        ax.set_xlim(FOREST_MIN, FOREST_MAX)
        log_axis(ax.xaxis)
        n = len([r for r in rows if r["kind"] == kind])
        ax.set_title("%s (%d pairs)" % (kind, n), fontsize=11, color=INK, loc="left")
    handles = [plt.Line2D([], [], marker="o", linestyle="", markersize=7, color=MUTED,
                          markerfacecolor=MUTED if step == steps[0] else SURFACE, markeredgewidth=2,
                          label="%d commit%s apart" % (step, "" if step == 1 else "s")) for step in steps]
    axes[0][0].legend(handles=handles, frameon=False, fontsize=8, loc="lower right", labelcolor=INK,
                      bbox_to_anchor=(1.0, 1.0), ncol=len(steps))
    axes[-1][0].set_xlabel("tinydelta's patch size ÷ the tool's (log scale)   ← tinydelta smaller · larger →\n"
                           "dot: geometric mean · line: 10th-90th percentile, arrowhead where it runs past the axis",
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


KIND_MARKERS = ["o", "^", "s", "D"]
KIND_COLORS = ["#2a78d6", "#eda100", "#1baf7a", "#e87ba4"]  # categorical slots, validated


def pair_scatter(rows, out, name, mine, mine_label, other):
    """Every pair as one dot: the other tool's patch size across,
    tinydelta's (column mine) up. One panel; the kinds of data by marker."""
    labels = {k: (l, c) for k, l, c, _, _ in OTHERS}
    if mine not in rows[0] or other not in rows[0]:
        return
    label = labels[other][0]
    fig, ax = plt.subplots(figsize=(5.2, 5.0), facecolor=SURFACE)
    style(ax)
    sizes = [r[k] for r in rows for k in (other, mine)]
    lo, hi = max(1, min(sizes)) / 1.5, max(sizes) * 1.5
    ax.plot([lo, hi], [lo, hi], color=MUTED, linewidth=1)
    for kind, marker, color in zip(kinds_of(rows), KIND_MARKERS, KIND_COLORS):
        sel = [r for r in rows if r["kind"] == kind]
        ax.scatter([r[other] for r in sel], [r[mine] for r in sel], s=24, marker=marker, color=color,
                   alpha=0.75, edgecolors=SURFACE, linewidths=0.6, label=kind)
    ax.set_xscale("log")
    ax.set_yscale("log")
    for axis in (ax.xaxis, ax.yaxis):  # decades only: the range spans several
        axis.set_major_locator(ticker.LogLocator(base=10))
        axis.set_major_formatter(ticker.FuncFormatter(lambda x, _: "{:,.0f}".format(x)))
        axis.set_minor_formatter(ticker.NullFormatter())
    ax.set_xlim(lo, hi)
    ax.set_ylim(lo, hi)
    ax.set_aspect("equal")
    ax.set_title("%s\nvs %s, every pair" % (mine_label, label), fontsize=11, color=INK, loc="left")
    ax.set_xlabel("%s patch (bytes, log scale)" % label, fontsize=9, color=MUTED)
    ax.set_ylabel("%s patch (bytes, log scale)" % mine_label, fontsize=9, color=MUTED)
    ax.text(0.97, 0.05, "below the line:\n%s smaller" % mine_label, transform=ax.transAxes, ha="right",
            fontsize=8, color=MUTED)
    ax.legend(frameon=False, fontsize=8, loc="upper left", labelcolor=INK)
    fig.tight_layout()
    save(fig, out, name)


def place_labels(fig, ax, points):
    """Labels each point at the first of a few positions around it where
    the label overlaps no other label, no point and stays inside the panel;
    the least crowded position otherwise."""
    fig.canvas.draw()
    renderer = fig.canvas.get_renderer()
    box = ax.get_window_extent(renderer)
    dots = [ax.transData.transform((x, y)) for x, y, _ in points]
    placed = []
    offsets = [(dx, dy * reach, ha, va) for reach in (1, 3, 5)
               for dx, dy, ha, va in [(8, 4, "left", "bottom"), (8, -4, "left", "top"),
                                      (-8, 4, "right", "bottom"), (-8, -4, "right", "top"),
                                      (0, 3, "center", "bottom"), (0, -3, "center", "top")]]
    for (x, y, label), dot in zip(points, dots):
        best = None
        for dx, dy, ha, va in offsets:
            text = ax.annotate(label, (x, y), xytext=(dx, dy), textcoords="offset points",
                               ha=ha, va=va, fontsize=8, color=INK)
            extent = text.get_window_extent(renderer).expanded(1.05, 1.15)
            cost = 3 * sum(extent.overlaps(other) for other in placed)
            cost += sum(1 for d in dots if d is not dot and extent.contains(*d))
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
        placed.append(best[2])


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
    fig, axes = plt.subplots(1, len(kinds), figsize=(5.6 * len(kinds), 4.9), squeeze=False, facecolor=SURFACE)
    for ax, kind in zip(axes[0], kinds):
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
        ax.margins(x=0.12, y=0.22)
        place_labels(fig, ax, points)
        ax.xaxis.set_major_locator(ticker.LogLocator(base=10))
        ax.xaxis.set_major_formatter(ticker.FuncFormatter(
            lambda x, _: "%g MB" % (x / 1e6) if x >= 1e6 else "%g KB" % (x / 1e3) if x >= 1e3 else "%g B" % x))
        ax.xaxis.set_minor_formatter(ticker.NullFormatter())
        log_axis(ax.yaxis, "%")
        ax.set_title("%s" % kind, fontsize=11, color=INK, loc="left")
        ax.set_xlabel("decoder footprint: code + RAM buffers, stack excluded (log scale)", fontsize=9, color=MUTED)
        ax.set_ylabel("median patch size, % of target (log scale)", fontsize=9, color=MUTED)
    height = fig.get_figheight()
    fig.text(0.02, 1 - 0.15 / height, "Patch size against what the decoder costs on the device",
             ha="left", va="top", fontsize=13, color=INK)
    fig.text(0.02, 1 - 0.45 / height, "Lower left is better. Arrows are lower bounds: code size unknown, "
             "or the decoder holds the whole old file in RAM.", ha="left", va="top", fontsize=9, color=MUTED)
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
    forest(rows, out)
    # tinydelta against the closest embedded alternative, plain and in
    # compression mode decoded twice.
    pair_scatter(rows, out, "pairs", tiny_key(rows), "tinydelta", "hpatchlite_4k")
    pair_scatter(rows, out, "pairs_compression", "tinydelta_cz_twice", "tinydelta, compression mode, decoded twice",
                 "hpatchlite_4k")
    tradeoff(rows, out)
    ratio_ecdf(rows, out)
    patch_share(rows, out)


if __name__ == "__main__":
    main()
