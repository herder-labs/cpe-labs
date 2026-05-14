//go:build acceptance

package scenarios_test

import (
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/acceptance/harness"
	"github.com/herder-labs/cpe-labs/internal/usp/codec"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

// TestUSP_SetValueChange captures the wire shape of the autonomous
// Notify(ValueChange) flow:
//
//  1. Bootstrap: agent emits OnBoardRequest (drained as setup).
//  2. Controller publishes Add to install a Subscription:
//        Subscription.{Enable=true, ID="vc-sub-1",
//                      NotifType=ValueChange,
//                      ReferenceList=Device.WiFi.Radio.1.Channel}
//  3. Agent responds with AddResp (drained as setup; covered by
//     Bundle 1 tests). Evaluator's debounced rescan picks up the
//     new subscription.
//  4. Controller publishes Set on the watched leaf:
//        Set{ObjPath=Device.WiFi.Radio.1., Param=Channel, Value=11}
//  5. Agent responds with SetResp (golden 01) AND autonomously
//     emits Notify(ValueChange) with subscription_id="vc-sub-1"
//     and param_path/param_value pointing to the new value (golden 02).
//
// The two messages in step 5 are captured in either order — the
// scenario classifies by msg_type rather than position.
func TestUSP_SetValueChange(t *testing.T) {
	fix := harness.StartUSPAcceptance(t, harness.USPOptions{FirstContact: harness.FactoryReset})
	fix.ProfilePath = harness.MaterializeProfile(t,
		harness.AcceptanceProfilePath("minimal-usp-set-vc"),
		map[string]string{
			"BROKER_ADDR": fix.BrokerHost,
			"BROKER_PORT": itoa(fix.BrokerPort),
		})

	_ = harness.LaunchSimDaemon(t, fix, "--seed=1")

	// 1. Drain OnBoardRequest.
	_ = fix.Capture.WaitForUSPMessage(t, 5*time.Second)

	// 2. Install Subscription via Add.
	_ = fix.PublishUSP(t, buildAddSubscriptionMsg(
		"vc-sub-1",
		"ValueChange",
		"Device.WiFi.Radio.1.Channel",
		fix.ControllerEID,
	))

	// 3. Drain AddResp.
	_ = fix.Capture.WaitForUSPMessage(t, 5*time.Second)

	// Give the evaluator's 50ms debounced rescan a moment to pick
	// up the new Subscription before driving Set. Without this, Set
	// could land before the index rebuild and the ValueChange Notify
	// wouldn't fire.
	time.Sleep(100 * time.Millisecond)

	// 4. Publish Set on the watched leaf.
	_ = fix.PublishUSP(t, buildSetMsg(
		"Device.WiFi.Radio.1.",
		"Channel", "11",
	))

	// 5. Capture SetResp + Notify(ValueChange) in either order.
	raw1 := fix.Capture.WaitForUSPMessage(t, 5*time.Second)
	raw2 := fix.Capture.WaitForUSPMessage(t, 5*time.Second)
	setResp, valueChange := classifySetVCPair(t, raw1, raw2)

	normSet, err := harness.NormalizeUSPRecord(setResp)
	if err != nil {
		t.Fatalf("NormalizeUSPRecord (set_resp): %v", err)
	}
	harness.CompareGolden(t, "usp_set_value_change/01_set_resp.txt", normSet)

	normVC, err := harness.NormalizeUSPRecord(valueChange)
	if err != nil {
		t.Fatalf("NormalizeUSPRecord (value_change): %v", err)
	}
	harness.CompareGolden(t, "usp_set_value_change/02_value_change_notify.txt", normVC)
}

// classifySetVCPair decodes both raw Records, finds the one whose
// embedded Msg is SET_RESP and the one that's NOTIFY. Either capture
// order is valid.
func classifySetVCPair(t *testing.T, a, b []byte) (setResp, valueChange []byte) {
	t.Helper()
	for _, raw := range [][]byte{a, b} {
		_, msg, err := codec.UnwrapRecord(raw)
		if err != nil {
			t.Fatalf("classifySetVCPair: unwrap: %v", err)
		}
		switch msg.GetHeader().GetMsgType() {
		case uspproto.Header_SET_RESP:
			setResp = raw
		case uspproto.Header_NOTIFY:
			valueChange = raw
		default:
			t.Fatalf("unexpected msg_type %s; expected SET_RESP or NOTIFY",
				msg.GetHeader().GetMsgType())
		}
	}
	if setResp == nil {
		t.Fatalf("no SET_RESP captured")
	}
	if valueChange == nil {
		t.Fatalf("no NOTIFY captured")
	}
	return setResp, valueChange
}

func buildAddSubscriptionMsg(id, notifType, referenceList, recipient string) *uspproto.Msg {
	return &uspproto.Msg{
		Header: &uspproto.Header{MsgType: uspproto.Header_ADD},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_Add{
						Add: &uspproto.Add{
							AllowPartial: false,
							CreateObjs: []*uspproto.Add_CreateObject{{
								ObjPath: "Device.LocalAgent.Subscription.",
								ParamSettings: []*uspproto.Add_CreateParamSetting{
									{Param: "Enable", Value: "true"},
									{Param: "ID", Value: id},
									{Param: "NotifType", Value: notifType},
									{Param: "ReferenceList", Value: referenceList},
									{Param: "Recipient", Value: recipient},
								},
							}},
						},
					},
				},
			},
		},
	}
}

func buildSetMsg(objPath, param, value string) *uspproto.Msg {
	return &uspproto.Msg{
		Header: &uspproto.Header{MsgType: uspproto.Header_SET},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_Set{
						Set: &uspproto.Set{
							AllowPartial: false,
							UpdateObjs: []*uspproto.Set_UpdateObject{{
								ObjPath: objPath,
								ParamSettings: []*uspproto.Set_UpdateParamSetting{
									{Param: param, Value: value},
								},
							}},
						},
					},
				},
			},
		},
	}
}
