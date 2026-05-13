//go:build acceptance

package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareGolden_MissingFixture(t *testing.T) {
	if *update {
		t.Skip("-update would write the fixture; missing-fixture path is the negative case")
	}
	name := "harness_selftest/missing.txt"
	defer os.RemoveAll(filepath.Join(repoRoot(), "acceptance", "golden", "harness_selftest"))

	rec := &recordingTB{}
	CompareGolden(rec, name, []byte("anything"))
	if !rec.failed {
		t.Errorf("expected CompareGolden to fail on missing fixture")
	}
}

func TestCompareGolden_Match(t *testing.T) {
	dir := filepath.Join(repoRoot(), "acceptance", "golden", "harness_selftest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	body := []byte("hello\n")
	if err := os.WriteFile(filepath.Join(dir, "match.txt"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &recordingTB{}
	CompareGolden(rec, "harness_selftest/match.txt", body)
	if rec.failed {
		t.Errorf("expected CompareGolden to pass on byte-equal match, got: %s", rec.log)
	}
}

func TestCompareGolden_Mismatch(t *testing.T) {
	if *update {
		t.Skip("-update would rewrite the fixture; mismatch path is the negative case")
	}
	dir := filepath.Join(repoRoot(), "acceptance", "golden", "harness_selftest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "mismatch.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &recordingTB{}
	CompareGolden(rec, "harness_selftest/mismatch.txt", []byte("beta\n"))
	if !rec.failed {
		t.Errorf("expected CompareGolden to fail on mismatch")
	}
}

// recordingTB implements TB and records whether any failing methods
// were called. Used by self-tests to assert CompareGolden's pass/fail
// behavior without aborting the surrounding *testing.T.
type recordingTB struct {
	failed bool
	log    string
}

func (r *recordingTB) Helper()                           {}
func (r *recordingTB) Errorf(format string, args ...any) { r.failed = true; r.log += fmt.Sprintf(format, args...) + "\n" }
func (r *recordingTB) Fatalf(format string, args ...any) { r.failed = true; r.log += fmt.Sprintf(format, args...) + "\n" }
func (r *recordingTB) Logf(format string, args ...any)   { r.log += fmt.Sprintf(format, args...) + "\n" }
