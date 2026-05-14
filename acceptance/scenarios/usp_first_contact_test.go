//go:build acceptance

package scenarios_test

import (
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/acceptance/harness"
)

// TestUSP_FirstContact_OnBoardRequest captures the OnBoardRequest a
// factory-fresh cpe-sim emits on first MQTT publish. The golden
// locks down the Record envelope (version, payload_security, from_id,
// to_id) plus the embedded Notify.OnBoardReq fields (oui,
// product_class, serial_number, agent_supported_protocol_versions).
//
// "Factory fresh" = Internal.Reboot.Cause defaulting to
// LocalFactoryReset, which the profile loader installs when the
// leaf is not pre-declared by the profile.
func TestUSP_FirstContact_OnBoardRequest(t *testing.T) {
	fix := harness.StartUSPAcceptance(t, harness.USPOptions{FirstContact: harness.FactoryReset})
	_ = harness.LaunchSimDaemon(t, fix, "--seed=1")

	payload := fix.Capture.WaitForUSPMessage(t, 5*time.Second)
	normalized, err := harness.NormalizeUSPRecord(payload)
	if err != nil {
		t.Fatalf("NormalizeUSPRecord: %v", err)
	}
	harness.CompareGolden(t, "usp_first_contact/01_onboard_request.txt", normalized)
}

// TestUSP_FirstContact_BootEvent captures the Event{Boot!} Notify a
// warm-boot cpe-sim emits on first MQTT publish. The golden locks
// down the Record envelope plus the embedded Notify.Event fields
// (event_name, params, subscription_id). The warm-boot variant
// pre-declares Internal.Reboot.Cause=LocalReboot in the profile so
// the simulator skips the OnBoardRequest branch.
func TestUSP_FirstContact_BootEvent(t *testing.T) {
	fix := harness.StartUSPAcceptance(t, harness.USPOptions{FirstContact: harness.WarmBoot})
	_ = harness.LaunchSimDaemon(t, fix, "--seed=1")

	payload := fix.Capture.WaitForUSPMessage(t, 5*time.Second)
	normalized, err := harness.NormalizeUSPRecord(payload)
	if err != nil {
		t.Fatalf("NormalizeUSPRecord: %v", err)
	}
	harness.CompareGolden(t, "usp_first_contact/02_boot_event.txt", normalized)
}
