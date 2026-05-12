package paramtree_test

import (
	"sync"
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/internal/paramtree"
)

type writeEvent struct {
	paths []string
	kind  paramtree.WriteKind
}

func TestTreeOnWriteFiresOnSet(t *testing.T) {
	tree := paramtree.New()
	mount(t, tree, "Device.X", paramtree.TypeString, "initial", true)
	events := registerOnWrite(tree)

	if err := tree.Set("Device.X", paramtree.Value{Type: paramtree.TypeString, Raw: "updated", Writable: true}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got := events.wait(t, 1, 200*time.Millisecond)
	if got[0].kind != paramtree.WriteSet {
		t.Errorf("kind=%v want WriteSet", got[0].kind)
	}
	if len(got[0].paths) != 1 || got[0].paths[0] != "Device.X" {
		t.Errorf("paths=%v", got[0].paths)
	}
}

func TestTreeOnWriteFiresOnSetBatch(t *testing.T) {
	tree := paramtree.New()
	mount(t, tree, "Device.A", paramtree.TypeString, "1", true)
	mount(t, tree, "Device.B", paramtree.TypeString, "2", true)
	events := registerOnWrite(tree)

	if _, err := tree.SetBatch([]paramtree.Setter{
		{Path: "Device.A", Value: paramtree.Value{Type: paramtree.TypeString, Raw: "x"}},
		{Path: "Device.B", Value: paramtree.Value{Type: paramtree.TypeString, Raw: "y"}},
	}); err != nil {
		t.Fatalf("SetBatch: %v", err)
	}
	got := events.wait(t, 1, 200*time.Millisecond)
	if got[0].kind != paramtree.WriteSetBatch {
		t.Errorf("kind=%v want WriteSetBatch", got[0].kind)
	}
	if len(got[0].paths) != 2 {
		t.Fatalf("paths=%v len=%d want 2", got[0].paths, len(got[0].paths))
	}
}

func TestTreeOnWriteFiresOnSetSystem(t *testing.T) {
	tree := paramtree.New()
	mount(t, tree, "Internal.X", paramtree.TypeString, "initial", false)
	events := registerOnWrite(tree)

	if err := tree.SetSystem("Internal.X", "system"); err != nil {
		t.Fatalf("SetSystem: %v", err)
	}
	got := events.wait(t, 1, 200*time.Millisecond)
	if got[0].kind != paramtree.WriteSetSystem {
		t.Errorf("kind=%v want WriteSetSystem", got[0].kind)
	}
}

func TestTreeOnWriteFiresOnAddObject(t *testing.T) {
	tree := paramtree.New()
	if err := tree.AddTable("Device.Tbl", paramtree.NewLeaf(paramtree.Value{Type: paramtree.TypeString, Raw: "x"})); err != nil {
		t.Fatalf("AddTable: %v", err)
	}
	events := registerOnWrite(tree)

	inst, err := tree.AddObject("Device.Tbl")
	if err != nil {
		t.Fatalf("AddObject: %v", err)
	}
	got := events.wait(t, 1, 200*time.Millisecond)
	if got[0].kind != paramtree.WriteAddObject {
		t.Errorf("kind=%v want WriteAddObject", got[0].kind)
	}
	wantPath := "Device.Tbl." + itoa(inst) + "."
	if got[0].paths[0] != wantPath {
		t.Errorf("paths[0]=%q want %q", got[0].paths[0], wantPath)
	}
}

func TestTreeOnWriteFiresOnDeleteObject(t *testing.T) {
	tree := paramtree.New()
	if err := tree.AddTable("Device.Tbl", paramtree.NewLeaf(paramtree.Value{Type: paramtree.TypeString, Raw: "x"})); err != nil {
		t.Fatalf("AddTable: %v", err)
	}
	inst, _ := tree.AddObject("Device.Tbl")
	events := registerOnWrite(tree)

	if err := tree.DeleteObject("Device.Tbl." + itoa(inst)); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	got := events.wait(t, 1, 200*time.Millisecond)
	if got[0].kind != paramtree.WriteDeleteObject {
		t.Errorf("kind=%v want WriteDeleteObject", got[0].kind)
	}
	if got[0].paths[0] != "Device.Tbl."+itoa(inst)+"." {
		t.Errorf("paths[0]=%q", got[0].paths[0])
	}
}

func TestTreeOnWriteMultipleCallbacksInOrder(t *testing.T) {
	tree := paramtree.New()
	mount(t, tree, "Device.X", paramtree.TypeString, "initial", true)
	var order []int
	var mu sync.Mutex
	tree.OnWrite(func(_ []string, _ paramtree.WriteKind) { mu.Lock(); order = append(order, 1); mu.Unlock() })
	tree.OnWrite(func(_ []string, _ paramtree.WriteKind) { mu.Lock(); order = append(order, 2); mu.Unlock() })
	tree.OnWrite(func(_ []string, _ paramtree.WriteKind) { mu.Lock(); order = append(order, 3); mu.Unlock() })

	if err := tree.Set("Device.X", paramtree.Value{Type: paramtree.TypeString, Raw: "updated", Writable: true}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("order=%v want [1 2 3]", order)
	}
}

func TestTreeOnWriteDoesNotFireOnFailedSet(t *testing.T) {
	tree := paramtree.New()
	mount(t, tree, "Device.X", paramtree.TypeString, "initial", false) // read-only
	events := registerOnWrite(tree)

	err := tree.Set("Device.X", paramtree.Value{Type: paramtree.TypeString, Raw: "updated", Writable: true})
	if err == nil {
		t.Fatal("expected error setting read-only leaf")
	}
	time.Sleep(50 * time.Millisecond)
	if got := events.snapshot(); len(got) != 0 {
		t.Errorf("expected no events on failed Set, got %v", got)
	}
}

func TestTreeIsAddDeletable(t *testing.T) {
	tree := paramtree.New()
	mount(t, tree, "Device.DeviceInfo.SerialNumber", paramtree.TypeString, "SN1", false)
	if err := tree.AddTable("Device.Tbl", paramtree.NewLeaf(paramtree.Value{Type: paramtree.TypeString, Raw: "x"})); err != nil {
		t.Fatalf("AddTable: %v", err)
	}

	if !tree.IsAddDeletable("Device.Tbl") {
		t.Errorf("Device.Tbl should be add-deletable")
	}
	if tree.IsAddDeletable("Device.DeviceInfo") {
		t.Errorf("Device.DeviceInfo (singleton) should NOT be add-deletable")
	}
	if tree.IsAddDeletable("Device.DeviceInfo.SerialNumber") {
		t.Errorf("a leaf should NOT be add-deletable")
	}
	if tree.IsAddDeletable("Does.Not.Exist") {
		t.Errorf("unknown paths should return false")
	}
}

type eventRecorder struct {
	mu     sync.Mutex
	events []writeEvent
}

func registerOnWrite(tree *paramtree.Tree) *eventRecorder {
	r := &eventRecorder{}
	tree.OnWrite(func(paths []string, kind paramtree.WriteKind) {
		r.mu.Lock()
		defer r.mu.Unlock()
		cp := append([]string(nil), paths...)
		r.events = append(r.events, writeEvent{paths: cp, kind: kind})
	})
	return r
}

func (r *eventRecorder) snapshot() []writeEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]writeEvent(nil), r.events...)
}

func (r *eventRecorder) wait(t *testing.T, n int, d time.Duration) []writeEvent {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if got := r.snapshot(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d OnWrite events; got %d", n, len(r.snapshot()))
	return nil
}

func mount(t *testing.T, tree *paramtree.Tree, path string, typ paramtree.Type, raw string, writable bool) {
	t.Helper()
	if err := tree.Mount(path, paramtree.NewLeaf(paramtree.Value{Type: typ, Raw: raw, Writable: writable})); err != nil {
		t.Fatalf("Mount %s: %v", path, err)
	}
}

