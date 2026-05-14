//go:build acceptance

package harness

import (
	"sync"
	"testing"
	"time"
)

// WireCapture is the per-scenario byte recorder. CWMP requests come
// from the mock ACS's POST handler; USP messages come from a paho
// client subscribed to usp/v1/controller. Both are stored verbatim;
// scenarios feed them to Normalize* before golden comparison.
type WireCapture struct {
	mu       sync.Mutex
	cwmpReq  [][]byte
	uspBytes [][]byte

	// uspCursor is the index of the next USP payload
	// WaitForUSPMessage will return. Increments on each successful
	// drain. uspBytes is append-only; this cursor preserves order.
	uspCursor int

	// uspWake fires whenever AppendUSPMessage runs so a parked
	// WaitForUSPMessage wakes up. Unbuffered + non-blocking send so
	// the producer never stalls.
	uspWake chan struct{}
}

// NewWireCapture returns an initialized capture. Constructor is
// required (not a zero-value struct) so the wake channel is wired up.
func NewWireCapture() *WireCapture {
	return &WireCapture{uspWake: make(chan struct{}, 1)}
}

// AppendCWMPRequest records a CWMP POST body sent by the CPE to the
// mock ACS. Called from the mock ACS handler goroutine.
func (w *WireCapture) AppendCWMPRequest(body []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cwmpReq = append(w.cwmpReq, append([]byte(nil), body...))
}

// AppendUSPMessage records a payload published to usp/v1/controller.
// Called from the controller-side subscriber goroutine. Non-blocking
// wakeup: if a WaitForUSPMessage is already parked, it fires; if not,
// the buffered channel keeps a single pending wakeup until the next
// Wait drains it.
func (w *WireCapture) AppendUSPMessage(payload []byte) {
	w.mu.Lock()
	w.uspBytes = append(w.uspBytes, append([]byte(nil), payload...))
	w.mu.Unlock()
	select {
	case w.uspWake <- struct{}{}:
	default:
	}
}

// WaitForUSPMessage blocks until the next unread USP payload is
// captured or the deadline elapses. Returns the FIRST unread payload;
// subsequent captures remain queued for follow-up calls. Scenarios
// that drain N payloads call this helper N times.
//
// On deadline, t.Fatalf with a summary of how many payloads were
// captured before the timeout.
func (w *WireCapture) WaitForUSPMessage(t *testing.T, deadline time.Duration) []byte {
	t.Helper()
	return w.waitForUSPMessageTB(t, deadline)
}

// waitForUSPMessageTB is the TB-typed body shared between
// WaitForUSPMessage (production callers) and the self-tests that
// need to fake a *testing.T to assert deadline behavior.
func (w *WireCapture) waitForUSPMessageTB(t TB, deadline time.Duration) []byte {
	t.Helper()
	end := time.Now().Add(deadline)
	for {
		w.mu.Lock()
		if w.uspCursor < len(w.uspBytes) {
			payload := w.uspBytes[w.uspCursor]
			w.uspCursor++
			w.mu.Unlock()
			return append([]byte(nil), payload...)
		}
		w.mu.Unlock()

		remaining := time.Until(end)
		if remaining <= 0 {
			w.mu.Lock()
			total := len(w.uspBytes)
			w.mu.Unlock()
			t.Fatalf("WaitForUSPMessage: timed out after %s; captured %d total payloads, %d already drained",
				deadline, total, w.uspCursor)
			return nil
		}

		select {
		case <-w.uspWake:
			// loop back and re-check under the lock
		case <-time.After(remaining):
			// next iteration's deadline check fails fast
		}
	}
}

// Snapshot returns a defensive copy of every captured byte slice.
// Safe to call while the scenario is still running; further captures
// after Snapshot do not affect the returned slices. Does NOT advance
// the WaitForUSPMessage cursor.
func (w *WireCapture) Snapshot() WireCaptureSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := WireCaptureSnapshot{
		CWMPRequests: make([][]byte, len(w.cwmpReq)),
		USPMessages:  make([][]byte, len(w.uspBytes)),
	}
	for i, b := range w.cwmpReq {
		out.CWMPRequests[i] = append([]byte(nil), b...)
	}
	for i, b := range w.uspBytes {
		out.USPMessages[i] = append([]byte(nil), b...)
	}
	return out
}

// WireCaptureSnapshot is an immutable view of a WireCapture at a
// point in time.
type WireCaptureSnapshot struct {
	CWMPRequests [][]byte
	USPMessages  [][]byte
}
