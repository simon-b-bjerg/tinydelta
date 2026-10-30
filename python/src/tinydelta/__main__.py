"""Runs the bundled tinydelta command-line tool, as `python -m tinydelta`
or as the `tinydelta` command the wheel installs."""

import subprocess
import sys

from . import Error, binary_path


def main():
    try:
        binary = binary_path()
    except Error as e:
        print("tinydelta:", e, file=sys.stderr)
        return 1
    return subprocess.call([binary] + sys.argv[1:])


if __name__ == "__main__":
    sys.exit(main())
