package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/herder-labs/cpe-labs/internal/usp/codec"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

// TestRunDaemonModeUSPBundle1RoundTrip drives the full Bundle 1
// (#8) handler surface end-to-end against a real cpe-sim process
// connected to an embedded MQTT broker. Verifies:
//
//   - First-contact OnBoardRequest still fires (foundation from #7).
//   - Get handler returns GetResp with the requested leaf values.
//   - Set handler returns SetResp + tree state reflects the write.
//   - Add handler returns AddResp with instantiated_path + unique_keys
//     AND emits an autonomous ObjectCreation Notify with matching keys.
//   - Delete handler returns DeleteResp + emits an autonomous
//     ObjectDeletion Notify.
//   - Operate(Device.Reboot()) returns OperateResp; after the reboot
//     delay, an Event{Boot!} Notify emits.
//   - Operate(UnknownOp) returns OperateResp.cmd_failure 7022.
//   - Unmapped msg types return Error 7000.
//   - Every response's msg_id echoes the request's.
func TestRunDaemonModeUSPBundle1RoundTrip(t *testing.T) {
	brokerHost, brokerPort, brokerCleanup := startBrokerForTest(t)
	defer brokerCleanup()

	acs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 || !strings.Contains(string(body), "<cwmp:Inform>") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = w.Write([]byte(informResponseEnvelope))
	}))
	defer acs.Close()

	tmp := t.TempDir()
	profile := filepath.Join(tmp, "profile.yaml")
	if err := os.WriteFile(profile, []byte(fmt.Sprintf(`deviceIdPaths:
  manufacturer: Device.DeviceInfo.Manufacturer
  oui:          Device.DeviceInfo.ManufacturerOUI
  productClass: Device.DeviceInfo.ProductClass
  serialNumber: Device.DeviceInfo.SerialNumber

parameters:
  - path: Device.DeviceInfo.Manufacturer
    value: "TestVendor"
  - path: Device.DeviceInfo.ManufacturerOUI
    value: "AABBCC"
  - path: Device.DeviceInfo.ProductClass
    value: "TestModel"
  - path: Device.DeviceInfo.SerialNumber
    value: "BUNDLE1"

objects:
  - path: Device.WiFi.SSID
    instances: 1
    uniqueKeys:
      - [SSID]
    parameters:
      - path: SSID
        value: "Initial"
        writable: true
      - path: Enable
        type: xsd:boolean
        value: "false"
        writable: true

fleet:
  count: 1
  serialPattern: "{base}"

usp:
  enable: true
  controllerEndpointID: "self::openacs"
  broker:
    address: %s
    port: %d
    protocolVersion: "3.1.1"
`, brokerHost, brokerPort)), 0o600); err != nil {
		t.Fatal(err)
	}

	const agentEID = "os::AABBCCBUNDLE1"
	const controllerEID = "self::openacs"
	hub := newControllerHub(t, brokerHost, brokerPort)
	defer hub.disconnect()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	args := []string{
		"--acs-url=" + acs.URL,
		"--profile=" + profile,
		"--log-level=error",
	}
	done := make(chan error, 1)
	go func() { done <- run(ctx, args, os.Stdout, os.Stderr) }()

	// 1. First-contact OnBoardRequest.
	onboard := hub.awaitNotify(t, "OnBoardRequest", 5*time.Second, func(n *uspproto.Notify) bool {
		return n.GetOnBoardReq() != nil
	})
	if got := onboard.GetOnBoardReq().GetSerialNumber(); got != "BUNDLE1" {
		t.Fatalf("OnBoardRequest SerialNumber=%q want BUNDLE1", got)
	}

	// 2. Get.
	getResp := hub.send(t, agentEID, controllerEID, "msg-get", uspproto.Header_GET, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Get{Get: &uspproto.Get{ParamPaths: []string{
				"Device.DeviceInfo.SerialNumber",
				"Device.DeviceInfo.Manufacturer",
			}}},
		}},
	}, 3*time.Second)
	if getResp.GetHeader().GetMsgType() != uspproto.Header_GET_RESP {
		t.Fatalf("Get response MsgType=%v", getResp.GetHeader().GetMsgType())
	}
	results := getResp.GetBody().GetResponse().GetGetResp().GetReqPathResults()
	if results[0].GetResolvedPathResults()[0].GetResultParams()["SerialNumber"] != "BUNDLE1" {
		t.Errorf("Get SerialNumber result=%+v", results[0])
	}

	// 3. Set.
	setResp := hub.send(t, agentEID, controllerEID, "msg-set", uspproto.Header_SET, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Set{Set: &uspproto.Set{
				AllowPartial: false,
				UpdateObjs: []*uspproto.Set_UpdateObject{{
					ObjPath: "Device.WiFi.SSID.1.",
					ParamSettings: []*uspproto.Set_UpdateParamSetting{
						{Param: "SSID", Value: "Updated", Required: true},
					},
				}},
			}},
		}},
	}, 3*time.Second)
	if setResp.GetBody().GetResponse().GetSetResp().GetUpdatedObjResults()[0].GetOperStatus().GetOperSuccess() == nil {
		t.Fatalf("Set did not succeed: %+v", setResp.GetBody().GetResponse().GetSetResp())
	}

	// 4. Install ObjectCreation Subscription on Device.WiFi.SSID. so the
	// evaluator (Bundle 2) emits ObjectCreation on Add. Bundle 2 makes
	// these notifies Subscription-gated; Bundle 1 alone emitted them
	// unconditionally.
	installSubscription(t, hub, agentEID, controllerEID, "msg-sub-oc", "ObjectCreation", "Device.WiFi.SSID.")

	// Add Device.WiFi.SSID. -> AddResp + autonomous ObjectCreation.
	addResp := hub.send(t, agentEID, controllerEID, "msg-add", uspproto.Header_ADD, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Add{Add: &uspproto.Add{
				CreateObjs: []*uspproto.Add_CreateObject{{
					ObjPath: "Device.WiFi.SSID.",
					ParamSettings: []*uspproto.Add_CreateParamSetting{
						{Param: "SSID", Value: "Created", Required: true},
					},
				}},
			}},
		}},
	}, 3*time.Second)
	addSuccess := addResp.GetBody().GetResponse().GetAddResp().GetCreatedObjResults()[0].GetOperStatus().GetOperSuccess()
	if addSuccess == nil {
		t.Fatalf("Add did not succeed: %+v", addResp.GetBody().GetResponse().GetAddResp())
	}
	if !strings.HasPrefix(addSuccess.GetInstantiatedPath(), "Device.WiFi.SSID.") {
		t.Errorf("Add InstantiatedPath=%q", addSuccess.GetInstantiatedPath())
	}
	if addSuccess.GetUniqueKeys()["SSID"] != "Created" {
		t.Errorf("Add unique_keys[SSID]=%q", addSuccess.GetUniqueKeys()["SSID"])
	}
	addedPath := addSuccess.GetInstantiatedPath()

	objCreation := hub.awaitNotify(t, "ObjectCreation", 3*time.Second, func(n *uspproto.Notify) bool {
		return n.GetObjCreation() != nil && n.GetObjCreation().GetObjPath() == addedPath
	})
	if objCreation.GetObjCreation().GetUniqueKeys()["SSID"] != "Created" {
		t.Errorf("ObjectCreation unique_keys[SSID]=%q", objCreation.GetObjCreation().GetUniqueKeys()["SSID"])
	}

	// 5. Install ObjectDeletion Subscription. Same evaluator-gating
	// requirement.
	installSubscription(t, hub, agentEID, controllerEID, "msg-sub-od", "ObjectDeletion", "Device.WiFi.SSID.")

	// Delete the just-added instance -> DeleteResp + autonomous
	// ObjectDeletion.
	delResp := hub.send(t, agentEID, controllerEID, "msg-del", uspproto.Header_DELETE, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Delete{Delete: &uspproto.Delete{
				ObjPaths: []string{addedPath},
			}},
		}},
	}, 3*time.Second)
	if delResp.GetBody().GetResponse().GetDeleteResp().GetDeletedObjResults()[0].GetOperStatus().GetOperSuccess() == nil {
		t.Fatalf("Delete did not succeed: %+v", delResp.GetBody().GetResponse().GetDeleteResp())
	}
	objDeletion := hub.awaitNotify(t, "ObjectDeletion", 3*time.Second, func(n *uspproto.Notify) bool {
		return n.GetObjDeletion() != nil && n.GetObjDeletion().GetObjPath() == addedPath
	})
	_ = objDeletion

	// Restore the default-seed Subscription to Event/Boot! so the
	// Reboot test below sees a matching subscription for Boot!. The
	// previous installSubscription overrode the row.
	installSubscription(t, hub, agentEID, controllerEID, "msg-sub-restore", "Event", "Device.Boot!")

	// 6. Operate(Device.Reboot()) -> OperateResp + Event{Boot!}.
	opResp := hub.send(t, agentEID, controllerEID, "msg-op", uspproto.Header_OPERATE, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Operate{Operate: &uspproto.Operate{
				Command:    "Device.Reboot()",
				CommandKey: "test-reboot",
				SendResp:   true,
			}},
		}},
	}, 3*time.Second)
	opResult := opResp.GetBody().GetResponse().GetOperateResp().GetOperationResults()[0]
	if opResult.GetReqOutputArgs() == nil {
		t.Fatalf("Reboot did not return success: %+v", opResult)
	}

	hub.awaitNotify(t, "Event{Boot!}", 3*time.Second, func(n *uspproto.Notify) bool {
		ev := n.GetEvent()
		return ev != nil && ev.GetEventName() == "Boot!"
	})

	// 7. Operate(UnknownOp) -> OperateResp with cmd_failure 7022.
	opUnknown := hub.send(t, agentEID, controllerEID, "msg-op2", uspproto.Header_OPERATE, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Operate{Operate: &uspproto.Operate{
				Command:    "Device.UnknownOp()",
				CommandKey: "test-unknown",
			}},
		}},
	}, 3*time.Second)
	fail := opUnknown.GetBody().GetResponse().GetOperateResp().GetOperationResults()[0].GetCmdFailure()
	if fail == nil || fail.GetErrCode() != 7022 {
		t.Errorf("Unknown command err_code=%v want 7022 (cmd_failure=%+v)", fail, fail)
	}

	// 8. Unmapped msg type (Register, which neither Bundle 1 nor 2 implements)
	// returns Error 7000.
	errResp := hub.send(t, agentEID, controllerEID, "msg-unmapped", uspproto.Header_REGISTER, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Register{Register: &uspproto.Register{}},
		}},
	}, 3*time.Second)
	if errResp.GetHeader().GetMsgType() != uspproto.Header_ERROR {
		t.Fatalf("unmapped msg type response MsgType=%v want ERROR", errResp.GetHeader().GetMsgType())
	}
	if errResp.GetBody().GetError().GetErrCode() != 7000 {
		t.Errorf("unmapped msg type ErrCode=%d want 7000", errResp.GetBody().GetError().GetErrCode())
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run returned error after cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return within 5s of ctx cancel")
	}
}

// controllerHub is a paho client connected to the embedded broker
// that subscribes to usp/v1/controller and exposes send() to publish
// requests to the agent's inbox topic, awaiting the matching response
// by msg_id.
type controllerHub struct {
	client    paho.Client
	mu        sync.Mutex
	pending   map[string]chan *uspproto.Msg
	notifies  chan *uspproto.Notify
	disconned bool
}

func newControllerHub(t *testing.T, host string, port int) *controllerHub {
	t.Helper()
	h := &controllerHub{
		pending:  make(map[string]chan *uspproto.Msg),
		notifies: make(chan *uspproto.Notify, 16),
	}
	h.client = newSubscriberClient(t, host, port, fmt.Sprintf("bundle1-hub-%d", time.Now().UnixNano()))
	if token := h.client.Subscribe("usp/v1/controller", 1, func(_ paho.Client, msg paho.Message) {
		_, parsed, err := codec.UnwrapRecord(msg.Payload())
		if err != nil {
			t.Logf("hub UnwrapRecord: %v", err)
			return
		}
		h.dispatch(parsed)
	}); token.Wait() && token.Error() != nil {
		t.Fatalf("hub Subscribe: %v", token.Error())
	}
	return h
}

func (h *controllerHub) dispatch(msg *uspproto.Msg) {
	header := msg.GetHeader()
	mt := header.GetMsgType()
	if mt == uspproto.Header_NOTIFY {
		if n := msg.GetBody().GetRequest().GetNotify(); n != nil {
			select {
			case h.notifies <- n:
			default:
			}
		}
		return
	}
	h.mu.Lock()
	ch, ok := h.pending[header.GetMsgId()]
	if ok {
		delete(h.pending, header.GetMsgId())
	}
	h.mu.Unlock()
	if ok {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (h *controllerHub) send(t *testing.T, agentEID, controllerEID, msgID string, mt uspproto.Header_MsgType, body *uspproto.Body, timeout time.Duration) *uspproto.Msg {
	t.Helper()
	req := &uspproto.Msg{
		Header: &uspproto.Header{MsgId: msgID, MsgType: mt},
		Body:   body,
	}
	wire, err := codec.WrapMessage(req, controllerEID, agentEID)
	if err != nil {
		t.Fatalf("WrapMessage: %v", err)
	}
	respCh := make(chan *uspproto.Msg, 1)
	h.mu.Lock()
	h.pending[msgID] = respCh
	h.mu.Unlock()

	topic := "usp/v1/agent/" + agentEID
	if token := h.client.Publish(topic, 1, false, wire); !token.WaitTimeout(timeout) {
		t.Fatalf("publish to %s timed out", topic)
	}
	select {
	case resp := <-respCh:
		return resp
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for response to msg_id=%s (type=%s)", msgID, mt)
		return nil
	}
}

func (h *controllerHub) awaitNotify(t *testing.T, name string, timeout time.Duration, match func(*uspproto.Notify) bool) *uspproto.Notify {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case n := <-h.notifies:
			if match(n) {
				return n
			}
		case <-deadline:
			t.Fatalf("timed out waiting for Notify %s", name)
			return nil
		}
	}
}

func (h *controllerHub) disconnect() {
	h.mu.Lock()
	if h.disconned {
		h.mu.Unlock()
		return
	}
	h.disconned = true
	h.mu.Unlock()
	h.client.Disconnect(250)
}

// installSubscription overrides the default-seed Subscription row (.1.)
// to match the (notifType, referenceList) combination needed for a
// later test assertion. Waits 100ms for the evaluator's debounced
// index rebuild. Bundle 1 originally emitted ObjectCreation/Deletion
// unconditionally; with the Bundle 2 evaluator in place, gating means
// the test must install a matching Subscription first.
func installSubscription(t *testing.T, hub *controllerHub, agentEID, controllerEID, msgID, notifType, referenceList string) {
	t.Helper()
	resp := hub.send(t, agentEID, controllerEID, msgID, uspproto.Header_SET, &uspproto.Body{
		MsgBody: &uspproto.Body_Request{Request: &uspproto.Request{
			ReqType: &uspproto.Request_Set{Set: &uspproto.Set{
				UpdateObjs: []*uspproto.Set_UpdateObject{{
					ObjPath: "Device.LocalAgent.Subscription.1.",
					ParamSettings: []*uspproto.Set_UpdateParamSetting{
						{Param: "NotifType", Value: notifType, Required: true},
						{Param: "ReferenceList", Value: referenceList, Required: true},
					},
				}},
			}},
		}},
	}, 3*time.Second)
	if resp.GetBody().GetResponse().GetSetResp().GetUpdatedObjResults()[0].GetOperStatus().GetOperSuccess() == nil {
		t.Fatalf("install subscription failed: %+v", resp)
	}
	time.Sleep(120 * time.Millisecond)
}
