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
  for_compression=False, max_distance=0, decoder_ram=0)` returns a patch
  that turns `dictionary` into `target`. `greedy` uses the fast greedy
  encoder instead of the optimal one; `max_instruction_len` limits the
  bytes any one instruction produces. `for_compression` and
  `max_distance` are for compression mode, `decoder_ram` for coded
  patches, below.
- `decode(dictionary, patch, *, max_size=None)` returns the target.
  Set `max_size` when the expected size is known: a short patch can
  describe a huge target.
- Errors raise `tinydelta.Error`; a target over `max_size` raises its
  subclass `tinydelta.TooLargeError`.

### Compression mode

For updates that change much of the file, such as firmware where code
moved, encode twice and send whichever patch is smaller. The device
decodes it once or twice with the same decoder, so send the number of
passes with the patch:

```python
plain = tinydelta.encode(old, new)
twice = tinydelta.encode(b"", tinydelta.encode(old, new, for_compression=True))
patch, passes = (twice, 2) if len(twice) < len(plain) else (plain, 1)

# On the receiving side:
inner = tinydelta.decode(b"", patch) if passes == 2 else patch
assert tinydelta.decode(old, inner) == new
```

`max_distance=N` limits how far back a patch reads its own output, so
the C callback decoder needs no `target_read` with an N-byte buffer.

### Coded patches

`decoder_ram=N` allows a range-coded patch, decoded in one pass, when the
decoder can lend N bytes of RAM to its probability tables (522, 777 or
1287 bytes for the three table layouts; see the
[main README](https://github.com/simon-b-bjerg/tinydelta#coded-patches)).
Firmware patches typically come out about 40% smaller; the smallest
patch is returned, so a tiny update that codes no smaller stays plain:

```python
patch = tinydelta.encode(old, new, decoder_ram=777)
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
