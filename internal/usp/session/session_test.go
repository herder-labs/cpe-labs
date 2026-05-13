package session_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/internal/paramtree"
	"github.com/herder-labs/cpe-labs/internal/usp/codec"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

func TestSessionEmitsOnBoardRequestOnFactoryReset(t *testing.T) {
	tree := buildTreeWithRebootCause(t, session.RebootCauseFactoryRst)
	fa := newFakeAdapter()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- session.Run(ctx, sessionOpts(tree, fa))
	}()

	wire := fa.awaitSend(t, 2*time.Second)
	_, msg, err := codec.UnwrapRecord(wire)
	if err != nil {
		t.Fatalf("UnwrapRecord: %v", err)
	}
	req := msg.GetBody().GetRequest().GetNotify().GetOnBoardReq()
	if req == nil {
		t.Fatalf("expected OnBoardRequest, got %+v", msg)
	}

	// Leaf must flip after emit.
	val, err := tree.Get(session.RebootCausePath)
	if err != nil {
		t.Fatalf("Get RebootCause: %v", err)
	}
	if val.Raw != session.RebootCauseLocalBoot {
		t.Fatalf("RebootCause=%q want %q", val.Raw, session.RebootCauseLocalBoot)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestSessionEmitsBootEventOnSubsequentContact(t *testing.T) {
	tree := buildTreeWithRebootCause(t, session.RebootCauseLocalBoot)
	fa := newFakeAdapter()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- session.Run(ctx, sessionOpts(tree, fa))
	}()

	wire := fa.awaitSend(t, 2*time.Second)
	_, msg, err := codec.UnwrapRecord(wire)
	if err != nil {
		t.Fatalf("UnwrapRecord: %v", err)
	}
	ev := msg.GetBody().GetRequest().GetNotify().GetEvent()
	if ev == nil {
		t.Fatalf("expected Event{Boot!}, got %+v", msg)
	}
	if ev.GetEventName() != "Boot!" {
		t.Fatalf("EventName=%q", ev.GetEventName())
	}

	// Leaf must NOT flip when already past first contact.
	val, _ := tree.Get(session.RebootCausePath)
	if val.Raw != session.RebootCauseLocalBoot {
		t.Fatalf("RebootCause=%q (should be unchanged)", val.Raw)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestSessionContextCancellationClosesAdapter(t *testing.T) {
	tree := buildTreeWithRebootCause(t, session.RebootCauseFactoryRst)
	fa := newFakeAdapter()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- session.Run(ctx, sessionOpts(tree, fa))
	}()

	_ = fa.awaitSend(t, 2*time.Second)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not exit on cancel")
	}
	if !fa.closed() {
		t.Fatalf("adapter not closed on cancel")
	}
}

func TestSessionRejectsNilTree(t *testing.T) {
	err := session.Run(context.Background(), session.Options{
		Adapter: newFakeAdapter(), AgentEID: "os::a", ControllerEID: "self::b",
	})
	if err == nil {
		t.Fatal("expected error for nil Tree")
	}
}

func buildTreeWithRebootCause(t *testing.T, cause string) *paramtree.Tree {
	t.Helper()
	tree := paramtree.New()
	mustMount(t, tree, "Device.DeviceInfo.ManufacturerOUI", "001122")
	mustMount(t, tree, "Device.DeviceInfo.ProductClass", "Generic")
	mustMount(t, tree, "Device.DeviceInfo.SerialNumber", "SN123")
	if err := tree.Mount(session.RebootCausePath, paramtree.NewLeaf(paramtree.Value{Type: paramtree.TypeString, Raw: cause, Writable: false})); err != nil {
		t.Fatalf("mount reboot cause: %v", err)
	}
	return tree
}

func mustMount(t *testing.T, tree *paramtree.Tree, path, raw string) {
	t.Helper()
	if err := tree.Mount(path, paramtree.NewLeaf(paramtree.Value{Type: paramtree.TypeString, Raw: raw, Writable: false})); err != nil {
		t.Fatalf("Mount %s: %v", path, err)
	}
}

func sessionOpts(tree *paramtree.Tree, fa *fakeAdapter) session.Options {
	return session.Options{
		Tree:          tree,
		Adapter:       fa,
		AgentEID:      "os::001122SN123",
		ControllerEID: "self::herder",
	}
}

type fakeAdapter struct {
	mu        sync.Mutex
	sent      chan []byte
	recv      chan []byte
	connected bool
	isClosed  bool
}

func newFakeAdapter() *fakeAdapter {
	return &fakeAdapter{
		sent: make(chan []byte, 8),
		recv: make(chan []byte, 8),
	}
}

func (f *fakeAdapter) Connect(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected = true
	return nil
}

func (f *fakeAdapter) Send(_ context.Context, b []byte) error {
	cp := make([]byte, len(b))
	copy(cp, b)
	f.sent <- cp
	return nil
}

func (f *fakeAdapter) Recv() <-chan []byte { return f.recv }

func (f *fakeAdapter) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isClosed {
		return nil
	}
	f.isClosed = true
	close(f.recv)
	return nil
}

func (f *fakeAdapter) closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.isClosed
}

func (f *fakeAdapter) awaitSend(t *testing.T, d time.Duration) []byte {
	t.Helper()
	select {
	case b := <-f.sent:
		return b
	case <-time.After(d):
		t.Fatalf("timed out waiting for Send")
		return nil
	}
}
