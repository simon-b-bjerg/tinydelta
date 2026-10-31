# SPDX-FileCopyrightText: 2026 Simon Bjerg
# SPDX-License-Identifier: MIT

"""tinydelta: tiny binary patches for small devices.

Creates and applies tinydelta patches by running the tinydelta command-line
tool that ships inside this package's wheel. A patch turns an old file (the
dictionary) into a new one (the target); see
https://github.com/simon-b-bjerg/tinydelta for the format.

    import tinydelta

    patch = tinydelta.encode(old, new)
    assert tinydelta.decode(old, patch) == new
"""

import os
import subprocess
import sys
import tempfile

__all__ = ["encode", "decode", "Error", "TooLargeError", "MAX_SIZE", "binary_path", "__version__"]

__version__ = "0.0.0.dev0"  # replaced in release wheels

#: Largest dictionary or target the format supports: 2**30 - 1 bytes.
MAX_SIZE = (1 << 30) - 1


class Error(Exception):
    """tinydelta failed, e.g. on a malformed patch. The message is the
    command-line tool's."""


class TooLargeError(Error):
    """The target would exceed its size limit, or the dictionary or target
    exceeds MAX_SIZE."""


def binary_path():
    """Returns the path of the tinydelta command-line tool this package runs:
    $TINYDELTA_BIN if set, else the one bundled in the wheel."""
    override = os.environ.get("TINYDELTA_BIN")
    if override:
        return override
    name = "tinydelta.exe" if sys.platform == "win32" else "tinydelta"
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "bin", name)
    if not os.path.isfile(path):
        raise Error(
            "the tinydelta binary is missing from this installation; install a "
            "platform wheel with pip, or set TINYDELTA_BIN to a tinydelta binary"
        )
    return path


def encode(dictionary, target, *, greedy=False, max_instruction_len=0,
           for_compression=False, max_distance=0):
    """Returns a patch that turns dictionary into target (both bytes-like).

    greedy uses the fast greedy encoder instead of the optimal one (patches
    are typically a few percent larger). max_instruction_len, if above 0,
    limits the target bytes any one instruction produces.

    for_compression makes the first pass of compression mode: a patch to
    encode again against an empty dictionary, which shrinks it when a
    change touches much of the file (format version 2 where that pays
    off). max_distance, if above 0, limits how far back the patch reads
    its own output, so a decoder only needs a buffer that large; see the
    README.
    """
    if max_instruction_len < 0:
        raise ValueError("max_instruction_len must be 0 (no limit) or more")
    if max_distance < 0:
        raise ValueError("max_distance must be 0 (no limit) or more")
    with tempfile.TemporaryDirectory(prefix="tinydelta-") as work:
        dict_file = _write(work, "dictionary", dictionary)
        target_file = _write(work, "target", target)
        args = ["encode", "-max-len", str(max_instruction_len), "-max-distance", str(max_distance)]
        if greedy:
            args.append("-greedy")
        if for_compression:
            args.append("-for-compression")
        return _run(args + [dict_file, target_file])


def decode(dictionary, patch, *, max_size=None):
    """Applies patch to dictionary (both bytes-like) and returns the target.

    max_size, if given, is the largest target accepted, in bytes; a patch
    that would produce more raises TooLargeError before producing it. Set it
    whenever the expected size is known: a short patch can describe a huge
    target. A malformed patch raises Error.
    """
    args = ["decode"]
    if max_size is not None:
        if max_size < 1:
            raise ValueError("max_size must be at least 1, or None for no limit")
        args += ["-max-size", str(min(max_size, MAX_SIZE))]
    with tempfile.TemporaryDirectory(prefix="tinydelta-") as work:
        dict_file = _write(work, "dictionary", dictionary)
        return _run(args + [dict_file, "-"], stdin=bytes(patch))


def _write(directory, name, data):
    path = os.path.join(directory, name)
    with open(path, "wb") as f:
        f.write(data)
    return path


def _run(args, stdin=b""):
    result = subprocess.run([binary_path()] + args, input=stdin, capture_output=True)
    if result.returncode != 0:
        message = result.stderr.decode("utf-8", "replace").strip()
        while message.startswith("tinydelta: "):  # the tool's and the Go error's prefixes
            message = message[len("tinydelta: "):]
        # The tool's message for Go's ErrTooLarge; pinned by a test in
        # cmd/tinydelta.
        if "size limit exceeded" in message:
            raise TooLargeError(message)
        raise Error(message or "tinydelta exited with status %d" % result.returncode)
    return result.stdout
