//go:build acceptance

package scenarios_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/acceptance/harness"
)

// TestCWMP_ConnectionRequestRoundtrip captures the Inform a CPE
// fires in response to an ACS-initiated Connection-Request (TR-069
// §3.2.2). Sequence:
//
//  1. Pre-pick a free localhost port; pass it as --cr-bind-addr so
//     the test knows the CR URL out of band.
//  2. cpe-sim boots in daemon mode. Bootstrap Inform fires; CR
//     listener is live shortly after.
//  3. Test issues a GET against the URL using digest auth (RFC 7616
//     qop=auth/MD5).
//  4. cpe-sim opens a follow-on session whose Inform's Event block
//     contains 6 CONNECTION REQUEST. Captured as CWMPRequests[1].
//
// Why pre-pick the port instead of --cr-bind-addr=127.0.0.1:0 and
// reading the URL from the bootstrap Inform's ParameterList:
// cpe-sim publishes the URL to the tree at registerCREndpoint time,
// BEFORE listener.Start() binds the port. The published value is
// therefore empty regardless of bind-addr. The bootstrap Inform
// would carry an empty <Value>. See cmd/cpe-sim/main.go:1244 +
// internal/cwmp/cr/listener.go::URL for the ordering bug; until it's
// fixed, scenarios that need the URL out of cpe-sim either ask cpe-sim
// after Start (via GPV) or pre-pick the port. The latter is simpler.
func TestCWMP_ConnectionRequestRoundtrip(t *testing.T) {
	fix := harness.StartCWMPAcceptance(t)
	fix.ProfilePath = harness.AcceptanceProfilePath("minimal-tr181-cr")

	port := harness.PickFreePort(t)
	bindAddr := fmt.Sprintf("127.0.0.1:%d", port)
	crURL := fmt.Sprintf("http://%s/cr", bindAddr)

	_ = harness.LaunchSimDaemon(t, fix,
		"--seed=1",
		"--cr-bind-addr="+bindAddr,
		"--cr-publish-path=Device.ManagementServer.ConnectionRequestURL",
	)

	// Wait for bootstrap so we know the CR listener is live.
	_ = fix.Capture.WaitForCWMPRequestCount(t, 1, 5*time.Second)

	// Issue the digest CR. Expect 200.
	resp := harness.DigestGet(t, crURL, "cpe-cr", "cpe-cr-pass")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("CR GET status %d, want 200", resp.StatusCode)
	}

	// Wait for the post-CR Inform, golden it.
	snap := fix.Capture.WaitForCWMPRequestCount(t, 2, 5*time.Second)
	body := harness.NormalizeCWMP(snap.CWMPRequests[1])
	harness.CompareGolden(t, "cwmp_cr_roundtrip/01_cr_triggered_inform.xml", body)
}
