//go:build acceptance

package scenarios_test

import (
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/acceptance/harness"
)

// TestCWMP_Periodic locks down the wire shape of the second Inform
// session — the periodic one — that a daemon-mode cpe-sim fires at
// the configured interval after the bootstrap session completes.
//
// Profile interval = 1s (scheduler minimum). With --seed=1 the
// jittered next-tick lands within ~1.0-1.1s of session-1 completion;
// total wall-clock budget for this test is 5s.
//
// CWMPRequests[0] is the bootstrap Inform (`0 BOOTSTRAP` / `1 BOOT`,
// covered by TestCWMP_FirstContact); CWMPRequests[1] is the periodic
// (`2 PERIODIC`). The mock ACS only records non-empty bodies, so
// empty-POST drain frames between the two Informs don't shift indices.
func TestCWMP_Periodic(t *testing.T) {
	fix := harness.StartCWMPAcceptance(t)
	fix.ProfilePath = harness.AcceptanceProfilePath("minimal-tr181-periodic")

	_ = harness.LaunchSimDaemon(t, fix, "--seed=1")

	deadline := time.Now().Add(5 * time.Second)
	var snapshot harness.WireCaptureSnapshot
	for {
		snapshot = fix.Capture.Snapshot()
		if len(snapshot.CWMPRequests) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected >=2 CWMP requests within 5s; got %d", len(snapshot.CWMPRequests))
		}
		time.Sleep(50 * time.Millisecond)
	}

	periodic := harness.NormalizeCWMP(snapshot.CWMPRequests[1])
	harness.CompareGolden(t, "cwmp_periodic/01_periodic_inform.xml", periodic)
}
