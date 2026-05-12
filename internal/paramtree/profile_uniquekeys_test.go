package paramtree_test

import (
	"strings"
	"testing"

	"github.com/herder-labs/cpe-labs/internal/paramtree"
)

func TestProfileLoadUniqueKeysSingleSet(t *testing.T) {
	t.Parallel()
	body := `
objects:
  - path: Device.WiFi.SSID
    instances: 2
    uniqueKeys:
      - [SSID]
    parameters:
      - path: SSID
        value: ""
        writable: true
      - path: BSSID
        value: ""
        writable: true
`
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(body), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	sets := p.UniqueKeys["Device.WiFi.SSID."]
	if len(sets) != 1 {
		t.Fatalf("got %d sets, want 1", len(sets))
	}
	if len(sets[0]) != 1 || sets[0][0] != "SSID" {
		t.Fatalf("set[0]=%v want [SSID]", sets[0])
	}
}

func TestProfileLoadUniqueKeysMultipleSets(t *testing.T) {
	t.Parallel()
	body := `
objects:
  - path: Device.WiFi.SSID
    instances: 1
    uniqueKeys:
      - [SSID]
      - [BSSID]
    parameters:
      - path: SSID
        value: ""
      - path: BSSID
        value: ""
`
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(body), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	sets := p.UniqueKeys["Device.WiFi.SSID."]
	if len(sets) != 2 {
		t.Fatalf("got %d sets, want 2", len(sets))
	}
}

func TestProfileLoadUniqueKeysOmitted(t *testing.T) {
	t.Parallel()
	body := `
objects:
  - path: Device.WiFi.SSID
    instances: 1
    parameters:
      - path: SSID
        value: ""
`
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(body), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if _, ok := p.UniqueKeys["Device.WiFi.SSID."]; ok {
		t.Errorf("Device.WiFi.SSID. should not be in UniqueKeys when omitted")
	}
}

func TestProfileLoadUniqueKeysRejectsEmptySet(t *testing.T) {
	t.Parallel()
	body := `
objects:
  - path: Device.WiFi.SSID
    instances: 1
    uniqueKeys:
      - []
    parameters:
      - path: SSID
        value: ""
`
	_, err := paramtree.LoadProfileFromReader(strings.NewReader(body), "<test>")
	if err == nil {
		t.Fatal("expected error for empty uniqueKeys set")
	}
}
