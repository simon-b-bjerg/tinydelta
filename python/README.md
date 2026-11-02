# tinydelta for Python

Tiny binary patches for small devices: a minimal delta format with an
optimizing encoder and a decoder that fits in under 1 KB of flash. This
package creates and applies tinydelta patches from Python by running the
tinydelta command-line tool, which ships inside the wheel.

```sh
pip install tinydelta
```

Wheels are available for Linux (x86-64 and ARM64, glibc or musl) and
Windows (x64 and ARM64), for Python 3.9 and later.

## Usage

```python
import tinydelta

old = open("config-v1.bin", "rb").read()
new = open("config-v2.bin", "rb").read()

patch = tinydelta.encode(old, new)
assert tinydelta.decode(old, patch, max_size=len(new)) == new
```

- `encode(dictionary, target, *, greedy=False, max_instruction_len=0,
  coder_tables=0, window=0, two_pass=False)` returns the smallest
  `Patch` that turns `dictionary` into `target` and that the device's
  decoder, described by the options, can decode. A `Patch` is `bytes`,
  plus `passes`: 1, or 2 for a two-pass patch. `greedy` uses the fast
  greedy encoder instead of the optimal one; `max_instruction_len`
  limits the bytes any one instruction produces. The decoder options
  are below.
- `decode(dictionary, patch, *, max_size=None, passes=None)` returns the
  target, decoding a `Patch` in its number of passes (`passes` for plain
  `bytes`).
  Set `max_size` when the expected size is known: a short patch can
  describe a huge target.
- Errors raise `tinydelta.Error`; a target over `max_size` raises its
  subclass `tinydelta.TooLargeError`.

### The device's decoder

| option | the decoder | default |
|---|---|---|
| `coder_tables=N` | has the coder, with an N-byte tables buffer: allows coded patches, decoded in one pass | 0: no coder |
| `window=N` | reads its own output only from its N-byte RAM buffer, never from flash (C callback decoder, `target_read` `NULL`) | 0: no limit |
| `two_pass=True` | can decode twice, storing the intermediate patch | one pass |

```python
patch = tinydelta.encode(old, new, two_pass=True, window=256)
send(patch, passes=patch.passes)       # the device needs the number of passes
assert tinydelta.decode(old, patch) == new
```

### Coded patches

`coder_tables=N` allows a range-coded patch, decoded in one pass, when
the decoder has an N-byte buffer for its probability tables (522, 777
or 1287 bytes for the three table layouts; see the
[main README](https://github.com/simon-b-bjerg/tinydelta#coded-patches)).
Firmware patches typically come out about 40% smaller; the smallest
patch is returned, so a tiny update that codes no smaller stays plain:

```python
patch = tinydelta.encode(old, new, coder_tables=777)
assert tinydelta.decode(old, patch) == new
```

Arguments may be any bytes-like objects. The wheel also installs the
`tinydelta` command (`tinydelta encode old new patch`); `python -m
tinydelta` runs it too.

A patch has a version byte but no checksum or length of its own, so send the
dictionary's hash and the target's length and hash alongside it. See
the [tinydelta repository](https://github.com/simon-b-bjerg/tinydelta)
for the wire format, the C decoder for devices, and how to integrate it.

## License

MIT
