package tinydelta

import (
	"math"
	"os"
	"strings"
	"testing"
)

// TestEncodeUnlimited encodes one file pair with the optimal encoder's
// size limit lifted, for the benchmark (bench/run.py): Encode falls back to
// the greedy encoder above roughly 100 KB to bound its memory, and this
// shows what optimal encoding gives on larger files such as firmware. It
// reads "<dictionary> <target> <patch>" from $TINYDELTA_ENCODE_UNLIMITED and
// is skipped when that is unset. Memory grows with the target, about 700
// bytes per target byte.
func TestEncodeUnlimited(t *testing.T) {
	spec := os.Getenv("TINYDELTA_ENCODE_UNLIMITED")
	if spec == "" {
		t.Skip("TINYDELTA_ENCODE_UNLIMITED not set")
	}
	paths := strings.Fields(spec)
	if len(paths) != 3 {
		t.Fatalf("TINYDELTA_ENCODE_UNLIMITED must be \"<dictionary> <target> <patch>\", got %q", spec)
	}
	dict, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.ReadFile(paths[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := checkSizes(dict, target); err != nil {
		t.Fatal(err)
	}
	saved := optMaxStates
	optMaxStates = math.MaxInt
	defer func() { optMaxStates = saved }()
	if err := os.WriteFile(paths[2], withHeader(encode(dict, target, MaxSize)), 0o644); err != nil {
		t.Fatal(err)
	}
}
