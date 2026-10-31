"""Tests for the tinydelta Python package. They run whatever binary
tinydelta.binary_path() finds: $TINYDELTA_BIN, or the one bundled in an
installed wheel. From the repository root:

    go build -o /tmp/tinydelta ./cmd/tinydelta
    TINYDELTA_BIN=/tmp/tinydelta PYTHONPATH=python/src python -m unittest discover -s python/tests
"""

import os
import random
import subprocess
import sys
import unittest

import tinydelta

VECTORS = os.path.join(os.path.dirname(__file__), "..", "..", "testdata", "vectors.txt")


def read_vectors():
    """Yields (name, capacity, dictionary, patch, result, target) for each
    conformance vector; see the format at the top of testdata/vectors.txt."""
    with open(VECTORS) as f:
        for line in f:
            if not line.strip() or line.startswith("#"):
                continue
            fields = line.split()
            unhex = lambda s: b"" if s == "-" else bytes.fromhex(s)
            target = unhex(fields[5]) if len(fields) == 6 else None
            yield fields[0], int(fields[1]), unhex(fields[2]), unhex(fields[3]), fields[4], target


class RoundTrip(unittest.TestCase):
    def setUp(self):
        rng = random.Random(7)
        words = [b"alpha", b"beta", b"gamma", b"delta", b"=", b"\n", b"0x1F"]
        self.old = b" ".join(rng.choice(words) for _ in range(800))
        new = bytearray(self.old)
        new[100:105] = b"CHANGED"
        new[2000:2000] = bytes(rng.randrange(256) for _ in range(40))
        self.new = bytes(new)

    def test_encoders(self):
        for greedy in (False, True):
            with self.subTest(greedy=greedy):
                patch = tinydelta.encode(self.old, self.new, greedy=greedy)
                self.assertLess(len(patch), len(self.new) // 4)
                self.assertEqual(tinydelta.decode(self.old, patch), self.new)

    def test_bytes_like_inputs(self):
        patch = tinydelta.encode(bytearray(self.old), memoryview(self.new))
        self.assertEqual(tinydelta.decode(memoryview(self.old), bytearray(patch)), self.new)

    def test_max_instruction_len(self):
        long = tinydelta.encode(b"", bytes(500))
        short = tinydelta.encode(b"", bytes(500), max_instruction_len=10)
        self.assertGreater(len(short), len(long))
        self.assertEqual(tinydelta.decode(b"", short), bytes(500))

    def test_empty(self):
        self.assertEqual(tinydelta.encode(b"", b""), b"\x01")  # the version byte alone
        self.assertEqual(tinydelta.decode(b"", b"\x01"), b"")

    def test_max_size(self):
        patch = tinydelta.encode(self.old, self.new)
        self.assertEqual(tinydelta.decode(self.old, patch, max_size=len(self.new)), self.new)
        with self.assertRaises(tinydelta.TooLargeError):
            tinydelta.decode(self.old, patch, max_size=len(self.new) - 1)


class Errors(unittest.TestCase):
    def test_malformed_patch(self):
        with self.assertRaises(tinydelta.Error) as caught:
            tinydelta.decode(b"", b"\x01\x0d\x61")  # version 1, ADD 3 with only 1 literal byte
        self.assertNotIsInstance(caught.exception, tinydelta.TooLargeError)
        self.assertNotIn("tinydelta: ", str(caught.exception))

    def test_bad_arguments(self):
        with self.assertRaises(ValueError):
            tinydelta.decode(b"", b"", max_size=0)
        with self.assertRaises(ValueError):
            tinydelta.encode(b"", b"", max_instruction_len=-1)

    def test_missing_binary(self):
        saved = os.environ.get("TINYDELTA_BIN")
        os.environ["TINYDELTA_BIN"] = os.path.join(os.path.dirname(__file__), "no-such-binary")
        try:
            with self.assertRaises(OSError):
                tinydelta.decode(b"", b"")
        finally:
            if saved is None:
                del os.environ["TINYDELTA_BIN"]
            else:
                os.environ["TINYDELTA_BIN"] = saved


class ConformanceVectors(unittest.TestCase):
    """The decoder against every vector in testdata/vectors.txt."""

    def test_vectors(self):
        count = 0
        for name, capacity, dictionary, patch, result, target in read_vectors():
            count += 1
            with self.subTest(name):
                if result == "OK":
                    self.assertEqual(tinydelta.decode(dictionary, patch, max_size=capacity), target)
                else:
                    with self.assertRaises(tinydelta.Error) as caught:
                        tinydelta.decode(dictionary, patch, max_size=capacity)
                    self.assertEqual(isinstance(caught.exception, tinydelta.TooLargeError), result == "TOO_LARGE")
        self.assertGreater(count, 0)


class CommandLine(unittest.TestCase):
    def test_module_runs_the_tool(self):
        out = subprocess.run([sys.executable, "-m", "tinydelta", "version"], capture_output=True,
                             env=dict(os.environ, PYTHONPATH=os.pathsep.join(sys.path)))
        self.assertEqual(out.returncode, 0, out.stderr)
        self.assertTrue(out.stdout.startswith(b"tinydelta "))


if __name__ == "__main__":
    unittest.main()
