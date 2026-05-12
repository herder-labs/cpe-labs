package paramtree_test

import (
	"strings"
	"testing"

	"github.com/herder-labs/cpe-labs/internal/paramtree"
)

const clientsProfile = `
objects:
  - path: Device.Hosts.Host
    instances: 1
    parameters:
      - path: HostName
        value: "host-{i}"
        writable: true
      - path: IPAddress
        value: "192.168.1.{i}"
        writable: true
      - path: Active
        type: xsd:boolean
        value: "true"
        writable: true

clients:
  - path: Device.Hosts.Host
    type: lanHost
    targetCount: 8
    churnInterval: 30s
    churnRate: 2
    rowDefaults:
      IPAddress: "192.168.1.{seq}"
      HostName: "host-{seq}"
`

func TestProfileLoadClientsHappyPath(t *testing.T) {
	t.Parallel()
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(clientsProfile), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if len(p.Clients) != 1 {
		t.Fatalf("got %d clients, want 1", len(p.Clients))
	}
	c := p.Clients[0]
	if c.Path != "Device.Hosts.Host" {
		t.Errorf("Path=%q", c.Path)
	}
	if c.TargetCount != 8 || c.ChurnRate != 2 {
		t.Errorf("TargetCount/ChurnRate=%d/%d", c.TargetCount, c.ChurnRate)
	}
	if c.ChurnInterval.Seconds() != 30 {
		t.Errorf("ChurnInterval=%v", c.ChurnInterval)
	}
	if c.RowDefaults["IPAddress"] != "192.168.1.{seq}" {
		t.Errorf("RowDefaults[IPAddress]=%q", c.RowDefaults["IPAddress"])
	}
}

func TestProfileLoadClientsRejectsUnknownPath(t *testing.T) {
	t.Parallel()
	body := `
clients:
  - path: Device.DoesNot.Exist
    targetCount: 1
    churnInterval: 30s
    churnRate: 1
`
	_, err := paramtree.LoadProfileFromReader(strings.NewReader(body), "<test>")
	if err == nil {
		t.Fatal("expected error for unknown path")
	}
}

func TestProfileLoadClientsRejectsZeroChurnRate(t *testing.T) {
	t.Parallel()
	body := strings.Replace(clientsProfile, "churnRate: 2", "churnRate: 0", 1)
	_, err := paramtree.LoadProfileFromReader(strings.NewReader(body), "<test>")
	if err == nil {
		t.Fatal("expected error for churnRate: 0")
	}
}

func TestProfileLoadClientsRejectsNegativeTarget(t *testing.T) {
	t.Parallel()
	body := strings.Replace(clientsProfile, "targetCount: 8", "targetCount: -1", 1)
	_, err := paramtree.LoadProfileFromReader(strings.NewReader(body), "<test>")
	if err == nil {
		t.Fatal("expected error for targetCount: -1")
	}
}

func TestProfileLoadClientsRejectsBadInterval(t *testing.T) {
	t.Parallel()
	body := strings.Replace(clientsProfile, "churnInterval: 30s", "churnInterval: not-a-duration", 1)
	_, err := paramtree.LoadProfileFromReader(strings.NewReader(body), "<test>")
	if err == nil {
		t.Fatal("expected error for malformed churnInterval")
	}
}

func TestProfileLoadClientsRejectsDuplicatePath(t *testing.T) {
	t.Parallel()
	body := clientsProfile + `
  - path: Device.Hosts.Host
    targetCount: 4
    churnInterval: 10s
    churnRate: 1
`
	_, err := paramtree.LoadProfileFromReader(strings.NewReader(body), "<test>")
	if err == nil {
		t.Fatal("expected error for duplicate fabricator path")
	}
}
