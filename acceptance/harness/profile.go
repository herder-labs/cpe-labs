//go:build acceptance

package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// profileTB is the subset of *testing.T MaterializeProfile uses. Same
// shape as harness.TB plus TempDir; widened separately so the helper
// keeps the t.TempDir() ergonomics while staying mockable.
type profileTB interface {
	TB
	TempDir() string
}

// MaterializeProfile copies every file from src into a fresh
// subdirectory under t.TempDir(), substituting each __KEY__ token in
// the file contents with the matching value from vars. Returns the
// absolute path of the materialized profile directory ready to be
// passed to cpe-sim's --profile flag.
//
// The placeholder syntax (double-underscore-bracketed keys) is
// unambiguous in TR-paths and YAML quote-safe; substitution is a
// straight strings.ReplaceAll, no regex, no escaping.
//
// Used by StartUSPAcceptance to bake the broker host:port into a
// USP-enabled profile per scenario.
func MaterializeProfile(t profileTB, src string, vars map[string]string) string {
	t.Helper()
	abs, err := filepath.Abs(src)
	if err != nil {
		t.Fatalf("MaterializeProfile: abs %s: %v", src, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("MaterializeProfile: stat %s: %v", abs, err)
	}
	if !info.IsDir() {
		t.Fatalf("MaterializeProfile: src %s is not a directory", abs)
	}

	dst := filepath.Join(t.TempDir(), filepath.Base(abs))
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("MaterializeProfile: mkdir %s: %v", dst, err)
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		t.Fatalf("MaterializeProfile: readdir %s: %v", abs, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			// Profile loader is single-level glob; subdirs would
			// surprise. Fail loudly rather than skip.
			t.Fatalf("MaterializeProfile: src %s contains subdir %s; profiles are single-level", abs, e.Name())
		}
		body, err := os.ReadFile(filepath.Join(abs, e.Name()))
		if err != nil {
			t.Fatalf("MaterializeProfile: read %s: %v", e.Name(), err)
		}
		out := string(body)
		for k, v := range vars {
			placeholder := fmt.Sprintf("__%s__", k)
			out = strings.ReplaceAll(out, placeholder, v)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), []byte(out), 0o644); err != nil {
			t.Fatalf("MaterializeProfile: write %s: %v", e.Name(), err)
		}
	}
	return dst
}
