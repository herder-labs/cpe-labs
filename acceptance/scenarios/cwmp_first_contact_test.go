//go:build acceptance

package scenarios_test

import (
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/acceptance/harness"
)

// TestCWMP_FirstContact captures the first Inform a fresh CPE emits
// against an ACS it has never spoken to before. The wire-format
// expectations:
//
//   - SOAP/CWMP envelope with the standard four namespaces
//   - cwmp:Inform body with DeviceId carrying the profile's four
//     manufacturer leaves
//   - Event block containing the 0 BOOTSTRAP event code (and 1 BOOT
//     per the CPE BOOTSTRAP convention, when the simulator emits it)
//   - MaxEnvelopes = 1
//   - RetryCount = 0 (fresh session)
//   - ParameterList with every leaf the profile declared as a
//     bootstrap inform parameter
//
// The golden is the normalized Inform body the CPE POSTed to the
// mock ACS. Updating: `make acceptance-update` rewrites it; inspect
// the diff before committing.
func TestCWMP_FirstContact(t *testing.T) {
	fix := harness.StartCWMPAcceptance(t)

	harness.LaunchSim(t, fix, 30*time.Second, "--seed=1")

	snapshot := fix.Capture.Snapshot()
	if len(snapshot.CWMPRequests) == 0 {
		t.Fatal("no CWMP requests captured; mock ACS recorded zero POST bodies")
	}

	// The first request is the Inform. Subsequent requests are the
	// empty-POST drain loop and (if RPCs came back from the ACS)
	// method-responses; only the Inform is asserted in this scenario.
	informBody := snapshot.CWMPRequests[0]
	normalized := harness.NormalizeCWMP(informBody)

	harness.CompareGolden(t, "cwmp_first_contact/01_inform_request.xml", normalized)
}
