#!/usr/bin/env python3
"""Builds the tinydelta Python wheels from the release archives.

    python3 python/build_wheels.py <version> <dist dir> <output dir>

<dist dir> holds the archives from scripts/build-release.sh (the same
<version>); one wheel per Linux and Windows archive is written to
<output dir>. Each wheel holds the Python package (python/src/tinydelta)
and that platform's tinydelta binary. Linux binaries are static, so their
wheels are tagged for both glibc (manylinux) and musl (musllinux) systems.

Wheels are zip files with a little metadata (PEP 427), written here
directly: no build backend is needed, and the output is reproducible
(fixed timestamps, sorted entries).
"""

import base64
import hashlib
import io
import os
import re
import sys
import tarfile
import zipfile

NAME = "tinydelta"
SUMMARY = "Tiny binary patches for small devices"
URL = "https://github.com/simon-b-bjerg/tinydelta"
REQUIRES_PYTHON = ">=3.9"
HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
PACKAGE = os.path.join(HERE, "src", NAME)
ZIP_TIME = (2020, 1, 1, 0, 0, 0)

# Release archive (os, arch) -> wheel platform tag.
PLATFORMS = {
    ("linux", "amd64"): "manylinux_2_17_x86_64.manylinux2014_x86_64.musllinux_1_1_x86_64",
    ("linux", "arm64"): "manylinux_2_17_aarch64.manylinux2014_aarch64.musllinux_1_1_aarch64",
    ("windows", "amd64"): "win_amd64",
    ("windows", "arm64"): "win_arm64",
}


def python_version(version):
    """Converts a release version (v1.2.3, v1.2.3-rc.1) to PEP 440 (1.2.3,
    1.2.3rc1). Build-only versions (dev-<commit>) become 0.0.0.dev0."""
    if version.startswith("dev-"):
        return "0.0.0.dev0"
    m = re.fullmatch(r"v(\d+\.\d+\.\d+)(?:-(alpha|beta|rc)\.(\d+))?", version)
    if not m:
        sys.exit("build_wheels: version %r is not vX.Y.Z or vX.Y.Z-(alpha|beta|rc).N" % version)
    pre = {"alpha": "a", "beta": "b", "rc": "rc"}
    return m.group(1) + (pre[m.group(2)] + m.group(3) if m.group(2) else "")


def read_binary(archive, goos):
    """Returns the tinydelta binary from a release archive."""
    name = "tinydelta.exe" if goos == "windows" else "tinydelta"
    if archive.endswith(".zip"):
        with zipfile.ZipFile(archive) as z:
            members = [m for m in z.namelist() if m.endswith("/" + name)]
            if len(members) == 1:
                return z.read(members[0])
    else:
        with tarfile.open(archive) as t:
            members = [m for m in t.getmembers() if m.name.endswith("/" + name) and m.isfile()]
            if len(members) == 1:
                return t.extractfile(members[0]).read()
    sys.exit("build_wheels: no single %s in %s" % (name, archive))


def metadata(version):
    with open(os.path.join(HERE, "README.md"), encoding="utf-8") as f:
        readme = f.read()
    headers = [
        ("Metadata-Version", "2.1"),
        ("Name", NAME),
        ("Version", version),
        ("Summary", SUMMARY),
        ("Home-page", URL),
        ("Author", "Simon Bjerg"),
        ("License", "MIT"),
        ("Project-URL", "Source, " + URL),
        ("Requires-Python", REQUIRES_PYTHON),
        ("Classifier", "License :: OSI Approved :: MIT License"),
        ("Classifier", "Programming Language :: Python :: 3"),
        ("Classifier", "Topic :: System :: Archiving :: Compression"),
        ("Classifier", "Topic :: Software Development :: Embedded Systems"),
        ("Description-Content-Type", "text/markdown"),
    ]
    return "".join("%s: %s\n" % h for h in headers) + "\n" + readme


def package_files(version):
    """Returns {path in wheel: bytes} for the pure-Python part."""
    files = {}
    for name in sorted(os.listdir(PACKAGE)):
        path = os.path.join(PACKAGE, name)
        if not os.path.isfile(path) or name.endswith(".pyc"):
            continue
        with open(path, "rb") as f:
            data = f.read()
        if name == "__init__.py":
            old = b'__version__ = "0.0.0.dev0"  # replaced in release wheels'
            if old not in data:
                sys.exit("build_wheels: __version__ line not found in __init__.py")
            data = data.replace(old, ('__version__ = "%s"' % version).encode())
        files["%s/%s" % (NAME, name)] = data
    return files


def write_wheel(out_dir, version, tag, binary, binary_name, common):
    dist_info = "%s-%s.dist-info" % (NAME, version)
    files = dict(common)
    files["%s/bin/%s" % (NAME, binary_name)] = binary
    files[dist_info + "/METADATA"] = metadata(version).encode()
    files[dist_info + "/WHEEL"] = (
        "Wheel-Version: 1.0\nGenerator: tinydelta build_wheels.py\nRoot-Is-Purelib: false\n"
        + "".join("Tag: py3-none-%s\n" % t for t in tag.split("."))
    ).encode()
    files[dist_info + "/entry_points.txt"] = b"[console_scripts]\ntinydelta = tinydelta.__main__:main\n"
    with open(os.path.join(ROOT, "LICENSE"), "rb") as f:
        files[dist_info + "/LICENSE"] = f.read()

    record = []
    for path in sorted(files):
        digest = base64.urlsafe_b64encode(hashlib.sha256(files[path]).digest()).rstrip(b"=").decode()
        record.append("%s,sha256=%s,%d" % (path, digest, len(files[path])))
    record.append(dist_info + "/RECORD,,")
    files[dist_info + "/RECORD"] = ("\n".join(record) + "\n").encode()

    wheel = os.path.join(out_dir, "%s-%s-py3-none-%s.whl" % (NAME, version, tag))
    with zipfile.ZipFile(wheel, "w", zipfile.ZIP_DEFLATED) as z:
        for path in sorted(files):
            info = zipfile.ZipInfo(path, ZIP_TIME)
            executable = path.startswith(NAME + "/bin/")
            info.external_attr = (0o100755 if executable else 0o100644) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(info, files[path])
    return wheel


def main():
    if len(sys.argv) != 4:
        sys.exit(__doc__)
    release_version, dist_dir, out_dir = sys.argv[1:]
    version = python_version(release_version)
    os.makedirs(out_dir, exist_ok=True)
    common = package_files(version)

    for (goos, arch), tag in sorted(PLATFORMS.items()):
        ext = "zip" if goos == "windows" else "tar.gz"
        archive = os.path.join(dist_dir, "tinydelta_%s_%s_%s.%s" % (release_version, goos, arch, ext))
        if not os.path.isfile(archive):
            sys.exit("build_wheels: missing %s" % archive)
        binary_name = "tinydelta.exe" if goos == "windows" else "tinydelta"
        wheel = write_wheel(out_dir, version, tag, read_binary(archive, goos), binary_name, common)
        print(wheel)


if __name__ == "__main__":
    main()
