package clients_test

import (
	"context"
	"strings"
	"testing"

	"github.com/herder-labs/cpe-labs/internal/clients"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
)

const fabricatorProfile = `
objects:
  - path: Device.Hosts.Host
    instances: 1
    parameters:
      - path: HostName
        value: "host-{i}"
        writable: true
      - path: IPAddress
        value: "0.0.0.0"
        writable: true
      - path: Active
        type: xsd:boolean
        value: "false"
        writable: true
`

func TestFabricatorAddsToTarget(t *testing.T) {
	tree := loadFabricatorTree(t)
	f := mustNewFabricator(t, tree, 5, 10)

	if err := f.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := instanceCount(t, tree); got != 5 {
		t.Errorf("after add-to-target, instance count=%d want 5", got)
	}
}

func TestFabricatorBoundedByChurnRate(t *testing.T) {
	tree := loadFabricatorTree(t)
	f := mustNewFabricator(t, tree, 10, 2)

	for i := 0; i < 3; i++ {
		if err := f.Tick(context.Background()); err != nil {
			t.Fatalf("Tick %d: %v", i, err)
		}
	}
	got := instanceCount(t, tree)
	// Start at 1 (profile pre-materialized), churnRate 2 → +2 per tick.
	// After 3 ticks: 1 + 6 = 7 (haven't reached target 10).
	if got != 7 {
		t.Errorf("after 3 ticks with rate 2, count=%d want 7", got)
	}
}

func TestFabricatorDropsAboveTarget(t *testing.T) {
	tree := loadFabricatorTree(t)
	// Pre-fill 6 extra rows so we're at 7 total.
	for i := 0; i < 6; i++ {
		if _, err := tree.AddObject("Device.Hosts.Host"); err != nil {
			t.Fatalf("AddObject: %v", err)
		}
	}
	f := mustNewFabricator(t, tree, 3, 2)

	if err := f.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if got := instanceCount(t, tree); got != 5 {
		t.Errorf("after one drop tick with rate 2, count=%d want 5 (was 7, dropping 2)", got)
	}
}

func TestFabricatorQuietAtTarget(t *testing.T) {
	tree := loadFabricatorTree(t)
	// Profile pre-fills 1 row.
	f := mustNewFabricator(t, tree, 1, 1)

	for i := 0; i < 5; i++ {
		if err := f.Tick(context.Background()); err != nil {
			t.Fatalf("Tick %d: %v", i, err)
		}
	}
	if got := instanceCount(t, tree); got != 1 {
		t.Errorf("at target, count drifted to %d want 1", got)
	}
}

func TestFabricatorRowDefaultsRenderSeq(t *testing.T) {
	tree := loadFabricatorTree(t)
	f, err := clients.New(clients.Options{
		Path:        "Device.Hosts.Host",
		Type:        "lanHost",
		Target:      2,
		ChurnRate:   10,
		Tree:        tree,
		RowDefaults: map[string]string{"HostName": "lan-{seq}", "IPAddress": "192.168.1.{seq:hex:02}"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := f.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	// Profile pre-mat'd .1. Target 2 → +1 row at .2. (deterministic instance).
	v, _ := tree.Get("Device.Hosts.Host.2.HostName")
	if v.Raw != "lan-2" {
		t.Errorf("HostName=%q want lan-2", v.Raw)
	}
	v, _ = tree.Get("Device.Hosts.Host.2.IPAddress")
	if v.Raw != "192.168.1.02" {
		t.Errorf("IPAddress=%q want 192.168.1.02", v.Raw)
	}
}

func TestFabricatorPathNotATableRejects(t *testing.T) {
	tree := loadFabricatorTree(t)
	_, err := clients.New(clients.Options{
		Path:      "Device.DoesNot.Exist",
		Target:    1,
		ChurnRate: 1,
		Tree:      tree,
	})
	if err == nil {
		t.Fatal("expected error for non-table path")
	}
}

func loadFabricatorTree(t *testing.T) *paramtree.Tree {
	t.Helper()
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(fabricatorProfile), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	return p.Tree
}

func mustNewFabricator(t *testing.T, tree *paramtree.Tree, target, rate int) *clients.Fabricator {
	t.Helper()
	f, err := clients.New(clients.Options{
		Path:      "Device.Hosts.Host",
		Type:      "generic",
		Target:    target,
		ChurnRate: rate,
		Tree:      tree,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return f
}

func instanceCount(t *testing.T, tree *paramtree.Tree) int {
	t.Helper()
	children, err := tree.Children("Device.Hosts.Host")
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	count := 0
	for _, c := range children {
		if strings.HasSuffix(c.Name, ".") {
			count++
		}
	}
	return count
}
