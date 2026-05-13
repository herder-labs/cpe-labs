//go:build acceptance

package harness

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// TB is the subset of *testing.T that CompareGolden and GoldenPath
// use. *testing.T satisfies it implicitly, so callers pass it
// directly. Tests of this package fake it to capture failures.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
}

var update = flag.Bool("update", false, "rewrite acceptance/golden/* fixtures from actual test output")

const (
	goldenDirName = "golden"
	dirMode       = 0o755
	fileMode      = 0o644
	contextSize   = 16
)

// CompareGolden diffs got against acceptance/golden/<name>. On
// mismatch it fails t with a position-anchored hexdump. With -update
// set, it writes got to that path and passes.
//
// name is taken verbatim and may contain subdirectories
// (e.g. "cwmp_first_contact/01_inform_request.xml"). The caller picks
// the extension that matches the content (.xml, .txt, .json, .pb).
//
// Modeled on internal/testgolden.Compare; reimplemented here because
// internal/testgolden expects testdata/golden/ relative to the test's
// package and operates on opaque blobs. Acceptance goldens live at a
// fixed location (acceptance/golden/) regardless of the scenario's
// directory.
func CompareGolden(t TB, name string, got []byte) {
	t.Helper()
	path := filepath.Join(repoRoot(), "acceptance", goldenDirName, name)

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
			t.Fatalf("acceptance: mkdir %s: %v", filepath.Dir(path), err)
			return
		}
		if err := os.WriteFile(path, got, fileMode); err != nil {
			t.Fatalf("acceptance: write %s: %v", path, err)
			return
		}
		t.Logf("acceptance: updated %s", path)
		return
	}

	want, err := os.ReadFile(path) //nolint:gosec // path is under acceptance/golden
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Errorf("acceptance: fixture %s does not exist; run `make acceptance-update` to create it", path)
			return
		}
		t.Errorf("acceptance: read %s: %v", path, err)
		return
	}

	if bytes.Equal(want, got) {
		return
	}

	t.Errorf("acceptance: %s mismatch\n%s", path, formatGoldenDiff(want, got))
}

// GoldenPath returns the absolute path CompareGolden would read or
// write for name. Useful for tests that want to read the fixture
// directly (e.g. assertions on its parse).
func GoldenPath(t TB, name string) string {
	t.Helper()
	return filepath.Join(repoRoot(), "acceptance", goldenDirName, name)
}

func formatGoldenDiff(want, got []byte) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "expected %d bytes, got %d bytes\n", len(want), len(got))

	idx := firstDiffIndex(want, got)
	if idx < 0 {
		return b.String()
	}
	switch {
	case idx >= len(want):
		fmt.Fprintf(&b, "got is longer than expected (diverges at byte %d)\n", idx)
	case idx >= len(got):
		fmt.Fprintf(&b, "got is shorter than expected (diverges at byte %d)\n", idx)
	default:
		fmt.Fprintf(&b, "first divergence at byte %d\n", idx)
	}

	fmt.Fprintln(&b, "--- want")
	fmt.Fprintln(&b, hexGoldenWindow(want, idx))
	fmt.Fprintln(&b, "--- got")
	fmt.Fprint(&b, hexGoldenWindow(got, idx))
	return b.String()
}

func firstDiffIndex(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}

func hexGoldenWindow(buf []byte, idx int) string {
	if idx >= len(buf) {
		return "<EOF>"
	}
	start := idx - contextSize
	if start < 0 {
		start = 0
	}
	end := idx + contextSize
	if end > len(buf) {
		end = len(buf)
	}
	window := buf[start:end]

	var hexPart, asciiPart bytes.Buffer
	for i, c := range window {
		if i > 0 {
			hexPart.WriteByte(' ')
		}
		fmt.Fprintf(&hexPart, "%02x", c)
		if c >= 0x20 && c < 0x7f {
			asciiPart.WriteByte(c)
		} else {
			asciiPart.WriteByte('.')
		}
	}
	return fmt.Sprintf("@%d: %s | %s", start, hexPart.String(), asciiPart.String())
}
