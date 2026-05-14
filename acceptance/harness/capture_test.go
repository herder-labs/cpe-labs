//go:build acceptance

package harness

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

func TestWaitForUSPMessage_DrainsInOrder(t *testing.T) {
	c := NewWireCapture()
	c.AppendUSPMessage([]byte("first"))
	c.AppendUSPMessage([]byte("second"))

	got := c.WaitForUSPMessage(t, 100*time.Millisecond)
	if !bytes.Equal(got, []byte("first")) {
		t.Errorf("first call returned %q, want %q", got, "first")
	}
	got = c.WaitForUSPMessage(t, 100*time.Millisecond)
	if !bytes.Equal(got, []byte("second")) {
		t.Errorf("second call returned %q, want %q", got, "second")
	}
}

func TestWaitForUSPMessage_BlocksUntilAppend(t *testing.T) {
	c := NewWireCapture()
	gotCh := make(chan []byte, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		gotCh <- c.WaitForUSPMessage(t, 500*time.Millisecond)
	}()

	// Confirm Wait is parked.
	select {
	case <-gotCh:
		t.Fatal("Wait returned before any AppendUSPMessage")
	case <-time.After(30 * time.Millisecond):
	}

	c.AppendUSPMessage([]byte("late"))
	got := <-gotCh
	if !bytes.Equal(got, []byte("late")) {
		t.Errorf("got %q, want %q", got, "late")
	}
	wg.Wait()
}

func TestWaitForUSPMessage_Deadline(t *testing.T) {
	c := NewWireCapture()
	// Use a separate t-like fake so the timeout doesn't fail this test.
	rec := &recordingTB{}
	ok := make(chan struct{})
	go func() {
		c.waitForUSPMessageTB(rec, 30*time.Millisecond)
		close(ok)
	}()
	select {
	case <-ok:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("WaitForUSPMessage did not return within 500ms on a 30ms deadline")
	}
	if !rec.failed {
		t.Errorf("expected deadline to fail t")
	}
}

func TestSnapshot_PreservesFullHistoryAcrossDrains(t *testing.T) {
	c := NewWireCapture()
	c.AppendUSPMessage([]byte("a"))
	c.AppendUSPMessage([]byte("b"))
	_ = c.WaitForUSPMessage(t, 100*time.Millisecond)

	snap := c.Snapshot()
	if len(snap.USPMessages) != 2 {
		t.Errorf("Snapshot returned %d messages, want 2", len(snap.USPMessages))
	}
}
