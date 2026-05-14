//go:build acceptance

package scenarios_test

import (
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/acceptance/harness"
)

// TestCWMP_ConnectionRequestRoundtrip captures the Inform a CPE
// fires in response to an ACS-initiated Connection-Request (TR-069
// §3.2.2). Sequence:
//
//  1. cpe-sim boots in daemon mode with --cr-bind-addr=127.0.0.1:0;
//     listener.Start binds a random port; cpe-sim publishes the
//     resolved URL into Device.ManagementServer.ConnectionRequestURL.
//  2. Bootstrap Inform fires, carrying the published URL in its
//     ParameterList per the profile's informParameters.bootstrap.
//  3. Test extracts the URL from the captured Inform body.
//  4. Test issues a GET against the URL using digest auth (RFC 7616
//     qop=auth/MD5).
//  5. cpe-sim opens a follow-on session whose Inform's Event block
//     contains 6 CONNECTION REQUEST. Captured as CWMPRequests[1].
func TestCWMP_ConnectionRequestRoundtrip(t *testing.T) {
	fix := harness.StartCWMPAcceptance(t)
	fix.ProfilePath = harness.AcceptanceProfilePath("minimal-tr181-cr")

	_ = harness.LaunchSimDaemon(t, fix,
		"--seed=1",
		"--cr-bind-addr=127.0.0.1:0",
		"--cr-publish-path=Device.ManagementServer.ConnectionRequestURL",
	)

	// 1+2: wait for bootstrap, extract CR URL from its ParameterList.
	snap := fix.Capture.WaitForCWMPRequestCount(t, 1, 5*time.Second)
	crURL, err := harness.ExtractCWMPParameter(
		snap.CWMPRequests[0],
		"Device.ManagementServer.ConnectionRequestURL",
	)
	if err != nil {
		t.Fatalf("extract CR URL: %v", err)
	}
	if crURL == "" {
		t.Fatalf("CR URL empty in bootstrap Inform; expected resolved URL")
	}

	// 3. Digest GET, expect 200.
	resp := harness.DigestGet(t, crURL, "cpe-cr", "cpe-cr-pass")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("CR GET status %d, want 200", resp.StatusCode)
	}

	// 4. Wait for post-CR Inform, golden it.
	snap = fix.Capture.WaitForCWMPRequestCount(t, 2, 5*time.Second)
	body := harness.NormalizeCWMP(snap.CWMPRequests[1])
	harness.CompareGolden(t, "cwmp_cr_roundtrip/01_cr_triggered_inform.xml", body)
}
