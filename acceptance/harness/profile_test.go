//go:build acceptance

package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeProfile_Substitutes(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.yaml"), []byte(`broker:
  address: __HOST__
  port: __PORT__
  unrelated: noun
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "b.yaml"), []byte(`no_placeholders: true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	dst := MaterializeProfile(t, src, map[string]string{
		"HOST": "127.0.0.1",
		"PORT": "1234",
	})

	gotA, _ := os.ReadFile(filepath.Join(dst, "a.yaml"))
	if !strings.Contains(string(gotA), "address: 127.0.0.1") {
		t.Errorf("HOST not substituted: %s", gotA)
	}
	if !strings.Contains(string(gotA), "port: 1234") {
		t.Errorf("PORT not substituted: %s", gotA)
	}
	if !strings.Contains(string(gotA), "unrelated: noun") {
		t.Errorf("unrelated content lost: %s", gotA)
	}
	gotB, _ := os.ReadFile(filepath.Join(dst, "b.yaml"))
	if string(gotB) != "no_placeholders: true\n" {
		t.Errorf("placeholder-free file modified: %q", gotB)
	}
}
