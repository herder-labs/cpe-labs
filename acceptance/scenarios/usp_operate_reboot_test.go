//go:build acceptance

package scenarios_test

import (
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/acceptance/harness"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

// TestUSP_OperateReboot captures the wire shape of the canonical
// Operate(Device.Reboot()) flow:
//
//  1. Bootstrap: cpe-sim emits OnBoardRequest (covered by #15;
//     drained here as setup).
//  2. Controller publishes Operate(Device.Reboot()) to
//     usp/v1/agent/<eid>.
//  3. Agent responds with OperateResp on usp/v1/controller.
//  4. After eventSchedule.rebootDelay (1s in this profile), the agent
//     emits Notify(Event{Boot!}) per the default-seed Subscription on
//     Device.Boot!.
//
// Two goldens: 01_operate_resp.txt (synchronous response) and
// 02_boot_event.txt (deferred Notify).
func TestUSP_OperateReboot(t *testing.T) {
	fix := harness.StartUSPAcceptance(t, harness.USPOptions{FirstContact: harness.FactoryReset})
	// Override the default minimal-usp/ profile with the reboot variant
	// (adds eventSchedule.rebootDelay). Re-materialize with the broker
	// host:port substituted.
	fix.ProfilePath = harness.MaterializeProfile(t,
		harness.AcceptanceProfilePath("minimal-usp-reboot"),
		map[string]string{
			"BROKER_ADDR": fix.BrokerHost,
			"BROKER_PORT": itoa(fix.BrokerPort),
		})

	_ = harness.LaunchSimDaemon(t, fix, "--seed=1")

	// 1. Drain OnBoardRequest (covered by #15's scenario).
	_ = fix.Capture.WaitForUSPMessage(t, 5*time.Second)

	// 2. Publish Operate(Device.Reboot()).
	_ = fix.PublishUSP(t, buildOperateRebootMsg())

	// 3. Capture OperateResp.
	raw := fix.Capture.WaitForUSPMessage(t, 5*time.Second)
	norm, err := harness.NormalizeUSPRecord(raw)
	if err != nil {
		t.Fatalf("NormalizeUSPRecord (operate_resp): %v", err)
	}
	harness.CompareGolden(t, "usp_operate_reboot/01_operate_resp.txt", norm)

	// 4. Wait for deferred Event{Boot!} (rebootDelay = 1s).
	raw = fix.Capture.WaitForUSPMessage(t, 5*time.Second)
	norm, err = harness.NormalizeUSPRecord(raw)
	if err != nil {
		t.Fatalf("NormalizeUSPRecord (boot_event): %v", err)
	}
	harness.CompareGolden(t, "usp_operate_reboot/02_boot_event.txt", norm)
}

// buildOperateRebootMsg returns a USP Msg for Operate(Device.Reboot()).
// command_key is left empty: synchronous Operate doesn't correlate
// against an async OperationComplete in this scenario.
func buildOperateRebootMsg() *uspproto.Msg {
	return &uspproto.Msg{
		Header: &uspproto.Header{MsgType: uspproto.Header_OPERATE},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_Operate{
						Operate: &uspproto.Operate{
							Command:    "Device.Reboot()",
							CommandKey: "",
							SendResp:   true,
						},
					},
				},
			},
		},
	}
}

func itoa(n int) string {
	const d = "0123456789"
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = d[n%10]
		n /= 10
	}
	return string(buf[pos:])
}
