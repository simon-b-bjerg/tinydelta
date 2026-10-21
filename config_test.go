package tinydelta

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestRealConfigs round-trips every ordered pair of *.cfg files found in
// each subdirectory of $TINYDELTA_CONFIG_DIR (default: testdata/private,
// which is git-ignored — real config data is kept local, not committed)
// and logs optimal vs greedy patch sizes. It skips when no data is
// present, so it's safe to run anywhere.
func TestRealConfigs(t *testing.T) {
	root := os.Getenv("TINYDELTA_CONFIG_DIR")
	if root == "" {
		root = filepath.Join("testdata", "private")
	}
	dirs, _ := filepath.Glob(filepath.Join(root, "*"))
	found := false
	for _, dir := range dirs {
		cfgs, _ := filepath.Glob(filepath.Join(dir, "*.cfg"))
		if len(cfgs) < 2 {
			continue
		}
		found = true
		for _, srcPath := range cfgs {
			for _, tgtPath := range cfgs {
				if srcPath == tgtPath {
					continue
				}
				name := filepath.Base(dir) + "/" + filepath.Base(srcPath) + "->" + filepath.Base(tgtPath)
				t.Run(name, func(t *testing.T) {
					src, err := os.ReadFile(srcPath)
					if err != nil {
						t.Fatal(err)
					}
					tgt, err := os.ReadFile(tgtPath)
					if err != nil {
						t.Fatal(err)
					}
					patch := encode(src, tgt, MaxSize)
					got, err := Decode(src, patch)
					if err != nil || !bytes.Equal(got, tgt) {
						t.Fatalf("round trip failed (err %v)", err)
					}
					greedy := encodeGreedy(src, tgt, MaxSize)
					if got, err := Decode(src, greedy); err != nil || !bytes.Equal(got, tgt) {
						t.Fatalf("greedy round trip failed (err %v)", err)
					}
					t.Logf("src %d B  tgt %d B  optimal %d B  greedy %d B",
						len(src), len(tgt), len(patch), len(greedy))
				})
			}
		}
	}
	if !found {
		t.Skipf("no config pairs under %s", root)
	}
}
