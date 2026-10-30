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

- `encode(dictionary, target, *, greedy=False, max_instruction_len=0)`
  returns a patch that turns `dictionary` into `target`. `greedy` uses
  the fast greedy encoder instead of the optimal one; `max_instruction_len`
  limits the bytes any one instruction produces.
- `decode(dictionary, patch, *, max_size=None)` returns the target.
  Set `max_size` when the expected size is known: a short patch can
  describe a huge target.
- Errors raise `tinydelta.Error`; a target over `max_size` raises its
  subclass `tinydelta.TooLargeError`.

Arguments may be any bytes-like objects. The wheel also installs the
`tinydelta` command (`tinydelta encode old new patch`); `python -m
tinydelta` runs it too.

A patch has no header, checksum or length of its own, so send the
dictionary's hash and the target's length and hash alongside it. See
the [tinydelta repository](https://github.com/simon-b-bjerg/tinydelta)
for the wire format, the C decoder for devices, and how to integrate it.

## License

MIT
