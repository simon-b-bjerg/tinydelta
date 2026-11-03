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
from typing import List, Optional, Union

__all__ = ["encode", "decode", "Patch", "Error", "TooLargeError", "MAX_SIZE", "binary_path", "__version__"]

__version__ = "0.0.0.dev0"  # replaced in release wheels

#: Largest dictionary or target the format supports: 2**30 - 1 bytes.
MAX_SIZE = (1 << 30) - 1

# What dictionaries, targets and patches may be.
_BytesLike = Union[bytes, bytearray, memoryview]


class Error(Exception):
    """tinydelta failed, e.g. on a malformed patch. The message is the
    command-line tool's."""


class TooLargeError(Error):
    """The target would exceed its size limit, or the dictionary or target
    exceeds MAX_SIZE."""


def binary_path() -> str:
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


class Patch(bytes):
    """A patch, as encode returns it: the patch bytes, plus passes, how
    many times the device decodes it: 1, or 2 for a two-pass patch
    (two_pass), which is decoded against an empty dictionary first and
    the result then against the dictionary. Send passes with the patch;
    it is not part of it. decode reads it from a Patch."""

    passes: int

    def __new__(cls, data: _BytesLike, passes: int = 1) -> "Patch":
        patch = super().__new__(cls, data)
        patch.passes = passes
        return patch


def encode(dictionary: _BytesLike, target: _BytesLike, *, greedy: bool = False, max_instruction_len: int = 0,
           coder_tables: int = 0, window: int = 0, two_pass: bool = False, relocation: Optional[str] = None,
           image_base: int = 0, rules: Optional[str] = None, ship_rules: bool = False) -> Patch:
    """Returns the smallest Patch that turns dictionary into target (both
    bytes-like) and that the device's decoder, described by the options,
    can decode.

    coder_tables, if above 0, is the decoder's buffer for the coder's
    tables (tables_size in C): it allows a coded patch, decoded in one
    pass, whose tables fit (522, 777 or 1287 bytes).
    window, if above 0, keeps copies from the target already produced
    within that many bytes, so the decoder reads them from its RAM buffer
    only (buffer_size in C, with target_read NULL).
    two_pass allows a patch the device decodes twice (Patch.passes).
    relocation, for firmware images, is the instruction set whose addresses
    the decoder relocates ("thumb" or "msp430x", TINYDELTA_RELOC in C),
    with image_base the address the image starts at on the device; it
    needs coder_tables, with 12 bytes more per map entry.
    rules, instead of relocation, is the path of a relocation rule file
    (YAML, docs/relocation-rules.md); the patch names its table by CRC, so
    decode needs the same rules=, unless ship_rules puts the table in the
    patch (experimental).

    greedy uses the fast greedy encoder instead of the optimal one (patches
    are typically a few percent larger). max_instruction_len, if above 0,
    limits the target bytes any one instruction produces.
    """
    if max_instruction_len < 0:
        raise ValueError("max_instruction_len must be 0 (no limit) or more")
    if window < 0:
        raise ValueError("window must be 0 (no limit) or more")
    if coder_tables < 0:
        raise ValueError("coder_tables must be 0 (no coder) or more")
    if relocation not in (None, "thumb", "msp430x"):
        raise ValueError('relocation must be None, "thumb" or "msp430x"')
    if relocation and rules:
        raise ValueError("give relocation or rules, not both")
    with tempfile.TemporaryDirectory(prefix="tinydelta-") as work:
        dict_file = _write(work, "dictionary", dictionary)
        target_file = _write(work, "target", target)
        args = ["encode", "-max-len", str(max_instruction_len), "-window", str(window),
                "-coder-tables", str(coder_tables)]
        if greedy:
            args.append("-greedy")
        if two_pass:
            args.append("-two-pass")
        if relocation:
            args += ["-reloc", relocation, "-base", str(image_base)]
        if rules:
            args += ["-rules", os.fspath(rules), "-base", str(image_base)]
        if ship_rules:
            args.append("-ship-rules")
        result = _run(args + [dict_file, target_file])
        passes = 1
        for line in result.stderr.decode("utf-8", "replace").splitlines():
            if line.startswith("passes "):
                passes = int(line.split()[1])
        return Patch(result.stdout, passes)


def decode(dictionary: _BytesLike, patch: _BytesLike, *, max_size: Optional[int] = None, passes: Optional[int] = None,
           rules: Optional[str] = None) -> bytes:
    """Applies patch to dictionary (both bytes-like) and returns the target.

    passes is how many times the patch is decoded, 1 or 2 (a two-pass
    patch); by default a Patch's own passes, else 1.

    max_size, if given, is the largest target accepted, in bytes; a patch
    that would produce more raises TooLargeError before producing it. Set it
    whenever the expected size is known: a short patch can describe a huge
    target. A malformed patch raises Error.

    rules is the relocation rule file a patch made with encode(rules=...)
    names (not needed if it ships its table).
    """
    if passes is None:
        passes = getattr(patch, "passes", 1)
    if passes not in (1, 2):
        raise ValueError("passes must be 1 or 2")
    args = ["decode"]
    if passes == 2:
        args.append("-two-pass")
    if max_size is not None:
        if max_size < 1:
            raise ValueError("max_size must be at least 1, or None for no limit")
        args += ["-max-size", str(min(max_size, MAX_SIZE))]
    if rules:
        args += ["-rules", os.fspath(rules)]
    with tempfile.TemporaryDirectory(prefix="tinydelta-") as work:
        dict_file = _write(work, "dictionary", dictionary)
        return _run(args + [dict_file, "-"], stdin=bytes(patch)).stdout


def _write(directory: str, name: str, data: _BytesLike) -> str:
    path = os.path.join(directory, name)
    with open(path, "wb") as f:
        f.write(data)
    return path


def _run(args: List[str], stdin: bytes = b"") -> "subprocess.CompletedProcess[bytes]":
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
    return result
