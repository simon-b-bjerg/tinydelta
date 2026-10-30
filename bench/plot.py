#!/usr/bin/env python3
"""Turns the benchmark CSV into a summary and two graphs.

    python3 bench/plot.py <results.csv> <output dir>

Writes to <output dir>:
    summary.md        per kind of data and commit step: how tinydelta's patch
                      size compares with each other tool (geometric mean,
                      median, 10th/90th percentile of the size ratio, share
                      of pairs where tinydelta is smaller), plain and
                      zlib-compressed, and each tool's patch size as a share
                      of the target
    ratio_ecdf.svg    distribution of tinydelta's patch size divided by each
                      other tool's, per kind of data; plain and zlib-compressed
    patch_share.svg   each tool's patch size as a share of the target
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

# The other tools in a fixed order: categorical slots 1-8 (validated with
# the dataviz palette checks), a line style each, and whether the patch is
# compressed. Uncompressed formats are compared with tinydelta, compressed
# ones with tinydelta + zlib.
OTHERS = [
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
    ("tinydelta", "tinydelta (library default)", "#3d3c38"),
    ("tinydelta_greedy", "tinydelta (greedy)", "#8a8983"),
    ("tinydelta_zlib", "tinydelta + zlib", MUTED),
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


def summary(rows, tools_file, out):
    kinds = sorted({r["kind"] for r in rows})
    lines = ["# tinydelta patch sizes", ""]
    if os.path.exists(tools_file):
        versions = json.load(open(tools_file))
        lines += ["Tools: " + "; ".join("%s (%s)" % (k, v) for k, v in versions.items()), ""]
    tiny = tiny_key(rows)
    zlib_measured = "tinydelta_zlib" in rows[0]
    lines += [
        "Ratio = tinydelta's patch size / the other tool's; below 1, tinydelta is smaller. "
        "Geometric mean is the right average for ratios; \"smaller\" is the share of pairs where "
        "tinydelta is smaller. tinydelta is %s%s." % (
            "the optimal encoder at any size" if tiny == "tinydelta_optimal" else "the library default",
            "; tinydelta + zlib is that patch at zlib level 9, against the same tools" if zlib_measured else ""),
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
                      "| tinydelta vs | geo. mean | median | p10-p90 | smaller |"
                      + (" + zlib: geo. mean | median | p10-p90 | smaller |" if zlib_measured else ""),
                      "|---|---:|---:|---:|---:|" + ("---:|---:|---:|---:|" if zlib_measured else "")]
            for key, label, _, _, _ in OTHERS:
                rs = ratios(sel, key, tiny)
                if not rs:
                    continue
                row = "| %s | %s |" % (label, stats(rs))
                if zlib_measured:
                    row += " %s |" % stats(ratios(sel, key, "tinydelta_zlib"))
                lines.append(row)
            lines += ["", "Patch size as a share of the target (median, p10-p90):", "",
                      "| tool | median | p10 | p90 |", "|---|---:|---:|---:|"]
            for key, label in [(k, l) for k, l, _ in TINY_VARIANTS] + [(k, l) for k, l, _, _, _ in OTHERS]:
                if key not in sel[0]:
                    continue
                share = [100 * r[key] / max(r["target_bytes"], 1) for r in sel]
                lines.append("| %s | %.2f%% | %.2f%% | %.2f%% |" % (
                    label, statistics.median(share), percentile(share, 10), percentile(share, 90)))
            lines.append("")
    with open(os.path.join(out, "summary.md"), "w") as f:
        f.write("\n".join(lines))


def ratio_ecdf(rows, out):
    """One row per comparison, like for like: tinydelta against uncompressed
    formats, tinydelta + zlib against compressed ones."""
    kinds = sorted({r["kind"] for r in rows})
    groups = [(tiny_key(rows), "tinydelta", "uncompressed formats", False)]
    if "tinydelta_zlib" in rows[0]:
        groups.append(("tinydelta_zlib", "tinydelta + zlib", "compressed formats", True))
    groups = [g for g in groups if any(k in rows[0] and c == g[3] for k, _, _, _, c in OTHERS)]
    fig, axes = plt.subplots(len(groups), len(kinds), figsize=(5.4 * len(kinds), 4.4 * len(groups)),
                             squeeze=False, facecolor=SURFACE)
    for row_axes, (tiny, tiny_label, against, compressed) in zip(axes, groups):
        for ax, kind in zip(row_axes, kinds):
            sel = [r for r in rows if r["kind"] == kind]
            style(ax)
            ax.axvline(1, color=MUTED, linewidth=1)
            for slot, (key, label, color, dash, comp) in enumerate(OTHERS):
                rs = sorted(ratios(sel, key, tiny)) if comp == compressed else []
                if not rs:
                    continue
                ys = [100 * (i + 1) / len(rs) for i in range(len(rs))]
                ax.step(rs, ys, where="post", color=color, linestyle=dash, linewidth=2, label=label)
                # Each tool's label at its own height, so labels don't collide.
                at = min(len(rs) - 1, int(len(rs) * (0.12 + 0.11 * slot)))
                ax.annotate(label, (rs[at], ys[at]), xytext=(6, 0),
                            textcoords="offset points", fontsize=8, color=INK, va="center")
            ax.set_xscale("log")
            log_axis(ax.xaxis)
            ax.set_ylim(0, 100)
            ax.set_title("%s vs %s: %s (%d pairs)" % (tiny_label, against, kind, len(sel)),
                         fontsize=10, color=INK, loc="left")
            ax.set_xlabel("%s patch size ÷ other tool's (log scale)" % tiny_label, fontsize=9, color=MUTED)
            ax.set_ylabel("share of pairs at or below (%)", fontsize=9, color=MUTED)
            ax.text(0.98, 0.04, "← tinydelta smaller", transform=ax.transAxes, ha="right", fontsize=8, color=MUTED)
        row_axes[0].legend(frameon=False, fontsize=8, loc="upper left", labelcolor=INK)
    fig.tight_layout()
    fig.savefig(os.path.join(out, "ratio_ecdf.svg"), facecolor=SURFACE)
    plt.close(fig)


def patch_share(rows, out):
    kinds = sorted({r["kind"] for r in rows})
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
    fig.savefig(os.path.join(out, "patch_share.svg"), facecolor=SURFACE)
    plt.close(fig)


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    results, out = sys.argv[1:]
    os.makedirs(out, exist_ok=True)
    rows = load(results)
    if not rows:
        sys.exit("plot: no rows in %s" % results)
    summary(rows, os.path.splitext(results)[0] + "-tools.json", out)
    ratio_ecdf(rows, out)
    patch_share(rows, out)


if __name__ == "__main__":
    main()
