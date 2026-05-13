//go:build acceptance

package harness

import "sync"

// WireCapture is the per-scenario byte recorder. CWMP requests come
// from the mock ACS's POST handler; USP messages come from a paho
// client subscribed to usp/v1/controller. Both are stored verbatim;
// scenarios feed them to Normalize* before golden comparison.
type WireCapture struct {
	mu       sync.Mutex
	cwmpReq  [][]byte
	uspBytes [][]byte
}

// AppendCWMPRequest records a CWMP POST body sent by the CPE to the
// mock ACS. Called from the mock ACS handler goroutine.
func (w *WireCapture) AppendCWMPRequest(body []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cwmpReq = append(w.cwmpReq, append([]byte(nil), body...))
}

// AppendUSPMessage records a payload published to usp/v1/controller.
// Called from the controller-side subscriber goroutine.
func (w *WireCapture) AppendUSPMessage(payload []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.uspBytes = append(w.uspBytes, append([]byte(nil), payload...))
}

// Snapshot returns a defensive copy of every captured byte slice.
// Safe to call while the scenario is still running; further captures
// after Snapshot do not affect the returned slices.
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
